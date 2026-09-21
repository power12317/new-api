package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
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

func TestUpstreamResponseLogTurnStateRules(t *testing.T) {
	header := func(length int) http.Header {
		return http.Header{"X-Codex-Turn-State": {strings.Repeat("a", length)}}
	}
	tests := []struct {
		name                    string
		code                    int
		response, request       http.Header
		status, source, verdict string
		length                  int
	}{
		{"normal 292", 200, header(292), nil, "normal", "response", "normal", 292},
		{"normal 332", 200, header(332), nil, "normal", "response", "normal", 332},
		{"degraded 312", 200, header(312), nil, "degraded", "response", "degraded", 312},
		{"degraded 356", 200, header(356), nil, "degraded", "response", "degraded", 356},
		{"request 292", 200, nil, header(292), "normal", "request", "normal", 292},
		{"request 332", 200, nil, header(332), "normal", "request", "normal", 332},
		{"request 312", 200, nil, header(312), "degraded", "request", "degraded", 312},
		{"request 356", 200, nil, header(356), "degraded", "request", "degraded", 356},
		{"response wins normal", 200, header(292), header(312), "normal", "response", "normal", 292},
		{"response wins degraded", 200, header(356), header(332), "degraded", "response", "degraded", 356},
		{"empty response prevents fallback", 200, header(0), header(312), "normal", "response", "unrecognized", 0},
		{"absent on both sides", 200, nil, nil, "normal", "none", "missing", -1},
		{"unrecognized length", 200, header(311), nil, "normal", "response", "unrecognized", 311},
		{"multiple values are not concatenated", 200, http.Header{"X-Codex-Turn-State": {strings.Repeat("a", 312), "b"}}, header(356), "normal", "response", "multiple", -1},
		{"case insensitive header", 200, http.Header{"x-CoDeX-turn-STATE": {strings.Repeat("a", 312)}}, nil, "degraded", "response", "degraded", 312},
		{"length counts bytes", 200, http.Header{"X-Codex-Turn-State": {strings.Repeat("é", 156)}}, nil, "degraded", "response", "degraded", 312},
		{"502 beats degraded", 502, header(312), nil, "error", "response", "degraded", 312},
		{"503 beats degraded", 503, header(356), nil, "error", "response", "degraded", 356},
		{"429 is error", 429, nil, nil, "error", "none", "missing", -1},
		{"other success code", 201, header(312), nil, "unknown", "response", "degraded", 312},
		{"redirect", 302, nil, nil, "unknown", "none", "missing", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RequestReasoningEffort: "high", ReasoningEffort: "low"}
			info.RecordUpstreamResponse(&http.Response{StatusCode: tc.code, Header: tc.response}, tc.request)
			other := model.NewLogOther()
			service.AppendUpstreamResponseLogInfo(info, other)
			var fields map[string]any
			require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &fields))
			assert.Equal(t, tc.status, fields["upstream_request_status"])
			assert.EqualValues(t, tc.code, fields["upstream_status_code"])
			assert.Equal(t, tc.source, fields["turn_state_source"])
			assert.Equal(t, tc.verdict, fields["turn_state_verdict"])
			assert.Equal(t, "high", fields["request_reasoning_effort"])
			assert.Equal(t, "", fields["response_model"])
			if tc.length < 0 {
				assert.NotContains(t, fields, "turn_state_length")
			} else {
				assert.EqualValues(t, tc.length, fields["turn_state_length"])
			}
			assert.NotContains(t, other.JSONString(), strings.Repeat("a", 200), "opaque header values must not enter logs")
		})
	}
}

func TestUpstreamResponseHTTPAttemptsDoNotLeakPreviousFacts(t *testing.T) {
	service.InitHttpClient()
	requestHeader := strings.Repeat("r", 292)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			w.Header().Set("X-Codex-Turn-State", strings.Repeat("s", 312))
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
		assert.Empty(t, info.UpstreamResponse.Model)
		if path == "/first" {
			assert.Equal(t, 503, info.UpstreamResponse.StatusCode)
			assert.Equal(t, "error", info.UpstreamResponse.Status)
			info.RecordResponseModel("first-model", true)
		} else {
			assert.Equal(t, 200, info.UpstreamResponse.StatusCode)
			assert.Equal(t, "normal", info.UpstreamResponse.Status)
			assert.Equal(t, "request", info.UpstreamResponse.TurnStateSource)
			assert.Equal(t, 292, *info.UpstreamResponse.TurnStateLength)
		}
	}
	info.InitChannelMeta(c)
	assert.Nil(t, info.UpstreamResponse)
	assert.Equal(t, "high", info.RequestReasoningEffort)
	server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("request"))
	require.NoError(t, err)
	_, err = channel.DoRequest(c, req, info)
	require.Error(t, err)
	other := model.NewLogOther()
	service.AppendUpstreamResponseLogInfo(info, other)
	assert.Equal(t, "error", other.Snapshot()["upstream_request_status"])
	assert.NotContains(t, other.Snapshot(), "upstream_status_code")
	assert.Empty(t, info.UpstreamResponse.Model)
}

