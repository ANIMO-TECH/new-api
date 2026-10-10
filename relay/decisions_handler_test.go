package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestDecisionBillingUsage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want dto.Usage
	}{
		{
			name: "reported tokens",
			body: `{"answers":{},"usage":{"input_tokens":313,"output_tokens":31,"cost":0.000013}}`,
			want: dto.Usage{PromptTokens: 313, CompletionTokens: 31, TotalTokens: 344},
		},
		{
			name: "missing usage uses the estimate",
			body: `{"answers":{}}`,
			want: dto.Usage{PromptTokens: 120, TotalTokens: 120},
		},
		{
			name: "negative counts never become a credit",
			body: `{"usage":{"input_tokens":-5000,"output_tokens":-1}}`,
			want: dto.Usage{PromptTokens: 120, TotalTokens: 120},
		},
		{
			name: "unreadable body uses the estimate",
			body: `not json`,
			want: dto.Usage{PromptTokens: 120, TotalTokens: 120},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, &tt.want, decisionBillingUsage([]byte(tt.body), 120))
		})
	}
}
