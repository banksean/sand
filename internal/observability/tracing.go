package observability

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const shutdownTimeout = 5 * time.Second

// InitTracing configures OpenTelemetry trace export when the process has
// explicit OTLP trace exporter configuration in its environment.
func InitTracing(ctx context.Context, serviceName string, attrs ...attribute.KeyValue) (func(context.Context) error, bool, error) {
	if !tracingEnabledFromEnv() {
		return func(context.Context) error { return nil }, false, nil
	}

	exporter, err := otlptracegrpc.New(ctx, traceExporterOptionsFromEnv()...)
	if err != nil {
		return func(context.Context) error { return nil }, false, err
	}

	res, err := traceResource(ctx, serviceName, attrs...)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		_ = exporter.Shutdown(ctx)
		return func(context.Context) error { return nil }, false, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, true, nil
}

func Shutdown(ctx context.Context, shutdown func(context.Context) error) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	return shutdown(shutdownCtx)
}

func tracingEnabledFromEnv() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}

	tracesExporter := strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER"))
	return tracesExporter != "" && !strings.EqualFold(tracesExporter, "none")
}

func traceExporterOptionsFromEnv() []otlptracegrpc.Option {
	var opts []otlptracegrpc.Option

	if endpoint, ok := traceEndpointFromEnv(); ok {
		if strings.Contains(endpoint, "://") {
			opts = append(opts, otlptracegrpc.WithEndpointURL(endpoint))
		} else {
			opts = append(opts, otlptracegrpc.WithEndpoint(endpoint))
			if traceInsecureFromEnv() {
				opts = append(opts, otlptracegrpc.WithInsecure())
			}
		}
		return opts
	}

	if traceInsecureFromEnv() {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return opts
}

func traceEndpointFromEnv() (string, bool) {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		if endpoint := strings.TrimSpace(os.Getenv(key)); endpoint != "" {
			return endpoint, true
		}
	}
	return "", false
}

func traceInsecureFromEnv() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
		"OTEL_EXPORTER_OTLP_INSECURE",
	} {
		if strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true") {
			return true
		}
	}
	return false
}

func traceResource(ctx context.Context, serviceName string, attrs ...attribute.KeyValue) (*resource.Resource, error) {
	attrs = append(defaultServiceName(serviceName), attrs...)
	return resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithFromEnv(),
		resource.WithAttributes(attrs...),
	)
}

func defaultServiceName(serviceName string) []attribute.KeyValue {
	if strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")) != "" {
		return nil
	}
	for _, resourceAttr := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		key, _, ok := strings.Cut(resourceAttr, "=")
		if ok && strings.TrimSpace(key) == "service.name" {
			return nil
		}
	}
	return []attribute.KeyValue{attribute.String("service.name", serviceName)}
}
