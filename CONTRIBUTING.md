# Contributing to sdk-ops

Thanks for your interest in contributing to sdk-ops!

## Conventional Commits

Every commit must follow `type(scope): description` with a **required scope**.

```
feat(cli): add --crowdsec flag to infra init
fix(ssh): handle reconnection after port change
```

See [docs/conventional-commits.md](docs/conventional-commits.md) for the full reference.

## Development

```bash
# Build
make build
# or: go build -o sdk-ops ./cmd/sdk-ops/

# Test
make test
# or: go test -race -count=1 ./...

# Run
./sdk-ops --help
```

## Pre-commit Checklist

Before every commit, run in order:

```bash
go fix ./...              # Auto-migrate deprecated APIs
go mod tidy               # Clean unused dependencies
golangci-lint run --timeout=5m   # 0 errors across 14 linters
go build ./...            # Compile
go test -short ./...      # Unit tests
go vet ./...              # Static analysis
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Run the [pre-commit checklist](#pre-commit-checklist) above
4. Push and submit a PR with a clear description

## Code Style

- Follow standard Go conventions (`gofmt`, `golangci-lint`)
- Use Cobra for CLI commands; **each subcommand in its own function** (see: `cmd/sdk-ops/deploy.go`)
- SSH operations go through `ssh/`
- Each hardening step is a separate function in `hardening/steps.go`
- New provider features go in `providers/<name>/<feature>.go`
- CLI commands follow `cmd/sdk-ops/<name>.go` pattern
- New deploy-side features go in `deploy/<feature>.go` (see: `rotate.go`, `database.go`)
- Agent features go in `agent/<feature>.go`

## Security Patterns (required)

| Pattern | Rule | Enforced by |
|---------|------|-------------|
| `filepath.Clean(path)` before every file read/write | G304/G703 | gosec |
| `os.WriteFile(path, data, 0600)` — never 0644 | G306 | gosec |
| `os.MkdirAll(dir, 0750)` — never 0755 | G301 | gosec |
| `exec.CommandContext(ctx, ...)` — never bare `exec.Command` | noctx | noctx |
| `http.NewRequestWithContext(ctx, ...)` — never bare `http.NewRequest` | noctx | noctx |
| `db.QueryContext/ExecContext(ctx, ...)` — never bare `db.Query/Exec` | noctx | noctx |
| `defer func() { if err := x.Close(); err != nil { ... } }()` for Close | errcheck | errcheck |
| `for range n` / `strings.Cut` / `min`/`max` — Go 1.26 idioms | go fix | go fix |
| `any` instead of `interface{}` | unconvert | unconvert |
| Avoid `if/else if` chains on same var — use `switch` | ifElseChain | gocritic |
| `fmt.Fprintf(&b, ...)` instead of `b.WriteString(fmt.Sprintf(...))` | QF1012 | staticcheck |
