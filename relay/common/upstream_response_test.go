package common

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPAStatusFramesPreserveOtherSSEBytes(t *testing.T) {
	longLine := "data: " + strings.Repeat("x", 16*1024) + "\n\n"
	for _, tc := range []struct {
		name, input, want, status string
	}{
		{"no initial header", "data: first\n\n: cpa-status normal\n\ndata: second\n\n: cpa-status degraded\n\ndata: third\n\n", "data: first\n\ndata: second\n\ndata: third\n\n", "degraded"},
		{"only metadata", ": cpa-status degraded\n\n", "", "degraded"},
		{"normal frame cannot clear degradation", ": cpa-status degraded\n\n: cpa-status normal\n\n", "", "degraded"},
		{"CRLF", "data: first\r\n\r\n: cpa-status degraded\r\n\r\ndata: second\r\n\r\n", "data: first\r\n\r\ndata: second\r\n\r\n", "degraded"},
		{"mixed line endings", ": cpa-status degraded\r\n\n: ordinary\r\n\n", ": ordinary\r\n\n", "degraded"},
		{"business frame separator", "data: first\n: cpa-status degraded\n\ndata: second\n\n", "data: first\n: cpa-status degraded\n\ndata: second\n\n", "normal"},
		{"candidate inside business frame", ": cpa-status degraded\ndata: first\n\ndata: second\n\n", ": cpa-status degraded\ndata: first\n\ndata: second\n\n", "normal"},
		{"two comments in one frame", ": cpa-status degraded\n: heartbeat\n\n", ": cpa-status degraded\n: heartbeat\n\n", "normal"},
		{"ordinary comment", ": heartbeat\n\n", ": heartbeat\n\n", "normal"},
		{"invalid verdict", ": cpa-status unknown\n\n", ": cpa-status unknown\n\n", "normal"},
		{"trailing spaces are not protocol", ": cpa-status degraded \n\n", ": cpa-status degraded \n\n", "normal"},
		{"extra blank frames", "\n\n: cpa-status degraded\n\n\ndata: done\n\n", "\n\n\ndata: done\n\n", "degraded"},
		{"long line", longLine + ": cpa-status degraded\n\n", longLine, "degraded"},
		{"long candidate lookalike", ": cpa-status degraded" + strings.Repeat("x", 16*1024) + "\n\n", ": cpa-status degraded" + strings.Repeat("x", 16*1024) + "\n\n", "normal"},
		{"candidate followed by long business line", ": cpa-status degraded\n" + longLine, ": cpa-status degraded\n" + longLine, "normal"},
		{"EOF without line ending", longLine + ": cpa-status degraded", longLine + ": cpa-status degraded", "normal"},
		{"EOF without separator", "data: first\n\n: cpa-status degraded\n", "data: first\n\n: cpa-status degraded\n", "normal"},
		{"EOF after partial business", ": cpa-status degraded\ndata: last", ": cpa-status degraded\ndata: last", "normal"},
		{"EOF without any data", "", "", "normal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fragmented := range []bool{false, true} {
				var reader io.Reader = strings.NewReader(tc.input)
				if fragmented {
					reader = iotest.OneByteReader(reader)
				}
				info := &RelayInfo{}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"Text/Event-Stream; charset=utf-8"}}, Body: io.NopCloser(reader)}
				info.RecordUpstreamResponse(response)
				// Small caller reads also exercise partially returned bufio fragments.
				body, err := io.ReadAll(iotest.HalfReader(response.Body))
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(body), "fragmented=%v", fragmented)
				assert.Equal(t, tc.status, info.UpstreamResponse.RequestStatus())
				require.NoError(t, response.Body.Close())
			}
		})
	}
}

func TestCPAStatusHeaderAndHTTPErrorSemantics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		values []string
		want   string
	}{
		{"normal", 200, []string{"normal"}, "normal"},
		{"degraded", 200, []string{"degraded"}, "degraded"},
		{"missing", 200, nil, "normal"},
		{"invalid", 200, []string{"unexpected"}, "normal"},
		{"multiple", 200, []string{"normal", "degraded"}, "normal"},
		{"HTTP error wins", 503, []string{"degraded"}, "error"},
		{"rate limited", 429, []string{"normal"}, "error"},
		{"other success", 201, []string{"degraded"}, "unknown"},
		{"redirect", 302, nil, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{}
			response := &http.Response{StatusCode: tc.code, Header: http.Header{"x-CpA-StAtUs": tc.values, "X-Codex-Turn-State": {strings.Repeat("opaque", 52)}}}
			info.RecordUpstreamResponse(response)
			assert.Equal(t, tc.want, info.UpstreamResponse.RequestStatus())
			for name := range response.Header {
				assert.NotEqual(t, strings.ToLower(CPAStatusHeader), strings.ToLower(name))
			}
			assert.Contains(t, response.Header, "X-Codex-Turn-State", "opaque protocol header still passes through")
		})
	}
}

func TestCPAStatusReadFailurePreservesPendingBytes(t *testing.T) {
	reader := io.MultiReader(strings.NewReader(": cpa-status degraded\n"), iotest.ErrReader(context.Canceled))
	info := &RelayInfo{}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(reader)}
	info.RecordUpstreamResponse(response)
	body, err := io.ReadAll(response.Body)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, ": cpa-status degraded\n", string(body))
	assert.Equal(t, "normal", info.UpstreamResponse.RequestStatus(), "an incomplete frame is not a verdict")
}

func TestLatePreviousAttemptCannotChangeRetryStatus(t *testing.T) {
	info := &RelayInfo{}
	first := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(": cpa-status degraded\n\n"))}
	info.RecordUpstreamResponse(first)
	info.RecordUpstreamResponse(&http.Response{StatusCode: 200})
	_, err := io.ReadAll(first.Body)
	require.NoError(t, err)
	assert.Equal(t, "normal", info.UpstreamResponse.RequestStatus())
}

func TestCPAStatusTrailerDeclarationIsPrivateAndIncompleteBodiesDoNotFinalize(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		var reader io.Reader = strings.NewReader("body")
		if cancelled {
			reader = io.MultiReader(reader, iotest.ErrReader(context.Canceled))
		}
		info := &RelayInfo{}
		response := &http.Response{StatusCode: 200, Header: http.Header{"Trailer": {"X-CPA-Status, X-Public-Fact"}}, Trailer: http.Header{"X-Cpa-Status": {"degraded"}, "X-Public-Fact": {"keep"}}, Body: io.NopCloser(reader)}
		info.RecordUpstreamResponse(response)
		assert.Equal(t, "X-Public-Fact", response.Header.Get("Trailer"))
		body, err := io.ReadAll(response.Body)
		assert.Equal(t, "body", string(body))
		if cancelled {
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, "normal", info.UpstreamResponse.RequestStatus())
		} else {
			require.NoError(t, err)
			assert.Equal(t, "degraded", info.UpstreamResponse.RequestStatus())
			assert.Empty(t, response.Trailer.Get(CPAStatusHeader))
			assert.Equal(t, "keep", response.Trailer.Get("X-Public-Fact"))
		}
	}
}
