package middleware

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		var traceID string
		if param.Keys != nil {
			if v, ok := param.Keys[common.RequestIdKey]; ok {
				if s, ok := v.(string); ok {
					requestID = s
				}
			}
			if v, ok := param.Keys[common.TraceIdKey]; ok {
				if s, ok := v.(string); ok {
					traceID = s
				}
			}
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			requestID,
			traceID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			param.Path,
		)
	}))
}
