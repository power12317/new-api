package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamResponseLogInfoUsesCPAStatusOnly(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.RecordUpstreamResponse(&http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-CPA-Status": {"degraded"}}})
	other := model.NewLogOther()
	service.AppendUpstreamResponseLogInfo(info, other)
	var fields map[string]any
	require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &fields))
	assert.Equal(t, "degraded", fields["upstream_request_status"])
	assert.NotContains(t, fields, "turn_state_length")
	assert.NotContains(t, fields, "turn_state_verdict")
}

func TestUpstreamResponseHTTPAttemptsDoNotLeakPreviousFacts(t *testing.T) {
	service.InitHttpClient()
	requestHeader := strings.Repeat("r", 128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			w.Header().Set(relaycommon.CPAStatusHeader, "degraded")
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = io.WriteString(w, "response body")
	}))
	t.Cleanup(server.Close)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-State", requestHeader)
	info := relaycommon.GenRelayInfoOpenAI(c, &dto.GeneralOpenAIRequest{ReasoningEffort: "high"})
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.SetReasoningEffort("low")
	for _, path := range []string{"/first", "/retry"} {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader("request"))
		require.NoError(t, err)
		resp, err := channel.DoRequest(c, req, info)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, "response body", string(body))
		if path == "/first" {
			assert.Equal(t, 503, info.UpstreamResponse.StatusCode)
			assert.Equal(t, "error", info.UpstreamResponse.RequestStatus())
		} else {
			assert.Equal(t, 200, info.UpstreamResponse.StatusCode)
			assert.Equal(t, "normal", info.UpstreamResponse.RequestStatus())
		}
	}
	info.InitChannelMeta(c)
	assert.Nil(t, info.UpstreamResponse)
	server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("request"))
	require.NoError(t, err)
	_, err = channel.DoRequest(c, req, info)
	require.Error(t, err)
	other := model.NewLogOther()
	service.AppendUpstreamResponseLogInfo(info, other)
	assert.Equal(t, "error", other.Snapshot()["upstream_request_status"])
	assert.NotContains(t, other.Snapshot(), "upstream_status_code")
}

func TestCPAStatusNegotiationRespectsRequestConfiguration(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name        string
		mode        int
		path        string
		passthrough bool
		runtime     bool
		override    map[string]any
		clientValue *string
		want        string
	}{
		{name: "responses", mode: relayconstant.RelayModeResponses, want: "1"},
		{name: "chat", mode: relayconstant.RelayModeChatCompletions, want: "1"},
		{name: "compact", mode: relayconstant.RelayModeResponsesCompact, want: "1"},
		{name: "Gemini generation", mode: relayconstant.RelayModeGemini, path: "/v1beta/models/gemini:generateContent", want: "1"},
		{name: "Gemini streaming generation", mode: relayconstant.RelayModeGemini, path: "/v1beta/models/gemini:streamGenerateContent?alt=sse", want: "1"},
		{name: "Gemini embeddings", mode: relayconstant.RelayModeGemini, path: "/v1beta/models/gemini:embedContent"},
		{name: "embeddings", mode: relayconstant.RelayModeEmbeddings},
		{name: "images", mode: relayconstant.RelayModeImagesGenerations},
		{name: "unknown non inference", mode: relayconstant.RelayModeUnknown},
		{name: "configured disabled", mode: relayconstant.RelayModeResponses, override: map[string]any{"X-CPA-Status": "0"}, want: "0"},
		{name: "configured empty", mode: relayconstant.RelayModeResponses, override: map[string]any{"X-CPA-Status": ""}},
		{name: "runtime disabled", mode: relayconstant.RelayModeResponses, runtime: true, override: map[string]any{"x-cpa-status": "0"}, want: "0"},
		{name: "runtime empty", mode: relayconstant.RelayModeResponses, runtime: true, override: map[string]any{"x-cpa-status": ""}},
		{name: "passthrough default", mode: relayconstant.RelayModeResponses, passthrough: true, want: "1"},
		{name: "passthrough client value", mode: relayconstant.RelayModeResponses, passthrough: true, clientValue: common.GetPointer("0"), want: "0"},
		{name: "passthrough empty value", mode: relayconstant.RelayModeResponses, passthrough: true, clientValue: common.GetPointer("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.want, r.Header.Get(relaycommon.CPAStatusHeader))
				assert.Equal(t, "keep", r.Header.Get("X-Custom"))
				w.Header().Set(relaycommon.CPAStatusHeader, "degraded")
				_, _ = io.WriteString(w, `{"id":"response","model":"gpt-test","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
			}))
			defer server.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			path := tc.path
			if path == "" {
				path = "/v1/responses"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-test"}`))
			c.Request.Header.Set("X-Custom", "keep")
			if tc.clientValue != nil {
				c.Request.Header.Set(relaycommon.CPAStatusHeader, *tc.clientValue)
			}
			headers := map[string]any{"X-Custom": "keep"}
			for key, value := range tc.override {
				headers[key] = value
			}
			info := &relaycommon.RelayInfo{RelayMode: tc.mode, RequestURLPath: path, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "channel-key", UpstreamModelName: "gpt-test", HeadersOverride: headers, ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough}}}
			if tc.runtime {
				info.UseRuntimeHeadersOverride = true
				info.RuntimeHeadersOverride = headers
				info.HeadersOverride = map[string]any{relaycommon.CPAStatusHeader: "1"}
			}
			response, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-test"}`))
			require.NoError(t, err)
			usage, apiErr := openai.OaiResponsesHandler(c, info, response)
			require.Nil(t, apiErr)
			assert.Equal(t, 5, usage.TotalTokens)
			assert.Empty(t, response.Header.Get(relaycommon.CPAStatusHeader))
			assert.Empty(t, recorder.Header().Get(relaycommon.CPAStatusHeader))
			other := model.NewLogOther()
			service.AppendUpstreamResponseLogInfo(info, other)
			assert.Equal(t, "degraded", other.Snapshot()["upstream_request_status"])
			assert.NotContains(t, other.JSONString(), "turn_state_")
		})
	}
}

