# What sdk-ops installs on a host (runtime inventory)

For transparency and auditing: everything the provision leaves running on a VPS
beyond the OS, the containers and k3s. Audit live with:

```bash
sdk-ops ops status                        # the cron/timer stack (diff-aware apply)
systemctl list-timers 'sdk-ops-*' --all   # every installed timer
systemctl list-units  'sdk-ops-*' --all   # every installed unit
```

## Timers / services (self-healing)

| Component | Units | Script | Cadence | Purpose | Remove |
|---|---|---|---|---|---|
| Security watch | `sdk-ops-security.timer` + `.service` | `/opt/sdk-ops/security/watch.sh` | 5 min | abuse detection (SSH attempts, DDoS thresholds); Telegram alerts only with evidence (IP + attempts + provider) | `sdk-ops infra uninstall security` |
| Firewall state watch | `sdk-ops-state.timer` + `.service` | `/opt/sdk-ops/firewall/state_watch.sh` | 5 min | verifies the allowlist sets + ports registry against the live `exposed` chain and repairs drift | `sdk-ops ops remove state` |
| Provider allowlist refresh | `sdk-ops-allowlist.timer` + `.service` | `/opt/sdk-ops/firewall/allowlist.sh` | daily | refreshes the provider IP ranges (`--provision-yaml` supplies the admin IPs) | `sdk-ops infra uninstall allowlist` |
| Firewall rollback | `sdk-ops-fw-rollback.timer` + `.service` | same dir | boot window | safety net: reverts firewall changes that lock the operator out | (installed with the allowlist) |
| Traefik watchdog | `sdk-ops-traefik.timer` + `.service` | `/opt/sdk-ops/traefik/{watch,install}.sh` | 5 min | recreates a vanished traefik container; verifies config and `acme.json` permissions | `sdk-ops ops remove traefik` |
| Cert sync | `sdk-ops-certs.timer` + `.service` | worker (cross-compiled Go) | daily | copies renewed ACME certs from traefik's `acme.json` into the services | `sdk-ops ops remove certs` |
| Logrotate | `/etc/logrotate.d/sdk-ops` (config, no timer) | — | monthly / 100M, keep 4 | rotates the sdk-ops logs | `sdk-ops ops remove logrotate` |

`sdk-ops ops {apply,status,logs,run,enable,disable,remove}` is the CLI for this
stack: `apply` diffs the scripts and only restarts what changed.

## Long-lived components (`infra init` / `infra provision`)

| Component | Where | Purpose | Remove |
|---|---|---|---|
| nftables ruleset | `/etc/nftables.conf` | default-deny input, peer rules, admin sets (allowlist) | `sdk-ops infra uninstall all` |
| fail2ban | `/etc/fail2ban/jail.local` | sshd + recidive jails, `ignoreip` seeded with the fleet admin IPs | `sdk-ops infra uninstall fail2ban` |
| swap | swapfile + unit | memory cushion (0.5x RAM base, +0.5x per 10GB free disk, cap 2x) | `sdk-ops infra swap remove` |
| node_exporter | systemd unit | metrics on :9100 (opt-in, `--monitor`) | `sdk-ops infra uninstall node-exporter` |
| docker / k3s / helm | packages | container runtime + cluster; helm pinned by the k3s provision | `sdk-ops infra uninstall docker\|k3s` |

## Service-level artifacts (per deployed service)

- `/opt/sdk-ops/services/<name>/` — rendered config, `init.sh`, `validate.sh`,
  `test/` (integration tests), DR scripts (`backup-s3.sh`, `restore-s3.sh`) and
  `.env` (secrets from the environment, mode `0600`).
- CLI: `sdk-ops service validate|test|dr <name>` runs those node-side scripts
  and prints the output **with timing**; `sdk-ops service status|logs|restart`
  handles the day-to-day.

## Rules

- The **agent never formats** a VPS (that is the user's action); cleaning up is
  YAML/CLI: undeclare a service and the provision uninstalls it, or use
  `sdk-ops infra uninstall <component>`.
- Secrets never live in the YAML: the provision writes each service `.env` from
  the environment.
