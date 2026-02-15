package tracing

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

var tracerProvider *sdktrace.TracerProvider

// Init initializes OpenTelemetry tracing exporter and global propagator.
func Init() error {
	if !common.GetEnvOrDefaultBool("OTEL_ENABLED", false) {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		return nil
	}

	exporterType := strings.ToLower(strings.TrimSpace(common.GetEnvOrDefaultString("OTEL_EXPORTER", "otlp-http")))
	endpoint := strings.TrimSpace(common.GetEnvOrDefaultString("OTEL_ENDPOINT", ""))
	if endpoint == "" {
		endpoint = strings.TrimSpace(common.GetEnvOrDefaultString("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	}

	var (
		exporter sdktrace.SpanExporter
		err      error
	)
	switch exporterType {
	case "otlp-grpc":
		exporter, err = newOTLPGRPCExporter(endpoint)
	case "otlp-http":
		exporter, err = newOTLPHTTPExporter(endpoint)
	default:
		common.SysError("unsupported OTEL_EXPORTER: " + exporterType + ", fallback to otlp-http")
		exporter, err = newOTLPHTTPExporter(endpoint)
	}
	if err != nil {
		return err
	}

	serviceName := strings.TrimSpace(common.GetEnvOrDefaultString("OTEL_SERVICE_NAME", "new-api"))
	serviceVersion := strings.TrimSpace(common.GetEnvOrDefaultString("OTEL_SERVICE_VERSION", common.Version))
	if serviceVersion == "" {
		serviceVersion = "unknown"
	}
	sampleRatio := parseSampleRatio(common.GetEnvOrDefaultString("OTEL_SAMPLE_RATIO", "1.0"))

	res, err := resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return err
	}

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second), sdktrace.WithMaxExportBatchSize(512)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return nil
}

func Shutdown(ctx context.Context) error {
	if tracerProvider == nil {
		return nil
	}
	return tracerProvider.Shutdown(ctx)
}

func newOTLPGRPCExporter(endpoint string) (sdktrace.SpanExporter, error) {
	host := normalizeEndpointHost(endpoint)
	if host == "" {
		host = "otel-collector.prod.api.dotai.internal:4317"
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(host)}
	if isInsecureEndpoint(endpoint) {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(context.Background(), opts...)
}

func newOTLPHTTPExporter(endpoint string) (sdktrace.SpanExporter, error) {
	host := normalizeEndpointHost(endpoint)
	if host == "" {
		host = "otel-collector.prod.api.dotai.internal:4318"
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if isInsecureEndpoint(endpoint) {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(context.Background(), opts...)
}

func normalizeEndpointHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = strings.TrimSuffix(raw, "/")
		if i := strings.IndexByte(raw, '/'); i >= 0 {
			raw = raw[:i]
		}
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSuffix(raw, "/")
	}
	if u.Host != "" {
		return u.Host
	}
	return strings.TrimSuffix(raw, "/")
}

func isInsecureEndpoint(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if !strings.Contains(raw, "://") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	return strings.EqualFold(u.Scheme, "http")
}

func parseSampleRatio(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 1
	}
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}
