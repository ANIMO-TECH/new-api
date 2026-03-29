package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logutils"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason))

	logutils.Warn(context.Background()).
		Str("log_source", "gateway").
		Str("module", "channel_manage").
		Int("channel_id", channelError.ChannelId).
		Str("channel_name", channelError.ChannelName).
		Str("action", "auto_disable").
		Str("reason", reason).
		Bool("alert", true).
		Msg("channel auto disable triggered")

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		// Check which models are now fully unavailable
		unavailableModels := getModelsWithNoAvailableChannel(channelError.ChannelId)
		if len(unavailableModels) > 0 {
			modelList := strings.Join(unavailableModels, ", ")
			subject := fmt.Sprintf("通道「%s」（#%d）已被禁用，以下模型已无可用渠道", channelError.ChannelName, channelError.ChannelId)
			content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s\n以下模型已无可用渠道: %s", channelError.ChannelName, channelError.ChannelId, reason, modelList)
			NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
		}
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	// Snapshot before enabling: which models currently have no available channels
	modelsBefore := getModelsWithNoAvailableChannel(channelId)

	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		logutils.Warn(context.Background()).
			Str("log_source", "gateway").
			Str("module", "channel_manage").
			Int("channel_id", channelId).
			Str("channel_name", channelName).
			Str("action", "auto_enable").
			Str("reason", "channel test passed after auto-disable").
			Bool("alert", true).
			Msg("channel auto enable triggered")

		// Post-check: only notify models that were unavailable before AND are now available
		if len(modelsBefore) > 0 {
			modelsStillUnavailable := getModelsWithNoAvailableChannel(channelId)
			stillUnavailableSet := make(map[string]struct{}, len(modelsStillUnavailable))
			for _, m := range modelsStillUnavailable {
				stillUnavailableSet[m] = struct{}{}
			}
			var recovered []string
			for _, m := range modelsBefore {
				if _, still := stillUnavailableSet[m]; !still {
					recovered = append(recovered, m)
				}
			}
			if len(recovered) > 0 {
				modelList := strings.Join(recovered, ", ")
				subject := fmt.Sprintf("通道「%s」（#%d）已被启用，以下模型恢复可用", channelName, channelId)
				content := fmt.Sprintf("通道「%s」（#%d）已被启用，以下模型恢复可用: %s", channelName, channelId, modelList)
					NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
			}
		}
	}
}

func ShouldDisableChannel(channelType int, err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}
	//if err.StatusCode == http.StatusUnauthorized {
	//	return true
	//}
	if err.StatusCode == http.StatusForbidden {
		switch channelType {
		case constant.ChannelTypeGemini:
			return true
		}
	}
	oaiErr := err.ToOpenAIError()
	switch oaiErr.Code {
	case "invalid_api_key":
		return true
	case "account_deactivated":
		return true
	case "billing_not_active":
		return true
	case "pre_consume_token_quota_failed":
		return true
	case "Arrearage":
		return true
	}
	switch oaiErr.Type {
	case "insufficient_quota":
		return true
	case "insufficient_user_quota":
		return true
	// https://docs.anthropic.com/claude/reference/errors
	case "authentication_error":
		return true
	case "permission_error":
		return true
	case "forbidden":
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(status int, shouldBanChannel bool) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	// Strategy A: allow re-enable for auto-disabled channels when the latest
	// test does NOT hit a disable condition.
	if shouldBanChannel {
		return false
	}
	return true
}

// getModelsWithNoAvailableChannel returns models from the given channel
// that currently have no other enabled channels in any of the channel's groups.
// This is used to decide whether to send alerts:
// - On disable: alert only if some models become fully unavailable
// - On enable: alert only if some models were previously fully unavailable (recovery)
// Note: this is best-effort — concurrent channel state changes may cause
// occasional missed or extra alerts, which is acceptable for notification purposes.
func getModelsWithNoAvailableChannel(channelId int) []string {
	channel, err := model.CacheGetChannel(channelId)
	if err != nil || channel == nil {
		return nil
	}

	models := channel.GetModels()
	rawGroups := strings.Split(channel.Group, ",")
	groups := make([]string, 0, len(rawGroups))
	for _, g := range rawGroups {
		if trimmed := strings.TrimSpace(g); trimmed != "" {
			groups = append(groups, trimmed)
		}
	}

	seen := make(map[string]struct{})
	var unavailable []string

	for _, m := range models {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}

		hasAvailable := false
		for _, g := range groups {
			c := countEnabledForGroupModel(g, m)
			if c > 0 || c == -1 {
				// c == -1 means DB error, fail-open (assume available)
				hasAvailable = true
				break
			}
			// Fallback: check normalized model name (matches real routing logic)
			normalized := ratio_setting.FormatMatchingModelName(m)
			if normalized != m {
				nc := countEnabledForGroupModel(g, normalized)
				if nc > 0 || nc == -1 {
					hasAvailable = true
					break
				}
			}
		}

		if !hasAvailable {
			unavailable = append(unavailable, m)
		}
	}

	return unavailable
}

// countEnabledForGroupModel returns the number of enabled channels for a group+model.
// Uses memory cache when available, falls back to DB query.
// Returns -1 only on DB error (fail-open: caller should treat as "available").
func countEnabledForGroupModel(group string, modelName string) int64 {
	count := model.CacheCountEnabledChannelsForModel(group, modelName)
	if count >= 0 {
		return int64(count)
	}
	// Memory cache disabled, fall back to DB
	dbCount, dbErr := model.CountEnabledAbilitiesForModel(group, modelName)
	if dbErr != nil {
		logutils.Warn(context.Background()).
			Str("module", "channel_manage").
			Str("group", group).
			Str("model", modelName).
			Err(dbErr).
			Msg("failed to count enabled abilities, assuming available")
		return -1
	}
	return dbCount
}
