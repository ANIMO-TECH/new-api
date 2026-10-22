package openai

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func invalidChatCompletionSettlementError() *types.NewAPIError {
	return types.NewOpenAIError(fmt.Errorf("chat completions upstream did not return valid settlement usage"), types.ErrorCodeChannelInvalidResponse, http.StatusBadGateway)
}

// Match Responses: a successful generation must carry real output usage.
// Tool-call and reasoning tokens are included in completion_tokens by upstream.
func validateChatCompletionSettlement(usage *dto.Usage) *types.NewAPIError {
	if usage == nil || usage.CompletionTokens <= 0 || usage.PromptTokens < 0 || usage.TotalTokens < 0 {
		return invalidChatCompletionSettlementError()
	}
	return nil
}

// Errors embedded in an HTTP 200 body must retain an error HTTP status for
// channel health handling. OpenRouter may omit type and use a numeric code.
func chatCompletionUpstreamError(upstream *types.OpenAIError) *types.NewAPIError {
	status := http.StatusBadGateway
	if code, err := strconv.Atoi(fmt.Sprint(upstream.Code)); err == nil && code >= 400 && code <= 599 {
		status = code
	}
	return types.WithOpenAIError(*upstream, status)
}
