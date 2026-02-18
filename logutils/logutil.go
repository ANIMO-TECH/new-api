package logutils

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

var (
	logger     = zerolog.New(os.Stdout).With().Timestamp().Logger()
	loggerLock sync.RWMutex
)

const (
	FieldRequestID    = "request_id"
	FieldTraceID      = "trace_id"
	FieldSpanID       = "span_id"
	FieldTraceSampled = "trace_sampled"
)

type InitOptions struct {
	Writer         io.Writer
	Level          string
	Env            string
	ServiceName    string
	ServiceVersion string
}

func Init(opts InitOptions) {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	level := parseLevel(opts.Level)
	zerolog.SetGlobalLevel(level)
	zerolog.DisableSampling(true)
	zerolog.TimestampFieldName = "timestamp"
	zerolog.MessageFieldName = "body"
	zerolog.LevelFieldName = "severity_text"
	zerolog.CallerFieldName = "code.line"
	zerolog.TimeFieldFormat = "2006-01-02 15:04:05.000"

	l := zerolog.New(w).With().Timestamp().Logger().
		Hook(StaticHook{
			Env:            opts.Env,
			ServiceName:    opts.ServiceName,
			ServiceVersion: opts.ServiceVersion,
		}).
		Hook(CorrelationHook{})

	loggerLock.Lock()
	logger = l
	loggerLock.Unlock()
}

func Debug(ctx context.Context) *zerolog.Event {
	l := getLogger()
	return l.Debug().Ctx(normalizeCtx(ctx))
}

func Info(ctx context.Context) *zerolog.Event {
	l := getLogger()
	return l.Info().Ctx(normalizeCtx(ctx))
}

func Warn(ctx context.Context) *zerolog.Event {
	l := getLogger()
	return l.Warn().Ctx(normalizeCtx(ctx))
}

func Error(ctx context.Context) *zerolog.Event {
	l := getLogger()
	return l.Error().Ctx(normalizeCtx(ctx))
}

func getLogger() zerolog.Logger {
	loggerLock.RLock()
	defer loggerLock.RUnlock()
	return logger
}

func normalizeCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func parseLevel(raw string) zerolog.Level {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return zerolog.InfoLevel
	}
	lvl, err := zerolog.ParseLevel(raw)
	if err != nil {
		return zerolog.InfoLevel
	}
	return lvl
}
