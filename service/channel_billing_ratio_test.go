package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBillingRatioMultipliesUsageExactlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, tc := range []struct {
		name  string
		fixed bool
		ratio float64
		want  int
	}{
		{"legacy default", false, 0, 2000}, {"discount", false, 0.5, 1000}, {"premium", false, 1.25, 2500},
		{"fixed price", true, 1.25, 62500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "channel-ratio-regression", ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 2, ModelPrice: 0.1, UsePrice: tc.fixed, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1, ChannelRatio: tc.ratio}}}
			summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})
			assert.Equal(t, tc.want, summary.Quota)
			other := GenerateTextOtherInfo(ctx, info, 1, summary.GroupRatio, 2, 0, 1, 0, -1)
			assert.Equal(t, float64(1), other["group_ratio"])
			if tc.ratio != 0 {
				assert.Equal(t, tc.ratio, other["channel_ratio"])
			}
		})
	}
}

func TestChannelFallbackReservesPremiumAndSettlesFinalChannel(t *testing.T) {
	for _, tiered := range []bool{false, true} {
		name := "ratio"
		if tiered {
			name = "expression"
		}
		t.Run(name, func(t *testing.T) {
			billing := &recordingBillingSettler{preConsumedQuota: 500}
			info := &relaycommon.RelayInfo{Billing: billing, FinalPreConsumedQuota: 500, PriceData: hosttypes.PriceData{QuotaBeforeGroup: 1000, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1, ChannelRatio: 0.5}}}
			if tiered {
				info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: `tier("base", p * 2)`, GroupRatio: 1, ChannelRatio: 0.5, EstimatedQuotaBeforeGroup: 1000, EstimatedQuotaAfterGroup: 500, QuotaPerUnit: 500000}
			}
			for _, r := range []float64{2, 1} {
				info.PriceData.GroupRatioInfo.ChannelRatio = r
				require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, info))
			}
			assert.Equal(t, []int{2000, 1000}, billing.reserveTargets)
			assert.Equal(t, 2000, info.FinalPreConsumedQuota, "cheaper fallback refunds only at settlement")
			if tiered {
				ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000})
				require.True(t, ok)
				assert.Equal(t, 1000, quota, "final channel, not peak reservation, determines charge")
			} else {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				info.PriceData.ModelRatio = 1
				assert.Equal(t, 1000, calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 1000, TotalTokens: 1000}).Quota)
			}
		})
	}
}
