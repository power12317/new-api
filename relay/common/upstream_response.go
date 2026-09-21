package common

import (
	"net/http"
	"strings"
)

// UpstreamResponseInfo describes one HTTP attempt, before status mapping or
// response conversion. Turn-state contents are deliberately never retained.
type UpstreamResponseInfo struct {
	StatusCode       int
	Status           string
	TurnStateSource  string
	TurnStateLength  *int
	TurnStateVerdict string
}

// RecordUpstreamResponse replaces the previous attempt's facts. A nil response
// records a transport failure, not a fabricated HTTP status code.
func (info *RelayInfo) RecordUpstreamResponse(response *http.Response, requestHeaders http.Header) {
	if info == nil {
		return
	}
	result := &UpstreamResponseInfo{Status: "error", TurnStateSource: "none", TurnStateVerdict: "missing"}
	info.UpstreamResponse = result
	if response == nil {
		return
	}
	result.StatusCode = response.StatusCode
	switch {
	case response.StatusCode == http.StatusOK:
		result.Status = "normal"
	case response.StatusCode >= 400 && response.StatusCode <= 599:
		result.Status = "error"
	default:
		result.Status = "unknown"
	}

	for _, candidate := range []struct {
		source  string
		headers http.Header
	}{{"response", response.Header}, {"request", requestHeaders}} {
		present := false
		var values []string
		for name, headerValues := range candidate.headers {
			if strings.EqualFold(name, "x-codex-turn-state") {
				present = true
				values = append(values, headerValues...)
			}
		}
		if !present {
			continue
		}
		result.TurnStateSource = candidate.source
		if len(values) > 1 {
			result.TurnStateVerdict = "multiple"
			return
		}
		length := 0
		if len(values) == 1 {
			length = len(values[0])
		}
		result.TurnStateLength = &length
		switch length {
		case 292, 332:
			result.TurnStateVerdict = "normal"
		case 312, 356:
			result.TurnStateVerdict = "degraded"
			if result.StatusCode == http.StatusOK {
				result.Status = "degraded"
			}
		default:
			result.TurnStateVerdict = "unrecognized"
		}
		return
	}
}
