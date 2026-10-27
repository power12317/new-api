package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type headerRelayAdaptor struct {
	Adaptor
	url string
}

func (a headerRelayAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a headerRelayAdaptor) SetupRequestHeader(_ *gin.Context, headers *http.Header, info *relaycommon.RelayInfo) error {
	headers.Set("Authorization", "Bearer "+info.ApiKey)
	headers.Set("X-Adaptor-Default", "default")
	return nil
}

func TestRelayHeaderPassthroughPreservesHeadersAndAppliesOverrides(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name              string
		bodyPassthrough   bool
		headerPassthrough bool
		form              bool
		authorization     string
	}{
		{name: "headers without body passthrough", headerPassthrough: true},
		{name: "body and header passthrough", bodyPassthrough: true, headerPassthrough: true},
		{name: "existing body passthrough", bodyPassthrough: true},
		{name: "multipart passthrough", bodyPassthrough: true, headerPassthrough: true, form: true},
		{name: "explicit credential override", headerPassthrough: true, authorization: "Custom {api_key}"},
		{name: "ordinary request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type observedRequest struct {
				headers http.Header
				body    string
				err     error
			}
			received := make(chan observedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- observedRequest{headers: r.Header.Clone(), body: string(body), err: err}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			const payload = "{\n  \"model\": \"example\", \"extra\": {\"b\": false, \"a\": 0}\n}"
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/example", strings.NewReader(payload))
			ctx.Request.Header = http.Header{
				"Authorization": {"Bearer client-token"},
				"Api-Key":       {"client-azure-key"},
				"X-Api-Key":     {"client-anthropic-key"},
				"Content-Type":  {"application/json; charset=utf-8"},
				"User-Agent":    {"client-agent"},
				"X-Multi":       {"first", "second"},
				"X-Trace":       {"original"},
				"Cookie":        {"client-setting=kept"},
			}
			if tc.form {
				ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=original-boundary")
			}
			originalHeaders := ctx.Request.Header.Clone()
			overrides := map[string]any{
				"X-Trace":        "overridden",
				"X-Upstream-Key": "{api_key}",
				"X-Client":       "{client_header:X-Trace}",
			}
			if tc.headerPassthrough {
				overrides["*"] = true
				overrides["re:^x-multi$"] = true
			}
			if tc.authorization != "" {
				overrides["Authorization"] = tc.authorization
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:     constant.ChannelTypeOpenAI,
				ApiKey:          "upstream-key",
				HeadersOverride: overrides,
			}}
			info.ChannelSetting.PassThroughBodyEnabled = tc.bodyPassthrough
			adaptor := headerRelayAdaptor{url: server.URL}
			var response *http.Response
			var err error
			if tc.form {
				response, err = DoFormRequest(adaptor, ctx, info, strings.NewReader(payload))
			} else {
				response, err = DoApiRequest(adaptor, ctx, info, strings.NewReader(payload))
			}
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			request := <-received
			require.NoError(t, request.err)
			assert.Equal(t, payload, request.body)
			assert.Equal(t, "overridden", request.headers.Get("X-Trace"))
			assert.Equal(t, "upstream-key", request.headers.Get("X-Upstream-Key"))
			assert.Equal(t, "original", request.headers.Get("X-Client"))
			wantAuthorization := "Bearer upstream-key"
			if tc.authorization != "" {
				wantAuthorization = "Custom upstream-key"
			}
			assert.Equal(t, wantAuthorization, request.headers.Get("Authorization"))
			assert.Empty(t, request.headers.Get("Api-Key"))
			assert.Empty(t, request.headers.Get("X-Api-Key"))
			if tc.bodyPassthrough || tc.headerPassthrough {
				for _, name := range []string{"X-Multi", "User-Agent", "Content-Type", "Cookie"} {
					assert.Equal(t, originalHeaders.Values(name), request.headers.Values(name), name)
				}
				assert.Empty(t, request.headers.Get("X-Adaptor-Default"))
			} else {
				assert.Empty(t, request.headers.Get("X-Multi"))
				assert.Equal(t, "default", request.headers.Get("X-Adaptor-Default"))
			}
			assert.Equal(t, originalHeaders, ctx.Request.Header, "upstream overrides must not mutate the client headers")
		})
	}
}

func TestWebSocketHeaderPassthroughPreservesHeadersAndRebuildsHandshake(t *testing.T) {
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	ctx.Request.Header = http.Header{
		"Authorization":            {"Bearer client-token"},
		"X-Multi":                  {"first", "second"},
		"X-Trace":                  {"original"},
		"Connection":               {"Upgrade"},
		"Upgrade":                  {"websocket"},
		"Sec-Websocket-Key":        {"client-handshake-key"},
		"Sec-Websocket-Version":    {"13"},
		"Sec-Websocket-Extensions": {"permessage-deflate"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeOpenAI,
		ApiKey:      "upstream-key",
		HeadersOverride: map[string]any{
			"*":       true,
			"X-Trace": "overridden",
		},
	}}
	connection, err := DoWssRequest(headerRelayAdaptor{url: server.URL}, ctx, info, nil)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	headers := <-received
	assert.Equal(t, []string{"first", "second"}, headers.Values("X-Multi"))
	assert.Equal(t, "Bearer upstream-key", headers.Get("Authorization"))
	assert.Equal(t, "overridden", headers.Get("X-Trace"))
	assert.NotEqual(t, "client-handshake-key", headers.Get("Sec-Websocket-Key"))
}