func TestCPAStatusIndependentHTTPRequestsUseOnlyCPAResult(t *testing.T) {
	service.InitHttpClient()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	rounds := []struct {
		status                        string
		requestLength, responseLength int
	}{
		{"degraded", 292, 332},
		{"normal", 312, 356},
		{"degraded", 0, 0},
		{"normal", 356, 312},
		{"normal", 780, 780},
		{"", 312, 356},
	}
	responseJSON := `{"id":"response","model":"gpt-test","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
	for _, transport := range []string{"header", "sse", "trailer"} {
		t.Run(transport, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index, err := strconv.Atoi(r.URL.Query().Get("round"))
				if !assert.NoError(t, err) {
					return
				}
				round := rounds[index]
				assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
				assert.Equal(t, "1", r.Header.Get(relaycommon.CPAStatusHeader))
				assert.Len(t, r.Header.Get("X-Codex-Turn-State"), round.requestLength)
				w.Header().Set("X-Codex-Turn-State", strings.Repeat("s", round.responseLength))
				switch transport {
				case "sse":
					w.Header().Set("Content-Type", "text/event-stream")
					if round.status != "" {
						_, _ = io.WriteString(w, ": cpa-status "+round.status+"\n\n")
					}
					_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":"+responseJSON+"}\n\ndata: [DONE]\n\n")
				case "trailer":
					w.Header().Set("Trailer", relaycommon.CPAStatusHeader)
					_, _ = io.WriteString(w, "\n")
					w.(http.Flusher).Flush()
					_, _ = io.WriteString(w, responseJSON)
					w.Header().Set(relaycommon.CPAStatusHeader, round.status)
				default:
					w.Header().Set(relaycommon.CPAStatusHeader, round.status)
					_, _ = io.WriteString(w, responseJSON)
				}
			}))
			defer server.Close()
			for index, round := range rounds {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				path := "/v1/responses?round=" + strconv.Itoa(index)
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-test"}`))
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, RequestURLPath: path, DisablePing: true, IsStream: transport == "sse", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "channel-key", UpstreamModelName: "gpt-test", HeadersOverride: map[string]any{"X-Codex-Turn-State": strings.Repeat("q", round.requestLength)}}}
				response, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-test"}`))
				require.NoError(t, err)
				handler := openai.OaiResponsesHandler
				if transport == "sse" {
					handler = openai.OaiResponsesStreamHandler
				}
				usage, apiErr := handler(c, info, response)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 5, usage.TotalTokens)
				other := model.NewLogOther()
				service.AppendUpstreamResponseLogInfo(info, other)
				want := round.status
				if want == "" {
					want = "normal"
				}
				assert.Equal(t, want, other.Snapshot()["upstream_request_status"], "request %d", index)
				assert.NotContains(t, other.JSONString(), "turn_state_")
				assert.Empty(t, recorder.Header().Get(relaycommon.CPAStatusHeader))
				assert.NotContains(t, recorder.Body.String(), "cpa-status")
			}
		})
	}
}

