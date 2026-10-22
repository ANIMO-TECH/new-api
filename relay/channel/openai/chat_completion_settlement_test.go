package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatSettlementContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ShouldIncludeUsage: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
	info.SetEstimatePromptTokens(44996)
	return c, w, info
}

func TestChatCompletionJSONRequiresUpstreamSettlement(t *testing.T) {
	cases := []struct {
		name, body string
		valid      bool
	}{
		{"missing usage", `{"choices":[{"message":{"content":"answer"}}]}`, false},
		{"zero output", `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`, false},
		{"negative input", `{"choices":[],"usage":{"prompt_tokens":-1,"completion_tokens":1}}`, false},
		{"valid usage", `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`, true},
		{"zero input is not replaced by estimate", `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":2,"total_tokens":2}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w, info := chatSettlementContext(t)
			usage, apiErr := OpenaiHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}})
			if !tc.valid {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.True(t, types.IsChannelError(apiErr))
				assert.Nil(t, usage)
				assert.Empty(t, w.Body.String())
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 2, usage.CompletionTokens)
				if strings.Contains(tc.name, "zero input") {
					assert.Zero(t, usage.PromptTokens)
				} else {
					assert.Equal(t, 10, usage.PromptTokens)
				}
			}
			assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
		})
	}
}

func TestChatCompletionStreamRequiresSettlementAndRejectsEmbeddedError(t *testing.T) {
	delta := `{"id":"chat-1","model":"gpt-test","choices":[{"delta":{"content":"partial answer"}}]}`
	usage := `{"id":"chat-1","model":"gpt-test","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":7}}}`
	cases := []struct {
		name    string
		events  []string
		valid   bool
		message string
	}{
		{"missing usage", []string{delta}, false, "valid settlement usage"},
		{"zero output", []string{delta, `{"usage":{"prompt_tokens":10,"completion_tokens":0}}`}, false, "valid settlement usage"},
		{"OpenRouter Flex error without type", []string{`{"error":{"code":502,"message":"Flex processing is temporarily unavailable.","metadata":{"error_type":"provider_unavailable"}}}`}, false, "Flex processing"},
		{"error after partial output", []string{delta, `{"error":{"code":"502","message":"upstream unavailable"}}`}, false, "upstream unavailable"},
		{"error after usage still fails", []string{delta, usage, `{"error":{"code":502,"message":"upstream failed after usage"}}`}, false, "upstream failed after usage"},
		{"malformed frame", []string{`{"choices":`}, false, "valid settlement usage"},
		{"valid final usage", []string{delta, usage}, true, ""},
		{"usage before empty terminal frame", []string{delta, usage, `{"id":"chat-1","model":"gpt-test","choices":[]}`}, true, ""},
		{"tool call with valid usage", []string{`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"search","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, usage}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w, info := chatSettlementContext(t)
			body := "data: " + strings.Join(tc.events, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
			got, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}})
			if !tc.valid {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Contains(t, apiErr.Error(), tc.message)
				assert.Nil(t, got)
				assert.True(t, info.StreamStatus.HasErrors())
				assert.NotContains(t, w.Body.String(), "[DONE]")
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, got)
				assert.Equal(t, 10, got.PromptTokens)
				assert.Equal(t, 2, got.CompletionTokens)
				assert.Equal(t, 7, got.PromptTokensDetails.CachedTokens)
				assert.Contains(t, w.Body.String(), "[DONE]")
			}
			assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
		})
	}
}

func TestChatCompletionHTTP200ErrorRetainsStatusForAutoDisable(t *testing.T) {
	oldEnabled := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = oldEnabled })
	oldCodes := operation_setting.AutomaticDisableStatusCodesToString()
	require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString("500-504"))
	t.Cleanup(func() { require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString(oldCodes)) })
	for _, code := range []string{`502`, `"502"`, `"provider_unavailable"`} {
		t.Run(code, func(t *testing.T) {
			c, w, info := chatSettlementContext(t)
			body := `{"error":{"code":` + code + `,"message":"Flex unavailable"}}`
			usage, apiErr := OpenaiHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}})
			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.True(t, service.ShouldDisableChannel(apiErr))
			assert.Empty(t, w.Body.String())
		})
	}
}

func TestChatCompletionClientCancellationDoesNotEstimateOrBan(t *testing.T) {
	c, _, info := chatSettlementContext(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Zero(t, usage.TotalTokens)
	assert.Zero(t, usage.PromptTokens)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
