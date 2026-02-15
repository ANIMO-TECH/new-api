package middleware

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const traceParentHeader = "traceparent"

func TraceId() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := normalizeTraceID(c.GetHeader(common.TraceIdKey))
		if traceID == "" {
			traceID = parseTraceIDFromTraceParent(c.GetHeader(traceParentHeader))
		}
		if traceID != "" {
			c.Set(common.TraceIdKey, traceID)
			ctx := context.WithValue(c.Request.Context(), common.TraceIdKey, traceID)
			c.Request = c.Request.WithContext(ctx)
			c.Request.Header.Set(common.TraceIdKey, traceID)
			c.Header(common.TraceIdKey, traceID)
		}
		c.Next()
	}
}

func parseTraceIDFromTraceParent(traceparent string) string {
	traceparent = strings.TrimSpace(traceparent)
	if traceparent == "" {
		return ""
	}
	parts := strings.Split(traceparent, "-")
	if len(parts) < 4 {
		return ""
	}
	return normalizeTraceID(parts[1])
}

func normalizeTraceID(value string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "-", "")
	if len(v) != 32 {
		return ""
	}
	for _, ch := range v {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	if v == "00000000000000000000000000000000" {
		return ""
	}
	return v
}
