# AGENTS.md

Autonomous engineering rules for this Go repository.

## 1. Build & Validation

- Run `go run ./cmd/xtask validate` before opening or updating any pull request. All eight gates must report `PASS` (`TOTAL PASS=8 FAIL=0`).
- **Do not run `gofmt`** or automated code formatters in hooks, CI, or pre-commit steps.
- **Do not add Node.js or external scripting runtimes**; keep all automation in pure Go under `cmd/xtask/`.

## 2. Modular Architecture Rules

- Place domain logic under `internal/modules/<domain>/` using the six-file NestJS-inspired layout (`entity.go`, `dto.go`, `repository.go`, `service.go`, `controller.go`, `module.go`).
- Respect strict one-way dependencies: `Controller` may call `Service` and `dto`, never `Repository`; `entity` and `dto` must stay free of service/repository/controller references.
- Keep every source and documentation file at or below **500 lines**.

## 3. Git & Commit Policy

- Configure your Git author and GitHub noreply email (`<id>+<handle>@users.noreply.github.com`) so personal email addresses are never exposed in commit history.
- Header format: `type(scope): description` in lowercase English, **3 to 6 words**, no trailing period, max 100 characters.
- Body format: one blank line followed by **at least 3 consecutive bullets** (`- **Label**: text`), each **3 to 10 words**, with no blank lines between bullets.
- Never include `Co-authored-by` trailers, local filesystem user paths, or internal workspace identifiers.

## 4. Dependencies & Licensing

- New dependencies must satisfy the **3-day publish-age cooldown** (`.github/dependabot.yml` `cooldown: default-days: 3`, no `replace` directives, no `@latest`).
- Run `go run ./cmd/xtask notices` whenever `go.mod` changes to keep `ThirdPartyNotices.txt` synchronized.
- All code and documentation are released under the Apache-2.0 License (`Copyright (c) 2026 Natuleadan`).
