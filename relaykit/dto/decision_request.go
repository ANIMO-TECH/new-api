package dto

import (
	"encoding/json"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// DecisionRequest is an OpenRouter decision model request (/api/alpha/decisions).
// RawBody preserves the original JSON so state and questions are forwarded intact.
type DecisionRequest struct {
	Model   string          `json:"model"`
	RawBody json.RawMessage `json:"-"`
}

func (r *DecisionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		CombineText: string(r.RawBody),
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *DecisionRequest) IsStream(_ *http.Request) bool {
	return false
}

func (r *DecisionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

// DecisionUsage is the usage block of a decision response; output tokens are the scores.
type DecisionUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
