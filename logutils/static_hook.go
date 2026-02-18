package logutils

import "github.com/rs/zerolog"

// StaticHook injects static service metadata into every log record.
type StaticHook struct {
	Env            string
	ServiceName    string
	ServiceVersion string
}

func (h StaticHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	if e == nil {
		return
	}
	if h.Env != "" {
		e.Str("env", h.Env)
	}
	if h.ServiceName != "" {
		e.Str("service_name", h.ServiceName)
	}
	if h.ServiceVersion != "" {
		e.Str("service_version", h.ServiceVersion)
	}
}
