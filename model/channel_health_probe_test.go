package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelHealthProbeStatePreservesGenerationAndTargetsDisabledKey(t *testing.T) {
	truncateTables(t)
	channel := Channel{
		Name:   "multi-key-probe",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, RegisterChannelHealthProbe(channel.Id, "key-a", 60))

	states, err := ListDueChannelHealthProbes(common.GetTimestamp()+60, 10)
	require.NoError(t, err)
	require.Len(t, states, 1)
	firstGeneration := states[0].Generation
	assert.NotEmpty(t, firstGeneration)
	assert.NotEqual(t, "key-a", states[0].KeyFingerprint)

	resolved, key, eligible, err := ResolveChannelHealthProbeTarget(states[0])
	require.NoError(t, err)
	assert.Equal(t, channel.Id, resolved.Id)
	assert.Equal(t, "key-a", key)
	assert.True(t, eligible)

	require.NoError(t, RegisterChannelHealthProbe(channel.Id, "key-a", 120))
	current, err := IsCurrentChannelHealthProbeState(states[0])
	require.NoError(t, err)
	assert.False(t, current, "a repeated real disable must invalidate an in-flight probe result")
}

func TestRecordChannelHealthProbeFailureUsesGenerationCAS(t *testing.T) {
	truncateTables(t)
	channel := Channel{Name: "single-key-probe", Key: "secret", Status: common.ChannelStatusAutoDisabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, RegisterChannelHealthProbe(channel.Id, "secret", 1))
	states, err := ListDueChannelHealthProbes(common.GetTimestamp()+1, 10)
	require.NoError(t, err)
	require.Len(t, states, 1)

	updated, err := RecordChannelHealthProbeFailure(states[0], common.GetTimestamp()+120, "bad_response", "credential value must not be copied")
	require.NoError(t, err)
	assert.True(t, updated)

	var stored ChannelHealthProbeState
	require.NoError(t, DB.First(&stored, states[0].ID).Error)
	assert.Equal(t, 1, stored.ProbeAttempts)
	assert.Equal(t, 1, stored.ConsecutiveFailures)
	assert.Equal(t, "bad_response", stored.LastErrorCode)
}

func TestHealthProbePreservesMultilineSingleCredential(t *testing.T) {
	truncateTables(t)
	credential := "{\n  \"access_token\": \"test-access\",\n  \"account_id\": \"test-account\"\n}"
	channel := Channel{Name: "single-oauth-probe", Key: credential, Status: common.ChannelStatusAutoDisabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, RegisterChannelHealthProbe(channel.Id, credential, 1))
	require.NoError(t, EnsureAutoDisabledChannelHealthProbes(1))

	states, err := ListDueChannelHealthProbes(common.GetTimestamp()+1, 10)
	require.NoError(t, err)
	require.Len(t, states, 1)
	_, key, eligible, err := ResolveChannelHealthProbeTarget(states[0])
	require.NoError(t, err)
	assert.True(t, eligible)
	assert.Equal(t, credential, key)
	assert.Equal(t, ChannelKeyFingerprint(credential), states[0].KeyFingerprint)
}

func TestHealthProbeBackfillsFullCredentialAndRejectsLegacyFragment(t *testing.T) {
	truncateTables(t)
	credential := "{\n  \"access_token\": \"test-access\",\n  \"account_id\": \"test-account\"\n}"
	channel := Channel{Name: "legacy-split-oauth-probe", Key: credential, Status: common.ChannelStatusAutoDisabled}
	require.NoError(t, DB.Create(&channel).Error)
	// The previous scanner incorrectly registered only the first JSON line.
	require.NoError(t, RegisterChannelHealthProbe(channel.Id, "{", 1))
	require.NoError(t, EnsureAutoDisabledChannelHealthProbes(1))

	states, err := ListDueChannelHealthProbes(common.GetTimestamp()+1, 10)
	require.NoError(t, err)
	require.Len(t, states, 2)
	for _, state := range states {
		_, key, eligible, err := ResolveChannelHealthProbeTarget(state)
		require.NoError(t, err)
		if state.KeyFingerprint == ChannelKeyFingerprint("{") {
			assert.False(t, eligible)
			assert.Empty(t, key)
			removed, err := RemoveChannelHealthProbeState(state)
			require.NoError(t, err)
			assert.True(t, removed)
		} else {
			assert.True(t, eligible)
			assert.Equal(t, credential, key)
		}
	}
	states, err = ListDueChannelHealthProbes(common.GetTimestamp()+1, 10)
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, ChannelKeyFingerprint(credential), states[0].KeyFingerprint)
}

func TestHealthProbeBackfillStillSelectsOnlyAutoDisabledMultiKeys(t *testing.T) {
	truncateTables(t)
	channel := Channel{
		Name: "multi-key-backfill", Key: "key-a\nkey-b", Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, EnsureAutoDisabledChannelHealthProbes(1))
	states, err := ListDueChannelHealthProbes(common.GetTimestamp()+1, 10)
	require.NoError(t, err)
	require.Len(t, states, 1)
	_, key, eligible, err := ResolveChannelHealthProbeTarget(states[0])
	require.NoError(t, err)
	assert.True(t, eligible)
	assert.Equal(t, "key-b", key)
}