func TestUpstreamResponseModelsAreObservedBeforeConversion(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	chatJSON := `{"id":"chat-1","model":"returned-model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
	responsesJSON := `{"id":"resp-1","model":"returned-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
	chatStream := "data: {\"id\":\"chat-1\",\"model\":\"returned-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"
	responsesStream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\",\"model\":\"initial-model\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":" + responsesJSON + "}\n\ndata: [DONE]\n\n"
	geminiJSON := `{"modelVersion":"returned-model","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`
	claudeJSON := `{"id":"msg-1","type":"message","model":"returned-model","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`
	tests := []struct {
		name, body string
		stream     bool
		format     types.RelayFormat
		handler    func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"gemini json", geminiJSON, false, types.RelayFormatOpenAI, gemini.GeminiChatHandler},
		{"gemini stream", "data: " + geminiJSON + "\n\n", true, types.RelayFormatOpenAI, gemini.GeminiChatStreamHandler},
		{"claude json", claudeJSON, false, types.RelayFormatClaude, func(c *gin.Context, info *relaycommon.RelayInfo, response *http.Response) (*dto.Usage, *types.NewAPIError) {
			return claude.ClaudeHandler(c, response, info)
		}},
		{"chat json", chatJSON, false, types.RelayFormatOpenAI, openai.OpenaiHandler},
		{"chat stream retains model before empty final chunk", chatStream, true, types.RelayFormatOpenAI, openai.OaiStreamHandler},
		{"responses json", responsesJSON, false, types.RelayFormatOpenAIResponses, openai.OaiResponsesHandler},
		{"responses compact", responsesJSON, false, types.RelayFormatOpenAIResponses, func(c *gin.Context, info *relaycommon.RelayInfo, response *http.Response) (*dto.Usage, *types.NewAPIError) {
			return openai.OaiResponsesCompactionHandler(c, response, info)
		}},
		{"responses stream final model wins", responsesStream, true, types.RelayFormatOpenAIResponses, openai.OaiResponsesStreamHandler},
		{"responses to chat json", responsesJSON, false, types.RelayFormatOpenAI, openai.OaiResponsesToChatHandler},
		{"responses to chat stream", responsesStream, true, types.RelayFormatOpenAI, openai.OaiResponsesToChatStreamHandler},
		{"buffered responses to chat", responsesStream, false, types.RelayFormatOpenAI, openai.OaiResponsesToChatBufferedStreamHandler},
		{"chat to responses json", chatJSON, false, types.RelayFormatOpenAIResponses, openai.OaiChatToResponsesHandler},
		{"chat to responses stream", chatStream, true, types.RelayFormatOpenAIResponses, openai.OaiChatToResponsesStreamHandler},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newUpstreamModelTestContext(tc.body, tc.stream)
			info.OriginModelName = "requested-model"
			info.ChannelMeta.UpstreamModelName = "mapped-model"
			info.RelayFormat = tc.format
			info.RelayMode = relayconstant.RelayModeChatCompletions
			if strings.HasPrefix(tc.name, "responses") {
				info.RelayMode = relayconstant.RelayModeResponses
			}
			info.RecordUpstreamResponse(resp, nil)
			usage, apiErr := tc.handler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 5, usage.TotalTokens)
			assert.Equal(t, "returned-model", info.UpstreamResponse.Model)
			assert.Contains(t, recorder.Body.String(), "hello")
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, -1)
			assert.Equal(t, "returned-model", other.Snapshot()["response_model"])
			assert.Equal(t, "requested-model", other.Snapshot()["request_model"])
		})
	}
}

func TestUpstreamResponseMissingModelDoesNotUseMappedModel(t *testing.T) {
	body := `{"id":"chat-1","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
	c, _, response, info := newUpstreamModelTestContext(body, false)
	info.ChannelMeta.UpstreamModelName = "mapped-model"
	info.OriginModelName = "requested-model"
	info.RelayFormat = types.RelayFormatOpenAI
	info.RecordUpstreamResponse(response, nil)
	_, apiErr := openai.OpenaiHandler(c, info, response)
	require.Nil(t, apiErr)
	other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, -1)
	assert.Equal(t, "", other.Snapshot()["response_model"])
}

func newUpstreamModelTestContext(body string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "response-model-test")
	contentType := "application/json"
	if stream || strings.HasPrefix(body, "data:") {
		contentType = "text/event-stream"
	}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, IsStream: stream, ShouldIncludeUsage: true, DisablePing: true}
	return c, recorder, response, info
}