// The protocol terminal is complete, but the HTTP connection has not reached
// EOF. A handler must finish without asking for another upstream body read.
type cpaTerminalBody struct {
	*strings.Reader
	tailReads int
}

func (b *cpaTerminalBody) Read(p []byte) (int, error) {
	if b.Len() == 0 {
		b.tailReads++
		return 0, io.ErrUnexpectedEOF
	}
	return b.Reader.Read(p)
}

func TestCPAStatusDoesNotReadAfterProtocolTerminal(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-test\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"
	for _, tc := range []struct {
		name     string
		handler  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
		terminal string
	}{
		{"responses DONE", openai.OaiResponsesStreamHandler, completed + "data: [DONE]\n\n"},
		{"buffered completion", openai.OaiResponsesToChatBufferedStreamHandler, completed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{DisablePing: true, IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			body := &cpaTerminalBody{Reader: strings.NewReader(": cpa-status degraded\n\n" + tc.terminal)}
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body)}
			info.RecordUpstreamResponse(response)
			usage, apiErr := tc.handler(c, info, response)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 5, usage.TotalTokens)
			assert.Zero(t, body.tailReads, "completion must not depend on additional upstream reads")
			other := model.NewLogOther()
			service.AppendUpstreamResponseLogInfo(info, other)
			assert.Equal(t, "degraded", other.Snapshot()["upstream_request_status"])
			assert.NotContains(t, recorder.Body.String(), "cpa-status")
		})
	}
}

func TestCPAStatusKeepaliveTrailersFinalizeNonStreamLog(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name, header, trailer, want string
		code                        int
	}{
		{"degraded after keepalive", "normal", "degraded", "degraded", 200},
		{"without initial header", "", "degraded", "degraded", 200},
		{"normal trailer", "", "normal", "normal", 200},
		{"normal trailer cannot clear degraded header", "degraded", "normal", "degraded", 200},
		{"invalid trailer ignored", "degraded", "invalid", "degraded", 200},
		{"HTTP error preserved", "normal", "degraded", "error", 503},
		{"normal trailer cannot erase HTTP error", "degraded", "normal", "error", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\n" + `{"id":"response","model":"gpt-test","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "1", r.Header.Get(relaycommon.CPAStatusHeader))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Trailer", "X-CPA-Status, X-Public-Fact")
				if tc.header != "" {
					w.Header().Set(relaycommon.CPAStatusHeader, tc.header)
				}
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, body[:1])
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, body[1:])
				w.Header().Set(relaycommon.CPAStatusHeader, tc.trailer)
				w.Header().Set("X-Public-Fact", "preserved")
			}))
			defer server.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "channel-key"}}
			response, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader("{}"))
			require.NoError(t, err)
			usage, apiErr := openai.OaiResponsesHandler(c, info, response)
			require.Nil(t, apiErr)
			assert.Equal(t, 5, usage.TotalTokens)
			assert.Equal(t, body, recorder.Body.String())
			assert.Empty(t, response.Trailer.Get(relaycommon.CPAStatusHeader))
			assert.Equal(t, "preserved", response.Trailer.Get("X-Public-Fact"))
			assert.Empty(t, recorder.Header().Get(relaycommon.CPAStatusHeader))
			assert.NotContains(t, strings.ToLower(recorder.Header().Get("Trailer")), "x-cpa-status")
			other := model.NewLogOther()
			service.AppendUpstreamResponseLogInfo(info, other)
			assert.Equal(t, tc.want, other.Snapshot()["upstream_request_status"])
		})
	}
}
