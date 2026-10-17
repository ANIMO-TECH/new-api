package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChannelBillingRatioSettingValidation(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
		want  float64
	}{
		{`{}`, true, 1}, {`{"billing_ratio":null}`, true, 1}, {`{"billing_ratio":1}`, true, 1},
		{`{"billing_ratio":0.01}`, true, .01}, {`{"billing_ratio":1.25}`, true, 1.25}, {`{"billing_ratio":1000}`, true, 1000},
		{`{"billing_ratio":0}`, false, 1}, {`{"billing_ratio":-1}`, false, 1}, {`{"billing_ratio":1000.01}`, false, 1}, {`{"billing_ratio":1.001}`, false, 1}, {`{"billing_ratio":1.00000000001}`, false, 1}, {`{"billing_ratio":2.55}`, true, 2.55},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c := &Channel{Setting: &tc.body}
			if tc.valid {
				require.NoError(t, c.ValidateSettings())
			} else {
				require.Error(t, c.ValidateSettings())
			}
			var setting dto.ChannelSettings
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &setting))
			assert.Equal(t, tc.want, setting.BillingMultiplier())
		})
	}
}
