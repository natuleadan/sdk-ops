# Contributing

Thank you for contributing to this repository.

## Local Setup

1. Enable repository git hooks:
   ```sh
   git config core.hooksPath .githooks
   ```
2. Verify your toolchain and baseline checks:
   ```sh
   go run ./cmd/xtask validate
   ```

## Commit Message Specification

Every commit is validated locally by `.githooks/commit-msg` and in CI by `Conventional Commits`:

- **Header**: `type(scope): description` (lowercase English, 3 to 6 words, no trailing period).
- **Body**: A single blank line followed by at least 3 consecutive `- **Label**: text` bullets (3 to 10 words each, no blank lines between bullets).

Example:

```text
feat(math): add bounded multiplication domain operation

- **Dto**: validate operand range before multiplication
- **Service**: record multiplication results in repository
- **Tests**: verify boundary rules and integration flow
```

## Pull Request Workflow

- Branch from `dev` and target `dev` for feature work.
- `main` accepts rebase-only pull requests from `dev` after all CI and CodeQL checks pass.
- Run `go run ./cmd/xtask notices` whenever `go.mod` changes.
