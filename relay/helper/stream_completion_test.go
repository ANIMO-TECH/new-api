package helper_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func billedStreamFixture(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	oldDB, oldLogs := model.DB, model.LOG_DB
	oldRedis, oldConsume, oldExport := common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogs
		common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldConsume, oldExport
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "stream-settlement-test")
	helper.SetEventStreamHeaders(c)
	return c, w, db
}

func TestStreamCompletionWaitsForQueryableConsumeLog(t *testing.T) {
	for _, tc := range []struct {
		name     string
		terminal string
		write    func(*gin.Context) error
	}{
		{"chat", "[DONE]", func(c *gin.Context) error { helper.Done(c); return nil }},
		{"responses", "response.completed", func(c *gin.Context) error {
			return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed"}`)
		}},
		{"claude", "message_stop", func(c *gin.Context) error {
			return helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w, db := billedStreamFixture(t)
			helper.BeginStreamCompletion(c)
			require.NoError(t, helper.StringData(c, `{"content":"progress"}`))
			assert.Contains(t, w.Body.String(), "progress", "text must keep streaming before settlement")
			require.NoError(t, tc.write(c))
			assert.NotContains(t, w.Body.String(), tc.terminal)
			model.RecordConsumeLog(c, 741, model.RecordConsumeLogParams{ModelName: "test", Quota: 10})
			require.NoError(t, helper.FinishStreamCompletion(c))
			assert.Contains(t, w.Body.String(), tc.terminal)
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("request_id = ? AND type = ?", "stream-settlement-test", model.LogTypeConsume).Count(&count).Error)
			assert.EqualValues(t, 1, count, "receipt must be queryable when completion is visible")
			completedBody := w.Body.String()
			require.NoError(t, helper.FinishStreamCompletion(c), "completion must not be sent twice")
			assert.Equal(t, completedBody, w.Body.String())
		})
	}
}

func TestFailedConsumeLogNeverSendsSuccessfulCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, terminal, errorFrame string
		write                      func(*gin.Context) error
	}{
		{"chat", "[DONE]", `data: {"error":`, func(c *gin.Context) error { helper.Done(c); return nil }},
		{"responses", "response.completed", "event: error\n", func(c *gin.Context) error {
			return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed"}`)
		}},
		{"claude", "message_stop", "event: error\n", func(c *gin.Context) error {
			return helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w, db := billedStreamFixture(t)
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail-log", func(tx *gorm.DB) {
				if tx.Statement.Table == "logs" {
					tx.AddError(errors.New("test log write failure"))
				}
			}))
			helper.BeginStreamCompletion(c)
			require.NoError(t, tc.write(c))
			model.RecordConsumeLog(c, 741, model.RecordConsumeLogParams{ModelName: "test", Quota: 10})
			require.Error(t, helper.FinishStreamCompletion(c))
			assert.NotContains(t, w.Body.String(), tc.terminal)
			assert.Contains(t, w.Body.String(), tc.errorFrame)
			assert.Contains(t, w.Body.String(), "billing_log_write_failed")
		})
	}
}

func TestUnmanagedStreamKeepsExistingCompletionBehavior(t *testing.T) {
	c, w, _ := billedStreamFixture(t)
	helper.Done(c)
	assert.Contains(t, w.Body.String(), "[DONE]")
}
