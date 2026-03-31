package logutils

import (
	"context"
	"strings"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// GatewayOutcome captures the full picture of a single gateway relay request
// (which channel served it, retries, fallback, latency, etc.).
// Emitted once per request at the end of the Relay() flow.
type GatewayOutcome struct {
	Module            string // e.g. "relay", "relay_task"
	APIName           string // e.g. "chat_completions", "embeddings", "images"
	ModelName         string // original model name requested by the user
	UpstreamModelName string // actual model name sent to upstream (after mapping)
	ChannelID         int    // final channel that served the request (or last tried)
	ChannelName       string // human-readable channel name
	UseChannel        string // ordered list of channel IDs tried, e.g. "3->7->12"
	RetryCount        int    // number of retries (0 = first attempt succeeded)
	FallbackTriggered bool   // true if more than one channel was tried
	Success           bool
	StatusCode        int
	LatencyMs         int64   // total wall-clock time from request start to end
	FrtMs             float64 // first response time in ms (0 if not applicable)
	IsStream          bool
	PromptTokens      int
	CompletionTokens  int
	Group             string // token group / user group
}

// EmitGatewayOutcome writes a structured log line for the gateway outcome.
// On success it logs at INFO level; on failure it logs at ERROR level.
// Uses the existing logutils.Info / logutils.Error functions.
func EmitGatewayOutcome(ctx context.Context, o *GatewayOutcome) {
	if o == nil {
		return
	}

	emit := Info
	if !o.Success {
		emit = Error
	}

	evt := emit(ctx).
		Str("log_source", "gateway").
		Str("module", o.Module).
		Bool("alert", !o.Success).
		Str("api_name", o.APIName).
		Str("model_name", o.ModelName).
		Int("channel_id", o.ChannelID).
		Str("channel_name", o.ChannelName).
		Str("use_channel", o.UseChannel).
		Int("retry_count", o.RetryCount).
		Bool("fallback_triggered", o.FallbackTriggered).
		Bool("success", o.Success).
		Int("status_code", o.StatusCode).
		Int64("latency_ms", o.LatencyMs).
		Float64("frt_ms", o.FrtMs).
		Bool("is_stream", o.IsStream).
		Int("prompt_tokens", o.PromptTokens).
		Int("completion_tokens", o.CompletionTokens).
		Str("group", o.Group)

	if o.UpstreamModelName != "" && o.UpstreamModelName != o.ModelName {
		evt = evt.Str("upstream_model_name", o.UpstreamModelName)
	}

	evt.Msg("gateway_outcome")
}

// FormatUseChannel builds the "3->7->12" string from a slice of channel ID strings.
func FormatUseChannel(channels []string) string {
	if len(channels) == 0 {
		return ""
	}
	return strings.Join(channels, "->")
}

// RelayModeToAPIName maps a relay mode constant to a human-readable API name
// for the gateway outcome log.
func RelayModeToAPIName(mode int) string {
	switch mode {
	case relayconstant.RelayModeChatCompletions:
		return "chat_completions"
	case relayconstant.RelayModeCompletions:
		return "completions"
	case relayconstant.RelayModeEmbeddings:
		return "embeddings"
	case relayconstant.RelayModeModerations:
		return "moderations"
	case relayconstant.RelayModeImagesGenerations:
		return "images_generations"
	case relayconstant.RelayModeImagesEdits:
		return "images_edits"
	case relayconstant.RelayModeEdits:
		return "edits"
	case relayconstant.RelayModeAudioSpeech:
		return "audio_speech"
	case relayconstant.RelayModeAudioTranscription:
		return "audio_transcription"
	case relayconstant.RelayModeAudioTranslation:
		return "audio_translation"
	case relayconstant.RelayModeRerank:
		return "rerank"
	case relayconstant.RelayModeResponses:
		return "responses"
	case relayconstant.RelayModeResponsesCompact:
		return "responses_compact"
	case relayconstant.RelayModeRealtime:
		return "realtime"
	case relayconstant.RelayModeGemini:
		return "gemini"
	default:
		return "unknown"
	}
}
