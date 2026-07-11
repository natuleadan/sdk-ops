# sdk-ops — AI Guide

## Overview

**sdk-ops** (`sdk-ops`) is a CLI tool for provisioning and operating VPS servers. It hardens, installs Docker/k3s, deploys services, and wraps kubectl — all via SSH.

**Stack:** Go + Cobra CLI + SSH (`golang.org/x/crypto/ssh`)

## Quick Reference

| Topic | Location |
|-------|----------|
| Commands reference | `docs/commands.md` |
| Architecture | `docs/architecture.md` |
| Conventional commits | `docs/conventional-commits.md` |
| CLI source | `cmd/sdk-ops/` |
| SSH client | `ssh/` |
| Hardening | `hardening/` |
| Docker operations | `docker/` |
| k3s operations | `k3s/` |
| Deploy engine | `deploy/` |
| Agent (on-VPS) | `agent/` |
| DB provisioning | `deploy/database.go` |
| Secrets rotation | `deploy/rotate.go` |
| State tracking | `cmd/sdk-ops/state.go` |
| TLS certs | `deploy/tls.go` |
| Log shipping | `deploy/logging.go` |
| Alerting | `deploy/alerting.go` |
| Monitoring | `monitor/` |
| Notifications | `notify/` |
| Docker Compose | `compose/` |
| Cloud-init | `cloudinit/` |
| Terraform export | `terraform/` |
| sops secrets | `secrets/` |
| High-level API | `server.go` |
| YAML config | `config.go` |
| Provider API | `providers/provider.go` + `providers/types.go` + `providers/credentials.go` |
| CubePath provider | `providers/cubepath/` |
| Hetzner | `providers/hetzner/` |
| DigitalOcean | `providers/digitalocean/` |
| Vultr | `providers/vultr/` (VPS, K8s, LB, DNS, firewall, S3, CDN, block storage) |
| AWS EC2 | `providers/aws/` |
| Bunny.net SDK | `bunny/` (MC, DNS, CDN, Storage, Stream, Shield, Edge Scripting) |
| License & third-party notices | `LICENSE` / `ThirdPartyNotices.txt` |

## Entrypoints

- `cmd/sdk-ops/` — CLI entrypoint (Cobra command tree, 18 commands)
- `agent/` — On-VPS monitoring agent (systemd or Docker, API :9000)
- `server.go` / `config.go` — High-level `ops.Server` API + YAML config
- `ssh/` — SSH client abstraction (public SDK)
- `hardening/` — VPS hardening (packages → user → kernel → fail2ban → SSH → nftables → node_exporter)
- `docker/` — Docker install + compose generation
- `k3s/` — k3s install + 16 kubectl wrappers
- `deploy/` — Build → push → upload → compose → health check → auto-rollback + secrets rotation
- `monitor/` — Real-time node dashboard (CPU, RAM, disk, k3s)
- `notify/` — Notifications (Slack, Discord, Telegram, Email, Webhook)
- `compose/` — Docker Compose YAML manipulation
- `providers/` — Multi-provider interface (21 methods)

## Architecture

```
[Local Machine]          [VPS]
CLI (Cobra) ───SSH──→   1. Hardening (nftables, fail2ban, kernel)
                         2. Docker + k3s install
                         3. /opt/sdk-ops/services/<name>/v{N}/
                         4. docker compose up
                         5. Health check → rollback on failure
```

Alternatively, cloud-init can provision without SSH push:

```
[Local Machine]          [Provider API]         [VPS]
CLI ──API──→ Create VPS ──user-data──→          Boot → cloud-init runs
                                                  (hardening + Docker + k3s)
```

## Commands

Full reference: `docs/commands.md`. Categories:

| Category | Key commands |
|----------|-------------|
| **Provision** | `infra init/join/adopt/status/remove` |
| **Backup** | `infra backup/restore`, `backup create/restore/schedule` |
| **Firewall** | `infra firewall open/close/list` |
| **TLS** | `infra cert install/info` |
| **Logs** | `infra logs install/remove` |
| **Alerts** | `infra alerts install/remove/rule add` |
| **Operations** | `node list/info/top/exec`, `agent install/status/logs/update/schedule` |
| **Deploy** | `deploy init/push`, `deploy encrypt/decrypt`, `service status/logs/restart/rollback/rotate` |
| **Cluster** | `cluster nodes/pods/.../token/events/helm/node-ssh` (29 commands) |
| **Databases** | `db create/list/remove` (postgres, mysql, redis, mongodb) |
| **State** | `state show/sync` (resource inventory) |
| **Compose** | `compose init/service/validate` |
| **SSH Keys** | `key generate/list/deploy` |
| **Notifications** | `notify send/test` |
| **Config** | `config init/add-node/list-nodes/remove-node/set-credentials` |
| **Provider** | `provider vps/k8s/lb/dns/ssh-key` (49 interface methods) |
| **Utilities** | `status` (dashboard), `completion` (bash/zsh/fish) |

