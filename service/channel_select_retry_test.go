package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createChannelSelectRetryChannel(t *testing.T, db *gorm.DB, id int, priority int64) {
	t.Helper()

	const modelName = "retry-same-priority-model"
	const group = "default"
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
		Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestRetryExhaustsUntriedChannelsAtCurrentPriorityBeforeFallback(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	common.MemoryCacheEnabled = true

	createChannelSelectRetryChannel(t, db, 3101, 100)
	createChannelSelectRetryChannel(t, db, 3102, 100)
	createChannelSelectRetryChannel(t, db, 3103, 100)
	createChannelSelectRetryChannel(t, db, 3199, 1)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "default",
		ModelName:   "retry-same-priority-model",
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}
	ctx.Set("use_channel", []string{"3101"})

	highPrioritySelections := map[int]struct{}{3101: {}}
	for range 2 {
		channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "default", selectedGroup)
		assert.Contains(t, []int{3101, 3102, 3103}, channel.Id)
		highPrioritySelections[channel.Id] = struct{}{}
		param.IncreaseRetry()
	}
	assert.Len(t, highPrioritySelections, 3)

	fallback, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, fallback)
	assert.Equal(t, "default", selectedGroup)
	assert.Equal(t, 3199, fallback.Id)
}
