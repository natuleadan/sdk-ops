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

- SSH key-only authentication (password auth disabled)
- SSH port stays on 22 by default (optional `--ssh-port N` migration)
- Root login restricted to key-based auth
- Root password NOT locked by default (optional `--lock-root` flag)
- nftables firewall (default deny policy)
- fail2ban with 1-hour bans
- Automatic security updates via unattended-upgrades
- Kernel hardening (sysctl: syncookies, rp_filter, ptrace_scope)
- Known-host verification with `~/.ssh/known_hosts` (optional `--insecure` flag to skip)
