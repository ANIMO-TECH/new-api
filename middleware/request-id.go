package middleware

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader(common.RequestIdKey))
		if id == "" {
			id = common.GetTimeString() + common.GetRandomString(8)
		}
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Request.Header.Set(common.RequestIdKey, id)
		c.Header(common.RequestIdKey, id)
		c.Next()
	}
}
