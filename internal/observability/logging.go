package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// InitLogging configures OpenTelemetry log export when the process has
// explicit OTLP log exporter configuration in its environment. When enabled,
// it returns an slog.Handler that forwards records to the configured OTLP
// endpoint (e.g. an OpenTelemetry Collector); this handler is meant to be
// combined with the process's other slog handlers via sandboxlog.NewMultiHandler.
func InitLogging(ctx context.Context, serviceName string, attrs ...attribute.KeyValue) (slog.Handler, func(context.Context) error, bool, error) {
	if !logsEnabledFromEnv() {
		return nil, func(context.Context) error { return nil }, false, nil
	}

	exporter, err := otlploggrpc.New(ctx, logExporterOptionsFromEnv()...)
	if err != nil {
		return nil, func(context.Context) error { return nil }, false, err
	}

	res, err := traceResource(ctx, serviceName, attrs...)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		_ = exporter.Shutdown(ctx)
		return nil, func(context.Context) error { return nil }, false, err
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)

	handler := otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(provider))

	return handler, provider.Shutdown, true, nil
}

func logsEnabledFromEnv() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}

	logsExporter := strings.TrimSpace(os.Getenv("OTEL_LOGS_EXPORTER"))
	return logsExporter != "" && !strings.EqualFold(logsExporter, "none")
}

func logExporterOptionsFromEnv() []otlploggrpc.Option {
	var opts []otlploggrpc.Option

	if endpoint, ok := logEndpointFromEnv(); ok {
		if strings.Contains(endpoint, "://") {
			opts = append(opts, otlploggrpc.WithEndpointURL(endpoint))
		} else {
			opts = append(opts, otlploggrpc.WithEndpoint(endpoint))
			if logInsecureFromEnv() {
				opts = append(opts, otlploggrpc.WithInsecure())
			}
		}
		return opts
	}

	if logInsecureFromEnv() {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return opts
}

func logEndpointFromEnv() (string, bool) {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		if endpoint := strings.TrimSpace(os.Getenv(key)); endpoint != "" {
			return endpoint, true
		}
	}
	return "", false
}

func logInsecureFromEnv() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_LOGS_INSECURE",
		"OTEL_EXPORTER_OTLP_INSECURE",
	} {
		if strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true") {
			return true
		}
	}
	return false
}
