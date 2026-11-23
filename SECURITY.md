# Security Policy

## Supply-Chain & Hygiene Controls

This template enforces automated defense-in-depth across local hooks and GitHub Actions:

1. **Three-Day Dependency Cooldown**: `.github/dependabot.yml` (`cooldown: default-days: 3`) and `cmd/xtask/main.go` (blocking `replace` and `@latest`) protect against newly published or compromised module versions.
2. **Static Analysis (CodeQL)**: `.github/workflows/security.yml` runs GitHub CodeQL analysis for Go on every push, pull request, and weekly schedule.
3. **Dependency Audit & SBOM**: `go mod verify` and `go list -m -json all` inventories are exported as CI build artifacts on every security run.
4. **Anti-Leak Hygiene Gate**: `cmd/xtask/hygiene.go` blocks hardcoded user paths, restricted tokens, and private keys before code can be pushed.

## Reporting a Vulnerability

Please report suspected security issues privately using GitHub Security Advisories for this repository rather than opening public issues.
