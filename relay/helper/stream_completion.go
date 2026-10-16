package helper

import (
	"errors"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

const streamCompletionKey = "deferred_stream_completion"

type streamCompletionFrame struct {
	event string
	data  string
}

type streamCompletion struct {
	mu     sync.Mutex
	frames []streamCompletionFrame
}

// BeginStreamCompletion keeps terminal events behind the synchronous consume-log write.
// Intermediate output continues to stream normally. A fresh state is used for each attempt.
func BeginStreamCompletion(c *gin.Context) {
	c.Set(streamCompletionKey, &streamCompletion{})
	common.SetContextKey(c, constant.ContextKeyConsumeLogPersisted, false)
}

func deferStreamCompletion(c *gin.Context, event, data string) bool {
	value, ok := c.Get(streamCompletionKey)
	if !ok {
		return false
	}
	state, ok := value.(*streamCompletion)
	if !ok {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.frames = append(state.frames, streamCompletionFrame{event: event, data: data})
	return true
}

// FinishStreamCompletion must run after the relay handler has settled and saved its log.
// A missing log produces a protocol error, never a successful completion or a new generation.
func FinishStreamCompletion(c *gin.Context) error {
	value, ok := c.Get(streamCompletionKey)
	if !ok {
		return nil
	}
	c.Set(streamCompletionKey, nil)
	state, ok := value.(*streamCompletion)
	if !ok {
		return nil
	}
	state.mu.Lock()
	frames := state.frames
	state.frames = nil
	state.mu.Unlock()
	if len(frames) == 0 {
		return nil
	}
	if requestContextDone(c) {
		return c.Request.Context().Err()
	}
	if !common.GetContextKeyBool(c, constant.ContextKeyConsumeLogPersisted) {
		message := common.MessageWithRequestId("Billing record could not be saved", c.GetString(common.RequestIdKey))
		detail := gin.H{"type": "billing_error", "code": "billing_log_write_failed", "message": message}
		payload := gin.H{"error": detail}
		event := ""
		if frames[0].event == "message_stop" {
			event = "error"
			payload["type"] = "error"
		} else if frames[0].event != "" {
			event = "error"
			payload = detail
			payload["type"] = "error"
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		if err := writeStreamCompletion(c, streamCompletionFrame{event: event, data: string(data)}); err != nil {
			return err
		}
		return errors.New("consume log was not persisted before stream completion")
	}
	for _, frame := range frames {
		if err := writeStreamCompletion(c, frame); err != nil {
			return err
		}
	}
	return nil
}

func writeStreamCompletion(c *gin.Context, frame streamCompletionFrame) error {
	if frame.event != "" {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", frame.event)})
	}
	c.Render(-1, common.CustomEvent{Data: "data: " + frame.data})
	return FlushWriter(c)
}
