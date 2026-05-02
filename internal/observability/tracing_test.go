package observability

import "testing"

func TestTracingEnabledFromEnv(t *testing.T) {
	clearTracingEnv(t)
	if tracingEnabledFromEnv() {
		t.Fatal("tracing should be disabled without OTLP env configuration")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	if !tracingEnabledFromEnv() {
		t.Fatal("tracing should be enabled when OTEL_EXPORTER_OTLP_ENDPOINT is set")
	}
}

func TestTracingEnabledFromEnvWithNoneExporter(t *testing.T) {
	clearTracingEnv(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	if tracingEnabledFromEnv() {
		t.Fatal("tracing should be disabled when OTEL_TRACES_EXPORTER=none")
	}
}

func TestDefaultServiceNameHonorsEnvironment(t *testing.T) {
	clearTracingEnv(t)
	if attrs := defaultServiceName("sand-cli"); len(attrs) != 1 || attrs[0].Key != "service.name" {
		t.Fatalf("expected default service.name attribute, got %#v", attrs)
	}

	t.Setenv("OTEL_SERVICE_NAME", "custom")
	if attrs := defaultServiceName("sand-cli"); len(attrs) != 0 {
		t.Fatalf("expected OTEL_SERVICE_NAME to suppress default, got %#v", attrs)
	}

	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=local,service.name=custom")
	if attrs := defaultServiceName("sand-cli"); len(attrs) != 0 {
		t.Fatalf("expected OTEL_RESOURCE_ATTRIBUTES service.name to suppress default, got %#v", attrs)
	}
}

func TestTraceEndpointFromEnv(t *testing.T) {
	clearTracingEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	endpoint, ok := traceEndpointFromEnv()
	if !ok || endpoint != "localhost:4317" {
		t.Fatalf("expected global endpoint, got endpoint=%q ok=%v", endpoint, ok)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "tempo.dev.local:4317")
	endpoint, ok = traceEndpointFromEnv()
	if !ok || endpoint != "tempo.dev.local:4317" {
		t.Fatalf("expected traces endpoint to take precedence, got endpoint=%q ok=%v", endpoint, ok)
	}
}

func TestTraceInsecureFromEnv(t *testing.T) {
	clearTracingEnv(t)
	if traceInsecureFromEnv() {
		t.Fatal("expected tracing exporter to default to secure")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	if !traceInsecureFromEnv() {
		t.Fatal("expected global insecure env to be honored")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	if !traceInsecureFromEnv() {
		t.Fatal("expected traces insecure env to be honored")
	}
}

func clearTracingEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_INSECURE",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
		"OTEL_TRACES_EXPORTER",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
	} {
		t.Setenv(key, "")
	}
}
