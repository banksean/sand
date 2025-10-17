# Agent Guidelines for Sand

## Build & Test Commands
- **Build**: `go build -v ./...` or `./build.sh` (adds version info)
- **Test all**: `GOEXPERIMENT=synctest go test ./...`
- **Test single package**: `GOEXPERIMENT=synctest go test ./path/to/package`
- **Test with race**: `GOEXPERIMENT=synctest go test -race ./...`
- **Test short (skip slow)**: `GOEXPERIMENT=synctest go test -short ./...`
- **Format check**: `scripts/run-formatters.sh check`
- **Format fix**: `scripts/run-formatters.sh fix` or `gofumpt -w .`
- **Generate code**: `go generate ./...` (runs sqlc, must have sqlc installed)

## Code Style
- **Formatting**: Use `gofumpt` (stricter than gofmt), enforced in CI
- **Imports**: Standard library first, then third-party, then local packages (e.g., `github.com/banksean/sand/...`)
- **Error handling**: Wrap errors with `fmt.Errorf("context: %w", err)`, use `errors.Join()` for multiple errors
- **Logging**: Use `log/slog` with context: `slog.InfoContext(ctx, "message", "key", value)`
- **Naming**: Camel case for exported (`NewBoxer`), lower camel for unexported (`hydrateBox`)
- **SQL**: Schema in `db/schema.sql`, queries in `db/queries.sql`, generate with `sqlc generate`
