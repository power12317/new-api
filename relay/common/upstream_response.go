package common

import (
	"bufio"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

const CPAStatusHeader = "X-CPA-Status"

// UpstreamResponseInfo holds one attempt's transport status and CPA verdict.
// The HTTP status is immutable; streaming CPA updates use the same snapshot.
type UpstreamResponseInfo struct {
	StatusCode int
	mu         sync.RWMutex
	status     string
}

func (r *UpstreamResponseInfo) RequestStatus() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

// RecordWebSocketCPAStatus records the active generation's verdict without
// inventing an HTTP status for it. The serial request worker owns this snapshot;
// a reused connection gets a fresh RelayInfo for every response.create.
func (info *RelayInfo) RecordWebSocketCPAStatus(status string) {
	if info == nil || status != "normal" && status != "degraded" {
		return
	}
	if info.UpstreamResponse == nil {
		info.UpstreamResponse = &UpstreamResponseInfo{status: "normal"}
	}
	info.UpstreamResponse.observeCPAStatus(status)
}

func (r *UpstreamResponseInfo) observeCPAStatus(status string) {
	if status != "normal" && status != "degraded" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// A normal update cannot clear degradation already reported in this attempt.
	if status == "degraded" && (r.StatusCode == 0 || r.StatusCode == http.StatusOK) {
		r.status = status
	}
}

func (r *UpstreamResponseInfo) consumeCPAStatus(headers http.Header) {
	var values []string
	for name, entries := range headers {
		if strings.EqualFold(name, CPAStatusHeader) {
			values = append(values, entries...)
			delete(headers, name)
		}
	}
	if len(values) == 1 {
		r.observeCPAStatus(values[0])
	}
}

// RecordUpstreamResponse replaces the previous attempt's facts. Only CPA decides
// quality; opaque turn-state headers remain transport data and are not inspected.
func (info *RelayInfo) RecordUpstreamResponse(response *http.Response) {
	if info == nil {
		return
	}
	result := &UpstreamResponseInfo{status: "error"}
	info.UpstreamResponse = result
	if response == nil {
		return
	}
	result.StatusCode = response.StatusCode
	switch {
	case response.StatusCode == http.StatusOK:
		result.status = "normal"
	case response.StatusCode >= 400 && response.StatusCode <= 599:
		result.status = "error"
	default:
		result.status = "unknown"
	}
	result.consumeCPAStatus(response.Header)
	for name, entries := range response.Header {
		if !strings.EqualFold(name, "Trailer") {
			continue
		}
		var publicTrailers []string
		for _, entry := range entries {
			for field := range strings.SplitSeq(entry, ",") {
				field = strings.TrimSpace(field)
				if field != "" && !strings.EqualFold(field, CPAStatusHeader) {
					publicTrailers = append(publicTrailers, field)
				}
			}
		}
		delete(response.Header, name)
		if len(publicTrailers) > 0 {
			response.Header[name] = []string{strings.Join(publicTrailers, ", ")}
		}
	}
	if response.Body != nil {
		response.Body = &upstreamStatusBody{ReadCloser: response.Body, response: response, status: result}
	}
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.Body != nil && mediaType == "text/event-stream" {
		response.Body = &cpaStatusBody{
			ReadCloser: response.Body, reader: bufio.NewReader(response.Body), status: result,
			frameStart: true,
		}
		response.Header.Del("Content-Length")
		response.ContentLength = -1
	}
}

// HTTP keepalive may commit headers before CPA has a verdict. net/http fills
// Trailer at EOF; consume it before the response handler constructs its log.
type upstreamStatusBody struct {
	io.ReadCloser
	response *http.Response
	status   *UpstreamResponseInfo
}

func (b *upstreamStatusBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.status.consumeCPAStatus(b.response.Trailer)
	}
	return n, err
}

// Only a complete, standalone reserved comment frame is internal metadata.
// Ordinary frames (including matching comments inside them), long lines and
// unterminated EOF fragments pass through byte-for-byte. At most one small
// candidate line is held while its following blank separator is checked.
type cpaStatusBody struct {
	io.ReadCloser
	reader       *bufio.Reader
	status       *UpstreamResponseInfo
	pending      []byte
	candidate    []byte
	continuation bool
	frameStart   bool
	err          error
}

func (b *cpaStatusBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.pending) == 0 {
		if b.err != nil {
			if len(b.candidate) > 0 {
				b.pending, b.candidate = b.candidate, nil
				break
			}
			return 0, b.err
		}
		line, err := b.reader.ReadSlice('\n')
		whole := !b.continuation && err == nil
		b.continuation = err == bufio.ErrBufferFull
		if err != nil && err != bufio.ErrBufferFull {
			b.err = err
		}
		blank := whole && (string(line) == "\n" || string(line) == "\r\n")
		if len(b.candidate) > 0 {
			candidate := b.candidate
			b.candidate = nil
			b.frameStart = blank
			if blank {
				status := strings.TrimSuffix(strings.TrimSuffix(string(candidate), "\n"), "\r")
				b.status.observeCPAStatus(strings.TrimPrefix(status, ": cpa-status "))
				continue
			}
			b.pending = append(candidate, line...)
			continue
		}
		if b.frameStart && whole {
			value := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
			if value == ": cpa-status normal" || value == ": cpa-status degraded" {
				b.candidate = append([]byte(nil), line...)
				continue
			}
		}
		b.frameStart = blank
		b.pending = line
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}
