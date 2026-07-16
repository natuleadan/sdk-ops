# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | ✅ |
| < 0.1   | ❌ |

## Reporting a Vulnerability

Please report security vulnerabilities via GitHub Security Advisories at:

https://github.com/natuleadan/sdk-ops/security/advisories

Do not open public issues for security vulnerabilities.

## Security Features

### Infrastructure Hardening
- SSH key-only authentication (password auth disabled)
- SSH port stays on 22 by default (optional `--ssh-port N` migration)
- Root login restricted to key-based auth
- Root password NOT locked by default (optional `--lock-root` flag)
- nftables firewall (default deny policy)
- fail2ban with 1-hour bans
- Automatic security updates via unattended-upgrades
- Kernel hardening (sysctl: syncookies, rp_filter, ptrace_scope)
- Known-host verification with `~/.ssh/known_hosts` (env var `SDK_OPS_SSH_STRICT_HOST_KEY=true`) or `--insecure` to skip

### Code-Level Security

| Feature | Description | Files |
|---------|-------------|-------|
| **Path traversal prevention** | `filepath.Clean` on all file read/write operations (37+ locations) | cmd/, deploy/ |
| **Secure file permissions** | Files written with `0600`, directories with `0750` | deploy/, templates/, hardening/ |
| **Root-scoped writes** | `writeFileSafe()` uses `os.OpenRoot("/")` for chroot-style safety | cmd/sdk-ops/deploy.go, k3s/install.go |
| **Registry config validation** | `RegistryConfig.Valid()` validates credentials before deploy | deploy/upload.go, deploy/builder_dockerfile.go |
| **Context propagation** | All HTTP/DB/exec calls use `context.Context` for cancellation | notify/, providers/ |
| **SSH host key configurable** | `knownhosts.New()` by default; env var `SDK_OPS_SSH_STRICT_HOST_KEY=false` for insecure | ssh/client.go |
| **Static security scanning** | `golangci-lint` with gosec (severity: medium, confidence: high) in CI | .golangci.yml, .github/workflows/ci.yml |
