package logutils

import (
	"context"
	"strings"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

const (
	requestIDHeader = "X-Oneapi-Request-Id"
	traceIDHeader   = "X-Trace-Id"
)

// CorrelationHook injects request_id/trace_id/span_id from context.
type CorrelationHook struct{}

func (CorrelationHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	if e == nil {
		return
	}
	ctx := e.GetCtx()
	if ctx == nil {
		return
	}

	if requestID, ok := requestIDFromContext(ctx); ok {
		e.Str(FieldRequestID, requestID)
	}

	if traceID, ok := traceIDFromContext(ctx); ok {
		e.Str(FieldTraceID, traceID)
	}

	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		e.Str(FieldSpanID, sc.SpanID().String())
		e.Bool(FieldTraceSampled, sc.IsSampled())
		if _, hasTrace := traceIDFromContext(ctx); !hasTrace {
			e.Str(FieldTraceID, sc.TraceID().String())
		}
	}
}

func requestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if v, ok := ctx.Value(requestIDHeader).(string); ok {
		v = strings.TrimSpace(v)
		if v != "" && v != "-" {
			return v, true
		}
	}
	return "", false
}

func traceIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if v, ok := ctx.Value(traceIDHeader).(string); ok {
		v = strings.TrimSpace(v)
		if v != "" && v != "-" {
			return v, true
		}
	}
	return "", false
}
