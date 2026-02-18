package middleware

import (
	"time"

	"github.com/QuantumNous/new-api/logutils"
	"github.com/gin-gonic/gin"
)

func SetUpLogger(server *gin.Engine) {
	server.Use(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		event := logutils.Info(c.Request.Context())
		if statusCode >= 500 || len(c.Errors) > 0 {
			event = logutils.Error(c.Request.Context())
		} else if statusCode >= 400 {
			event = logutils.Warn(c.Request.Context())
		}

		event.
			Str("log_source", "http").
			Int("status_code", statusCode).
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", query).
			Str("client_ip", c.ClientIP()).
			Dur("latency", latency).
			Int("body_size", c.Writer.Size())
		if len(c.Errors) > 0 {
			event.Str("errors", c.Errors.String())
		}
		event.Msg("http request")
	})
}
