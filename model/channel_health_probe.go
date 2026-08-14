package model

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ChannelHealthProbeState is the Advoo recovery state for one channel key.
// KeyFingerprint deliberately avoids persisting channel credentials a second time.
type ChannelHealthProbeState struct {
	ID                  int64  `json:"id" gorm:"primaryKey"`
	ChannelID           int    `json:"channel_id" gorm:"uniqueIndex:idx_channel_probe_target;index"`
	KeyFingerprint      string `json:"key_fingerprint" gorm:"type:varchar(64);uniqueIndex:idx_channel_probe_target"`
	Generation          string `json:"generation" gorm:"type:varchar(64);index"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	ProbeAttempts       int    `json:"probe_attempts"`
	NextProbeAt         int64  `json:"next_probe_at" gorm:"bigint;index"`
	LastProbeAt         int64  `json:"last_probe_at" gorm:"bigint"`
	LastSuccessAt       int64  `json:"last_success_at" gorm:"bigint"`
	LastErrorCode       string `json:"last_error_code" gorm:"type:varchar(128)"`
	LastErrorMessage    string `json:"last_error_message" gorm:"type:varchar(512)"`
	ExhaustedNotified   bool   `json:"exhausted_notified"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint"`
}

func ChannelKeyFingerprint(key string) string {
	return hex.EncodeToString(common.Sha256Raw([]byte(key)))
}

func RegisterChannelHealthProbe(channelID int, usingKey string, initialDelaySeconds int64) error {
	channel, err := GetChannelById(channelID, true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(usingKey) == "" && !channel.ChannelInfo.IsMultiKey {
		usingKey = channel.Key
	}
	if strings.TrimSpace(usingKey) == "" {
		return errors.New("health probe target key is empty")
	}
	if initialDelaySeconds < 1 {
		initialDelaySeconds = 1
	}
	generation, err := common.GenerateRandomCharsKey(24)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	state := ChannelHealthProbeState{}
	fingerprint := ChannelKeyFingerprint(usingKey)
	err = DB.Where("channel_id = ? AND key_fingerprint = ?", channelID, fingerprint).First(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state = ChannelHealthProbeState{
			ChannelID:      channelID,
			KeyFingerprint: fingerprint,
			Generation:     generation,
			NextProbeAt:    now + initialDelaySeconds,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		return DB.Create(&state).Error
	}
	// A fresh real-request disable starts a new generation and invalidates any
	// in-flight result from the previous recovery cycle.
	return DB.Model(&state).Updates(map[string]any{
		"generation":           generation,
		"consecutive_failures": 0,
		"probe_attempts":       0,
		"next_probe_at":        now + initialDelaySeconds,
		"last_probe_at":        0,
		"last_success_at":      0,
		"last_error_code":      "",
		"last_error_message":   "",
		"exhausted_notified":   false,
		"updated_at":           now,
	}).Error
}

// EnsureAutoDisabledChannelHealthProbes backfills channels that were already
// disabled when the Advoo mechanism was enabled. Existing generations are left untouched.
func EnsureAutoDisabledChannelHealthProbes(initialDelaySeconds int64) error {
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusAutoDisabled).Find(&channels).Error; err != nil {
		return err
	}
	for _, channel := range channels {
		keys := channel.GetKeys()
		for index, key := range keys {
			if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeyStatusList[index] != common.ChannelStatusAutoDisabled {
				continue
			}
			var count int64
			fingerprint := ChannelKeyFingerprint(key)
			if err := DB.Model(&ChannelHealthProbeState{}).
				Where("channel_id = ? AND key_fingerprint = ?", channel.Id, fingerprint).
				Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := RegisterChannelHealthProbe(channel.Id, key, initialDelaySeconds); err != nil {
					return err
				}
			}
			if !channel.ChannelInfo.IsMultiKey {
				break
			}
		}
	}
	return nil
}

func ListDueChannelHealthProbes(now int64, limit int) ([]*ChannelHealthProbeState, error) {
	if limit <= 0 {
		limit = 100
	}
	var states []*ChannelHealthProbeState
	err := DB.Where("next_probe_at > 0 AND next_probe_at <= ?", now).
		Order("next_probe_at asc, id asc").Limit(limit).Find(&states).Error
	return states, err
}

func HasDueChannelHealthProbe(now int64) bool {
	var count int64
	return DB.Model(&ChannelHealthProbeState{}).
		Where("next_probe_at > 0 AND next_probe_at <= ?", now).
		Limit(1).Count(&count).Error == nil && count > 0
}

func ResolveChannelHealthProbeTarget(state *ChannelHealthProbeState) (*Channel, string, bool, error) {
	if state == nil {
		return nil, "", false, errors.New("health probe state is nil")
	}
	channel, err := GetChannelById(state.ChannelID, true)
	if err != nil {
		return nil, "", false, err
	}
	for _, key := range channel.GetKeys() {
		if ChannelKeyFingerprint(key) != state.KeyFingerprint {
			continue
		}
		if channel.ChannelInfo.IsMultiKey {
			keyIndex := -1
			for index, candidate := range channel.GetKeys() {
				if candidate == key {
					keyIndex = index
					break
				}
			}
			if keyIndex < 0 || channel.ChannelInfo.MultiKeyStatusList[keyIndex] != common.ChannelStatusAutoDisabled {
				return channel, key, false, nil
			}
		} else if channel.Status != common.ChannelStatusAutoDisabled {
			return channel, key, false, nil
		}
		return channel, key, true, nil
	}
	return channel, "", false, nil
}

func RecordChannelHealthProbeFailure(state *ChannelHealthProbeState, nextProbeAt int64, errorCode string, _ string) (bool, error) {
	if state == nil {
		return false, errors.New("health probe state is nil")
	}
	now := common.GetTimestamp()
	result := DB.Model(&ChannelHealthProbeState{}).
		Where("id = ? AND generation = ?", state.ID, state.Generation).
		Updates(map[string]any{
			"consecutive_failures": state.ConsecutiveFailures + 1,
			"probe_attempts":       state.ProbeAttempts + 1,
			"next_probe_at":        nextProbeAt,
			"last_probe_at":        now,
			"last_error_code":      errorCode,
			"last_error_message":   "probe request failed",
			"updated_at":           now,
		})
	return result.RowsAffected == 1, result.Error
}

func RemoveChannelHealthProbeState(state *ChannelHealthProbeState) (bool, error) {
	if state == nil {
		return false, errors.New("health probe state is nil")
	}
	result := DB.Where("id = ? AND generation = ?", state.ID, state.Generation).
		Delete(&ChannelHealthProbeState{})
	return result.RowsAffected == 1, result.Error
}

func IsCurrentChannelHealthProbeState(state *ChannelHealthProbeState) (bool, error) {
	if state == nil {
		return false, errors.New("health probe state is nil")
	}
	var count int64
	err := DB.Model(&ChannelHealthProbeState{}).
		Where("id = ? AND generation = ?", state.ID, state.Generation).
		Count(&count).Error
	return count == 1, err
}

func ClearChannelHealthProbeStates(channelID int) error {
	return DB.Where("channel_id = ?", channelID).Delete(&ChannelHealthProbeState{}).Error
}

func ClearChannelHealthProbeTarget(channelID int, usingKey string) error {
	if strings.TrimSpace(usingKey) == "" {
		return ClearChannelHealthProbeStates(channelID)
	}
	return DB.Where("channel_id = ? AND key_fingerprint = ?", channelID, ChannelKeyFingerprint(usingKey)).
		Delete(&ChannelHealthProbeState{}).Error
}