func TestNewTaskAPIRequestInheritsClientCancellation(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)

	upstream, err := newTaskAPIRequest(c, "https://provider.example/tasks", nil)
	require.NoError(t, err)
	cancel()

	require.ErrorIs(t, upstream.Context().Err(), context.Canceled)
}

func TestPreparePassThroughRequestPreservesHeadersExceptCredentialAndHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "https://client.example/v1/responses", nil)
	ctx.Request.Header = http.Header{
		"Authorization":     {"Bearer client-token"},
		"Content-Type":      {"application/json; charset=utf-8"},
		"Accept":            {"text/event-stream", "application/json"},
		"X-Custom":          {"first", "second"},
		"X-Codex-Window-Id": {"window-1"},
		"Host":              {"client.example"},
	}

	upstream, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", nil)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ApiKey:      "upstream-token",
		},
	}

	require.NoError(t, preparePassThroughRequest(upstream, ctx, info))
	require.Equal(t, "upstream.example", upstream.Host)
	require.NotContains(t, upstream.Header, "Host")
	require.Equal(t, []string{"application/json; charset=utf-8"}, upstream.Header.Values("Content-Type"))
	require.Equal(t, []string{"text/event-stream", "application/json"}, upstream.Header.Values("Accept"))
	require.Equal(t, []string{"first", "second"}, upstream.Header.Values("X-Custom"))
	require.Equal(t, []string{"window-1"}, upstream.Header.Values("X-Codex-Window-Id"))
	require.Equal(t, "Bearer upstream-token", upstream.Header.Get("Authorization"))
}

func TestPreparePassThroughRequestUsesChannelCredentialHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name        string
		channelType int
		wantHeader  string
	}{
		{name: "azure", channelType: constant.ChannelTypeAzure, wantHeader: "api-key"},
		{name: "anthropic", channelType: constant.ChannelTypeAnthropic, wantHeader: "x-api-key"},
		{name: "gemini", channelType: constant.ChannelTypeGemini, wantHeader: "x-goog-api-key"},
		{name: "openai", channelType: constant.ChannelTypeOpenAI, wantHeader: "Authorization"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "https://client.example/v1", nil)
			ctx.Request.Header.Set("Authorization", "Bearer client-token")
			ctx.Request.Header.Set("api-key", "client-api-key")
			ctx.Request.Header.Set("x-api-key", "client-x-api-key")
			ctx.Request.Header.Set("x-goog-api-key", "client-goog-key")
			upstream, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1", nil)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: test.channelType,
					ApiKey:      "upstream-token",
				},
			}

			require.NoError(t, preparePassThroughRequest(upstream, ctx, info))
			wantValue := "upstream-token"
			if test.wantHeader == "Authorization" {
				wantValue = "Bearer upstream-token"
			}
			require.Equal(t, wantValue, upstream.Header.Get(test.wantHeader))
			for _, name := range []string{"Authorization", "api-key", "x-api-key", "x-goog-api-key"} {
				if name != test.wantHeader {
					require.Empty(t, upstream.Header.Get(name), name)
				}
			}
		})
	}
}

func TestRequestPassThroughEnabledByGlobalOrChannelSetting(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = original })

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	settings.PassThroughRequestEnabled = false
	info.ChannelSetting.PassThroughBodyEnabled = false
	assert.False(t, requestPassThroughEnabled(info))

	info.ChannelSetting.PassThroughBodyEnabled = true
	assert.True(t, requestPassThroughEnabled(info))

	info.ChannelSetting.PassThroughBodyEnabled = false
	settings.PassThroughRequestEnabled = true
	assert.True(t, requestPassThroughEnabled(info))
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

func TestToWebSocketURL(t *testing.T) {
	for input, want := range map[string]string{
		"https://api.openai.com/v1/responses":             "wss://api.openai.com/v1/responses",
		"http://127.0.0.1:3000/v1/responses":              "ws://127.0.0.1:3000/v1/responses",
		"wss://chatgpt.com/backend-api/codex/responses":   "wss://chatgpt.com/backend-api/codex/responses",
		"ws://127.0.0.1:3000/backend-api/codex/responses": "ws://127.0.0.1:3000/backend-api/codex/responses",
	} {
		assert.Equal(t, want, toWebSocketURL(input), input)
	}
}
