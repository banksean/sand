package sandboxlog

import "context"

const SandboxIDAttrKey = "sandbox_id"

type sandboxIDContextKey struct{}

// WithSandboxID returns a context that carries a sandbox ID for downstream logging.
func WithSandboxID(ctx context.Context, sandboxID string) context.Context {
	if sandboxID == "" {
		return ctx
	}
	return context.WithValue(ctx, sandboxIDContextKey{}, sandboxID)
}

// SandboxIDFromContext returns the sandbox ID carried by ctx, if any.
func SandboxIDFromContext(ctx context.Context) (string, bool) {
	sandboxID, ok := ctx.Value(sandboxIDContextKey{}).(string)
	return sandboxID, ok && sandboxID != ""
}
