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