## Modes

- `--k3s` (default) — Hardening + Docker + k3s + Traefik
- `--docker` — Hardening + Docker only
- `--bare` — Hardening only

## Init Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--ssh-port` | 0 | Migrate SSH to custom port (0 = keep 22) |
| `--monitor` | false | Install node_exporter (port 9100) |
| `--auditd` | false | Install auditd (CIS) |
| `--lynis` | false | Install Lynis security auditor |
| `--usg` | false | Install Ubuntu Security Guide (CIS) |
| `--crowdsec` | false | Install CrowdSec WAF/IPS |
| `--insecure` | false | Skip SSH host key verification (env: `SDK_OPS_SSH_STRICT_HOST_KEY=true` for strict) |
| `--lock-root` | false | Lock root password |
| `--logs` | "" | Install Promtail to Loki URL |
| `--alerts` | "" | Install Alertmanager (Slack webhook) |
| `--cloud-init` | false | Use cloud-init instead of SSH |
| `--provider` | "" | Create VPS via provider API |
| `--secrets-encryption` | false | Encrypt secrets in etcd (CIS) |
| `--protect-kernel-defaults` | false | Protect kubelet kernel defaults (CIS) |
| `--admission-plugins` | `NodeRestriction,EventRateLimit` | Admission plugins (CIS) |
| `--cis-psa` | false | Pod Security Admission restricted |
| `--cis-audit-log` | false | Kube-apiserver audit logging |
| `--cis-netpol` | false | Default-deny NetworkPolicy |
| `--cis-svcacc` | false | Patch ServiceAccount automount |
| `--cis-tls-ciphers` | false | Restrict TLS cipher suites |

## Workflows (for AI assistants)

### Provision a new VPS via SSH
```
1. sdk-ops config add-node <ip> --user root --key ~/.ssh/id_ed25519
2. sdk-ops infra init <ip>          # hardening + Docker + k3s (or --docker / --bare)
3. Verify: sdk-ops infra status <ip>
   → SSH port stays on 22, user stays root (unless --ssh-port / --lock-root)
   → After hardening: root SSH is blocked — use --user sdkops
```

### Provision with CIS hardening
```
1. sdk-ops config add-node <ip> --user root --key ~/.ssh/id_ed25519
2. sdk-ops infra init <ip> \
     --secrets-encryption --protect-kernel-defaults \
     --cis-psa --cis-audit-log --cis-netpol --cis-svcacc --cis-tls-ciphers \
     --auditd --lynis
3. Verify: sdk-ops infra status <ip>
   → PermitRootLogin no, MaxAuthTries 3, auditd active, Lynis installed
```

### Provision a new VPS via provider API + cloud-init
```
1. sdk-ops infra init --provider cubepath --plan gp.nano --location us-mia-1 --cloud-init
   → Creates VPS via API, passes cloud-init user-data, waits for boot
   → Hardening + Docker + k3s already applied via cloud-init
```

### Deploy a service
```
1. Create a Go service with main.go + service.yaml
2. sdk-ops deploy push ./my-service --node <ip>
   → Auto-installs Docker if missing on node
   → Builds binary for linux/amd64 → Docker buildx + push → tar + SSH pipe
   → docker compose up -d → Health check → Auto-rollback on failure
```

### Multi-node deploy
```
sdk-ops deploy push ./my-service --all
   → Deploys to all registered nodes in parallel (sync.WaitGroup)
```

### Manage firewall rules
```
sdk-ops infra firewall open 9090 --node <ip>
sdk-ops infra firewall close 9090 --node <ip>
sdk-ops infra firewall list --node <ip>
```

### TLS certificate (Let's Encrypt + Caddy)
```
sdk-ops infra cert install --domain example.com --email admin@x.com --node <ip>
```

