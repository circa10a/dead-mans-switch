# Agent Guidelines for Dead Man's Switch

This doc provides guidelines for AI agents working on this codebase to maintain consistency and quality standards. I hate that I have to add this, but it's 2026 :sigh:

## Project structure

```
cmd/                        Cobra CLI commands (root, server, switch)
internal/server/            Core server initialization, worker loop, banner
  database/                 SQLite store implementing the Store interface
  handlers/                 HTTP handlers — one struct per resource (Switch, Auth, Health)
  middleware/               JWT auth, logging, Prometheus, request validation
  secrets/                  Encryption key and VAPID key management
  web/                      Embedded SPA (Alpine.js + Tailwind CSS)
  docs/                     Generated API documentation HTML
api/                        OpenAPI spec, codegen config, generated Go SDK
deploy/                     Docker Compose, monitoring stack, K8s manifests
```

### Libraries

Do not introduce alternatives to these without discussion:

| Library | Purpose |
|---|---|
| `go-chi/chi` | HTTP router |
| `spf13/cobra` + `spf13/viper` | CLI + config (env vars use `DEAD_MANS_SWITCH_` prefix) |
| `charmbracelet/log` | slog handler (text/JSON output) |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) |
| `nicholas-fedor/shoutrrr` | Multi-provider notifications |
| `go-playground/validator` | Request body validation |

## Generated Code

The following files are **all generated** — never modify them by hand:

| File(s) | Regenerate with |
|---|---|
| `api/gen.go` | `go generate ./...` (from `api/openapi.yaml`) |
| `api/openapi.internal.gen.yaml`, `api/openapi.external.gen.yaml` | `make docs` |
| `internal/server/docs/api.internal.html`, `internal/server/docs/api.public.html` | `make docs` |

## Build Pipeline

`make build` runs these steps in order:

1. **Codegen** — `go generate ./...` (regenerates `api/gen.go`)
2. **Assets** — Tailwind CSS compilation + Alpine.js download (requires Docker)
3. **Docs** — Redocly API doc rendering (requires Docker)
4. **Compile** — `go build` with `-ldflags` injecting `cmd.version`, `cmd.commit`, `cmd.date`

### Key Make Targets

| Target | Purpose |
|---|---|
| `make build` | Full build pipeline → `bin/dead-mans-switch` |
| `make test` | Run tests with coverage (excludes `api/gen.go` from report) |
| `make lint` | `golangci-lint` via Docker |
| `make sdk` | Regenerate only `api/gen.go` |
| `make run` | Build + start the server |
| `make monitoring` / `monitoring-down` | Start/stop Prometheus + Grafana + Loki stack |
| `make auth` / `auth-down` | Start/stop local Authentik OIDC provider |

## Version control

NEVER stage, commit, or push changes. Never.

## Security

When changes are made, `gosec ./...` should pass without error. Should errors be encountered, fix them properly. Do not insert comments to ignore the issues.

## Linting

All code introduced should pass `golangci-lint run -v` without error. Should errors be encountered, fix them properly. Do not insert comments to ignore the issues.

## Code Review Checklist

When creating new features, ensure:
- [ ] Error handling follows the next-line rule with `%w` wrapping
- [ ] Unit tests exist for all code paths (happy path, error cases, edge cases)
- [ ] Standard library `t.Error()`, `t.Errorf()`, and `t.Fatal()` used for assertions
- [ ] Config struct fields and flag definitions are alphabetically sorted
- [ ] Code compiles and all existing tests still pass
- [ ] No manual edits to generated files

## Go-Specific Rules

### Configuration and Code Organization

**Rule**: Configuration struct fields and CLI flag definitions must be alphabetically sorted by name.

**Good**:
```go
type Config struct {
	AutoTLS           bool
	ContactEmail      string
	DemoMode          bool
	Port              int
	DataDir           string
	Validation        bool
}
```

**Bad**:
```go
type Config struct {
	ContactEmail      string
	AutoTLS           bool
	Port              int
}
```

### Error Handling

**Rule**: Error handling conditions must always be placed on the next line after a statement that produces an error.

**Good**:
```go
result, err := someFunction()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

**Bad**:
```go
result, err := someFunction(); if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

**Error wrapping**: Always use `%w` when wrapping an existing `error` value. Only use `%v` or `%s` when constructing a new error from non-error data.

```go
// Wrapping an error — use %w
return fmt.Errorf("failed to initialize: %w", err)

// Constructing from non-error values — %v or %s is fine
return fmt.Errorf("unexpected signing method: %v", alg)
```

Prefer `if err != nil` (positive check) consistently. Avoid `if err == nil` unless absolutely necessary.

### Logging

Use `*slog.Logger` stored on structs. Create child loggers with a `"component"` label for context:

```go
log := s.logger.With("component", "server")
log.Info("Starting server", "addr", addr)
```

In tests, silence the logger:
```go
logger := slog.New(slog.NewTextHandler(io.Discard, nil))
```

### Middleware

All middleware follows the `func(http.Handler) http.Handler` signature. Use chi route groups to scope middleware:

- **Global**: Logging, Prometheus metrics
- **Route-group**: JWT auth (all `/api/v1` routes except `/auth/config`), validation (POST/PUT only)

For request-scoped values, use a private `contextKey` type:
```go
type contextKey string
const UserIDKey contextKey = "userID"
```

### Embedded Assets

Web assets and API docs are compiled into the binary via `//go:embed`. After changing files in `web/` or `docs/`, rebuild the binary to pick up the changes.

## Testing Requirements

### Organization

- Place tests in `*_test.go` files alongside the code
- Use table-driven tests with `t.Run()` subtests
- Use standard library `t.Error()` and `t.Errorf()` for tests instead of an external library
- Use `t.Helper()` in setup functions (e.g., `setupTestHandler(t)`)

### Test Infrastructure

- **Handler tests**: Use `httptest.NewRequest` + `httptest.NewRecorder` with a real SQLite database in `t.TempDir()`
- **Worker tests**: Use mock `Store` implementations for isolated unit testing
- **Logger**: Always silence with `slog.New(slog.NewTextHandler(io.Discard, nil))`

### Example

```go
func TestNewFeature(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
	}{
		{
			name:      "valid input",
			input:     "test",
			expected:  "result",
			expectErr: false,
		},
		{
			name:      "empty input returns error",
			input:     "",
			expected:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewFeature(tt.input)
			if tt.expectErr && err != nil {
				t.Error(t, err)
			}
			if input != expected {
				t.Errorf("expected '%s', got %s", expected, input)
			}
	}
}

```
## Docker Conventions

Image is built from `scratch`:

- `CGO_ENABLED=0` — required (SQLite driver is pure Go)
- Runs as non-root user (UID 1000)
- `/data` volume — database + VAPID keys (persistent, back it up)
- `/cache` volume — CertMagic TLS certificate state
- Exposes ports: 8080 (HTTP), 80/443 (AutoTLS)
