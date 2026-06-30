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

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Run `go build ./... && go vet ./...`
4. Run `go test ./...`
5. Submit a PR with a clear description

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use Cobra for CLI commands
- SSH operations go through `ssh/`
- Each hardening step is a separate function in `hardening/steps.go`
- New provider features go in `providers/<name>/<feature>.go`
- CLI commands follow `cmd/sdk-ops/<name>.go` pattern (see: `state.go`, `status.go`, `spinner.go`)
- New deploy-side features go in `deploy/<feature>.go` (see: `rotate.go`, `database.go`)
- Agent features go in `agent/<feature>.go`