### Log shipping (Promtail + Loki)
```
sdk-ops infra logs install --node <ip> --loki http://loki:3100
```

### Alerting (Alertmanager)
```
# Slack
sdk-ops infra alerts install --node <ip> --slack https://hooks.slack.com/...
# Telegram
sdk-ops infra alerts install --node <ip> --bot-token 123:abc --chat-id -100123
# Custom rules
sdk-ops infra alerts rule add ./rules.yml --node <ip>
```

### SSH key management on providers
```
sdk-ops provider ssh-key upload my-key --pub-key ~/.ssh/id_ed25519.pub
sdk-ops provider ssh-key list
sdk-ops provider ssh-key delete <id>
```

### Managed Kubernetes
```
sdk-ops provider k8s create --provider cubepath --name cluster --nodes 2 --node-plan gp.micro
sdk-ops provider k8s kubeconfig <id> > kc.yaml
sdk-ops provider k8s delete <id>
```

### Operate k3s cluster
```
sdk-ops cluster nodes          kubectl get nodes
sdk-ops cluster pods           kubectl get pods --all-namespaces
sdk-ops cluster top            kubectl top nodes + pods
sdk-ops cluster logs <pod> -f  kubectl logs -f
sdk-ops cluster scale deploy/my-svc --replicas 5
```

### Encrypt secrets with sops
```
# Encrypt service.yaml with age public key
sdk-ops deploy encrypt service.yaml --age-key age1...

# Deploy with auto-decrypt
sdk-ops deploy push ./my-service --sops-key age1...
```

### Backup and restore
```
sdk-ops infra backup <ip>
sdk-ops infra restore <ip> ./backup.tar.gz
```

## Deploy Flow

```
Local (Mac ARM)                        VPS (x86_64)
─────                                   ─────
1. go build (linux/amd64)               docker login (auto)
2. docker buildx + push ──registry──→   docker pull
3. tar files + SSH pipe ────────→   /opt/sdk-ops/services/<name>/v{N}/
4.                                      symlink: current → v{N}
5.                                      docker compose up -d
6.                                      Health check → OK or rollback
```

## Testing

```bash
make test    # go test -race -count=1 ./...
make lint    # golangci-lint run --timeout=5m ./... + go vet ./...
make build   # go build -o sdk-ops ./cmd/sdk-ops/
```

## Gotchas

- **SSH port stays on 22** by default after hardening. Only changes if `--ssh-port N` is explicitly set.
- **nftables is used** (not UFW). Port 22 is always kept open.
- **Root is NOT locked** by default. Use `--lock-root` to disable root password.
- **Auto-healing**: `deploy push` auto-installs Docker if missing, `cluster` commands auto-install k3s, `node top` auto-installs htop.
- **Registry credentials**: set `REGISTRY_USER` and `REGISTRY_PASS` env vars. Auto-login on VPS during deploy.
- **Credentials fallback**: `~/.sdk-ops/credentials.yaml` is loaded when env vars are not set (`config set-credentials` saves it).
- **Cloud-init**: `--cloud-init` generates user-data with hardening + Docker + k3s baked in. Faster than SSH push.
- **CubePath API rate limit**: 5 requests per 5 minutes. Add sleep between batch operations.
- **`kubectl top` requires metrics-server** to be fully ready (may take 1-2 min after k3s install).
- **Load Balancer cannot be deleted** while in "deploying" state. Wait for it to become active.
- **PermitRootLogin no** after hardening — SSH as root is blocked. Use `--user sdkops` to connect.
- **CIS hardening flags** (`--cis-*`) run post-install and may restart k3s (audit-log, tls-ciphers).
- **etcd-snapshot** requires k3s with embedded etcd (default). Fails with "etcd datastore disabled" if using sqlite.
- **SSH host key strict mode**: set `SDK_OPS_SSH_STRICT_HOST_KEY=true` to enforce `known_hosts` check. Default is `InsecureIgnoreHostKey` (backward compat).
- **File permissions**: project enforces `0600` for files and `0750` for directories — never `0644`/`0755`.
- **Path sanitization**: all file operations use `filepath.Clean` — never pass user input directly to `os.ReadFile`/`os.WriteFile`.
- **Context propagation**: all HTTP/DB/exec calls use `context.Context` — never bare `exec.Command` or `http.NewRequest`.
