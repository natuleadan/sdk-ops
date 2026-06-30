# Natuleadan SDK OPS

<p align="center">
  <img src="https://avatars.githubusercontent.com/u/210283438?s=400&u=1afe4cf2a1a5347c739f4efc60b86e3c1564cb6&v=4" width="120" height="120" style="border-radius: 50%;">
  <br>
  <b>CLI:</b> <code>sdk-ops</code> — <b>Go SDK:</b> <code>import "github.com/natuleadan/sdk-ops"</code>
</p>

<p align="center">
  <a href="https://github.com/natuleadan/sdk-ops/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/natuleadan/sdk-ops/ci.yml?style=for-the-badge&label=CI&logo=github"></a>
  <a href="https://github.com/natuleadan/sdk-ops/releases/latest"><img src="https://img.shields.io/github/v/release/natuleadan/sdk-ops?style=for-the-badge&label=Release&logo=github"></a>
  <br>
  <a href="https://pkg.go.dev/github.com/natuleadan/sdk-ops"><img src="https://img.shields.io/badge/Go-Reference-00ADD8?style=for-the-badge&logo=go"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/github/go-mod/go-version/natuleadan/sdk-ops?style=for-the-badge&logo=go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge"></a>
  <a href="https://conventionalcommits.org"><img src="https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg?style=for-the-badge"></a>
</p>

A CLI tool and Go SDK for provisioning, hardening, and operating VPS servers. Automates the full lifecycle: SSH-based hardening (nftables, fail2ban, kernel tuning), Docker/k3s installation, service deployment with auto-rollback, cluster management via kubectl, and cloud resource management across providers.

---

## 1. Install

```bash
go install github.com/natuleadan/sdk-ops/cmd/sdk-ops@latest
```

Or download a pre-built binary from the [releases page](https://github.com/natuleadan/sdk-ops/releases).

## 2. Quick Start

### 2.1 Register a node

```bash
sdk-ops config add-node 192.168.1.100 --user root --key ~/.ssh/id_ed25519
```

### 2.2 Provision a fresh VPS

```bash
sdk-ops infra init 192.168.1.100              # hardening + Docker + k3s (default)
sdk-ops infra init 192.168.1.100 --docker     # Docker only
sdk-ops infra init 192.168.1.100 --bare       # Hardening only
sdk-ops infra init --provider cubepath --plan gp.nano --location us-mia-1  # Create + provision
```

### 2.3 Check the cluster

```bash
sdk-ops infra status 192.168.1.100
```

### 2.4 Deploy a service

```bash
sdk-ops deploy push ./my-service --node 192.168.1.100
sdk-ops deploy push ./my-service --all              # deploy to all nodes
```

### 2.5 Operate the cluster

```bash
sdk-ops cluster nodes
sdk-ops cluster pods
sdk-ops cluster scale deploy/my-app --replicas 5
```

### 2.6 Manage firewall

```bash
sdk-ops infra firewall open 8080 --node 192.168.1.100
sdk-ops infra firewall list --node 192.168.1.100
```

### 2.7 Backup and restore

```bash
sdk-ops infra backup 192.168.1.100
sdk-ops infra restore 192.168.1.100 ./backup.tar.gz
```

### 2.8 Deploy monitoring agent

```bash
sdk-ops agent install --node 192.168.1.100                  # systemd (default)
sdk-ops agent install --node 192.168.1.100 --runtime docker  # Docker container
sdk-ops agent status --node 192.168.1.100
sdk-ops agent schedule add nightly --cron "0 3 * * *" --task shell --config "echo hello"
```

### 2.9 Provision a database

```bash
sdk-ops db create postgres --name mydb --node 192.168.1.100
sdk-ops db create redis --port 6379 --node 192.168.1.100
```

### 2.10 Rotate secrets

```bash
sdk-ops service rotate db my-postgres --type postgres --node 192.168.1.100
sdk-ops service rotate env myservice --name API_KEY --node 192.168.1.100
```

### 2.11 Track resources

```bash
sdk-ops state show
sdk-ops state sync --node 192.168.1.100
```

### 2.12 Unified dashboard

```bash
sdk-ops status
sdk-ops status --node 192.168.1.100
```

### 2.13 Adopt existing server

```bash
sdk-ops infra adopt 192.168.1.100 --force
```

### 2.14 Generate CI/CD

```bash
sdk-ops deploy init ./app --template go --ci github
```

## 3. Features

| Category | Feature | Description |
|----------|---------|-------------|
| **Provision** | `infra init` | Harden + install Docker/k3s from zero |
| | `infra join` | Join a worker to k3s cluster |
| | `infra ready` | Check if cluster is fully operational (exit 0/1) |
| | `infra plan` | Validate and preview a multi-node plan |
| | `infra apply` | Execute a multi-node plan (parallel servers + agents) |
| | `infra status` | Show server health and installed components |
| | `infra remove` | Uninstall sdk-ops from a server |
| | `infra adopt` | Scan existing server + register without reprovisioning |
| | `infra backup` | Backup all services from a node |
| | `infra restore` | Restore services from a backup file |
| | `infra firewall` | Open/close/list nftables rules |
| | `infra cert` | Install TLS certs via Caddy (Let's Encrypt) |
| | `infra logs` | Install Promtail to ship logs to Loki |
| | `infra alerts` | Install Alertmanager (Slack, Email, Telegram) |
| **Operations** | `node list` | List registered nodes |
| | `node info` | Real-time dashboard (CPU, RAM, disk, k3s) |
| | `node top` | Interactive htop via SSH |
| | `node exec` | Run a command remotely (single, --all, --servers, --agents) |
| | `agent install` | Deploy monitoring agent (systemd or Docker) |
| | `agent status/logs` | Check agent health and logs |
| | `agent uninstall` | Remove agent with optional data purge |
| | `agent update` | Check and apply agent updates |
| | `agent schedule` | Add/list/remove scheduled tasks on agent |
| **Deploy** | `deploy push` | Build + upload + deploy a service with auto-rollback |
| | `deploy push --all` | Deploy to all registered nodes in parallel |
| | `deploy push --sops-key` | Auto-decrypt service.yaml with sops |
| | `deploy push --runtime` | docker, k3s, swarm, bare |
| | `deploy encrypt` | Encrypt a service.yaml with sops |
| | `deploy decrypt` | Decrypt a sops-encrypted file |
| | `deploy init` | Scaffold from template (html, node, wordpress, go, nextjs, python-fastapi, django) |
| | `deploy init --ci` | Generate GitHub Actions / GitLab CI pipeline |
| | `service status/logs/restart` | Manage deployed services |
| | `service rollback` | Rollback to previous version (--version N, --diff) |
| | `service versions` | List deployed versions |
| | `service rotate db` | Rotate database password |
| | `service rotate env` | Rotate environment variable |
| **Cluster** | `cluster nodes/pods/services/...` | kubectl wrappers (16 commands) |
| | `cluster top/logs/exec/scale/apply/delete/describe` | Pod + deployment management |
| | `cluster token/restart/events` | Cluster info and monitoring |
| | `cluster cordon/uncordon/drain/label` | Node management |
| | `cluster upgrade/etcd-snapshot/etcd-restore/cert-rotate` | Upgrade and maintenance |
| | `cluster get <type> <name> -o yaml` | Resource inspection |
| | `cluster helm repo-add/repo-list/install/upgrade/list` | Helm chart management |
| | `cluster node-ssh` | SSH into a cluster node |
| | `cluster port-forward` | Port forwarding via SSH tunnel |
| **Databases** | `db create` | Provision postgres, mysql, redis, mongodb |
| | `db list` | List databases on a node |
| | `db remove` | Remove a database |
| **Config** | `config init/add-node/list-nodes/remove-node` | Manage ~/.sdk-ops/config.yaml |
| | `config set-credentials` | Save provider credentials to file |
| **Compose** | `compose init` | Create new docker-compose.yml |
| | `compose service` | Add/remove/list/env services |
| | `compose validate` | Validate docker-compose syntax |
| **SSH Keys** | `key generate` | Generate SSH key pair locally |
| | `key list` | List local SSH keys |
| | `key deploy` | Deploy SSH key to server |
| **Notifications** | `notify send` | Send notification (Slack, Discord, Telegram, Email) |
| | `notify test` | Test all configured notifiers |
| **Provider API** | CubePath, Hetzner, DO, Vultr, AWS | 49 methods: VPS, K8s, LB, DNS, SSH keys |
| | `provider k8s update/protection` | Upgrade K8s version + toggle deletion protection |
| | `provider k8s addons list/available/install/uninstall` | K8s addon management |
| | `provider k8s node-pool list/add/scale/delete` | Node pool management |
| | `provider k8s lb-list <id>` | List LBs attached to a cluster |
| | `provider lb listener add/update/delete` | LB listener management |
| | `provider lb health-check set` | LB health check configuration |
| | `provider lb target add/list/drain` | LB target management |
| | `provider lb resize/metrics/protection` | LB plan, metrics, and protection |
| | `provider ssh-key` | Upload/list/delete SSH keys on providers |
| | `provider vps export` | Export VPS as Terraform HCL |
| **State** | `state show` | Show tracked resources (services, databases, schedules) |
| | `state sync` | Scan nodes and update inventory |
| **Utilities** | `status` | Unified dashboard for all nodes |
| | `completion` | Generate bash/zsh/fish completion scripts |

## 4. Use as Go SDK

```go
import "github.com/natuleadan/sdk-ops"

// Option 1: YAML-driven
cfg, _ := ops.LoadConfig("server.yaml")
s := ops.New(*cfg)
s.Provision(ctx)

// Option 2: Programmatic
cfg := ops.ServerConfig{
    Host:        "192.168.1.100",
    User:        "root",
    SSHKey:      "~/.ssh/id_ed25519",
    Mode:        ops.ModeK3s,
    Monitor:     true,
    CrowdSec:    true,
    CloudInit:   true,
    InsecureSSH: true,
}
s := ops.New(cfg)
s.Provision(ctx)

// Check status
status, _ := s.Status()
fmt.Println(status)

// Deploy a service
s.Deploy("./my-service")

// Backup / Restore
s.BackupServices(".")
s.RestoreServices("./backup.tar.gz")

// Cluster operations
s.Cluster().Nodes()
s.Cluster().Pods("")
s.Cluster().Services("")
s.Cluster().Deployments("")
s.Cluster().Top()
s.Cluster().Scale("deploy/my-app", 5)
```

## 5. Init Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `k3s` | k3s, docker, or bare |
| `--ssh-port` | `0` | Migrate SSH to custom port (0 = keep port 22) |
| `--monitor` | `false` | Install Prometheus node_exporter |
| `--auditd` | `false` | Install auditd for system auditing (CIS) |
| `--lynis` | `false` | Install Lynis security auditor |
| `--usg` | `false` | Install Ubuntu Security Guide (CIS) |
| `--crowdsec` | `false` | Install CrowdSec WAF/IPS |
| `--lock-root` | `false` | Lock root password after creating sdkops user |
| `--logs` | `""` | Install Promtail, ship logs to Loki URL |
| `--alerts` | `""` | Install Alertmanager with Slack webhook |
| `--cloud-init` | `false` | Use cloud-init instead of SSH-based provisioning |
| `--provider` | `""` | Create VPS via provider API first |
| `--plan` | `gp.nano` | VPS plan (provider) |
| `--location` | `us-mia-1` | VPS location (provider) |
| `--secrets-encryption` | `false` | Enable secrets encryption at rest in etcd (CIS) |
| `--protect-kernel-defaults` | `false` | Protect kubelet kernel defaults (CIS) |
| `--admission-plugins` | `NodeRestriction,EventRateLimit` | Kube-apiserver admission plugins (CIS) |
| `--cis-psa` | `false` | Enforce Pod Security Admission restricted (CIS) |
| `--cis-audit-log` | `false` | Enable kube-apiserver audit logging (CIS) |
| `--cis-netpol` | `false` | Apply default-deny NetworkPolicy (CIS) |
| `--cis-svcacc` | `false` | Patch default ServiceAccount automount=false (CIS) |
| `--cis-tls-ciphers` | `false` | Restrict TLS cipher suites (CIS) |

## 6. Deploy Flow

```
Local (Mac ARM)                        VPS (x86_64)
─────                                   ─────
1. go build (linux/amd64)               docker login (auto)
2. docker buildx + push ──registry──→   docker pull
3. tar files + SSH pipe ────────→      /opt/sdk-ops/services/<name>/v{N}/
4.                                      symlink: current → v{N}
5.                                      docker compose up -d
6.                                      Health check → OK or rollback (configurable via health_url)
```

Health check probes configurable endpoints via `health_url` in `service.yaml`. Falls back to ports 18081/8080/3000 at `/healthz` and `/health`. Custom timeout via `health_timeout`.

## 7. Documentation

| File | Contents |
|------|----------|
| [docs/commands.md](docs/commands.md) | Full command reference (all flags, subcommands) |
| [docs/architecture.md](docs/architecture.md) | Architecture, hardening order, deploy engine |
| [docs/conventional-commits.md](docs/conventional-commits.md) | Commit rules, versioning, release flow |
| [docs/provider-credentials.md](docs/provider-credentials.md) | Provider credential setup |
| [docs/known-issues.md](docs/known-issues.md) | Known limitations and workarounds |
| [AGENTS.md](AGENTS.md) | AI assistant guide |
| | `cluster nodes/pods/services/top/logs/scale` (16 kubectl commands) | |

## 8. Examples

### 8.1 Provision a VPS via provider + CLI

```bash
# Create VPS via CubePath, then harden + install k3s automatically
sdk-ops infra init --provider cubepath \
  --plan gp.nano \
  --location us-mia-1 \
  --template ubuntu-24 \
  --ssh-key-ids 421 \
  --api-key "${CUBEPATH_API_KEY}"
```

### 8.2 Provision via YAML + Go SDK

Create `server.yaml`:

```yaml
provider: cubepath
api_key: "${CUBEPATH_API_KEY}"
project_id: 4601
plan: gp.nano
location: us-mia-1
template: ubuntu-24
ssh_key_ids: [421]
```

Then run with Go:

```go
cfg, _ := ops.LoadConfig("server.yaml")
s := ops.New(*cfg)
s.Provision(ctx)
```

### 8.3 Firewall management

```bash
sdk-ops infra firewall open 9090 --node 192.168.1.100
sdk-ops infra firewall close 9090 --node 192.168.1.100
sdk-ops infra firewall list --node 192.168.1.100
```

### 8.4 TLS certificate via Caddy

```bash
sdk-ops infra cert install \
  --domain example.com \
  --email admin@example.com \
  --node 192.168.1.100

# Use Let's Encrypt staging for testing
sdk-ops infra cert install \
  --domain example.com \
  --email admin@example.com \
  --staging \
  --node 192.168.1.100
```

### 8.5 Log shipping with Promtail

```bash
sdk-ops infra logs install \
  --node 192.168.1.100 \
  --loki http://loki.example.com:3100
```

### 8.6 Alerting with Alertmanager

```bash
# Slack
sdk-ops infra alerts install \
  --node 192.168.1.100 \
  --slack https://hooks.slack.com/...

# Telegram
sdk-ops infra alerts install \
  --node 192.168.1.100 \
  --bot-token 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11 \
  --chat-id -1001234567890

# Custom alert rules
sdk-ops infra alerts rule add ./rules.yml --node 192.168.1.100
```

### 8.7 Deploy with secrets

```bash
# Encrypt service.yaml with age key
sdk-ops deploy encrypt service.yaml --age-key age1...

# Deploy with auto-decrypt
sdk-ops deploy push ./my-service --node 192.168.1.100 --sops-key age1...
```

### 8.8 Multi-node deploy

```bash
# Deploy to all registered nodes in parallel
sdk-ops deploy push ./my-service --all
```

### 8.9 SSH key management on providers

```bash
# Upload your public key
sdk-ops provider ssh-key upload my-key --pub-key ~/.ssh/id_ed25519.pub

# List keys
sdk-ops provider ssh-key list

# Delete a key
sdk-ops provider ssh-key delete 123
```

### 8.10 Export to Terraform

```bash
sdk-ops provider vps export <vps-id> --provider cubepath
```

### 8.11 Save credentials to file

```bash
export CUBEPATH_API_KEY="your-key"
sdk-ops config set-credentials
# → Credentials saved to ~/.sdk-ops/credentials.yaml
```

### 8.12 Managed Kubernetes

```bash
# Create a 2-node managed K8s cluster
sdk-ops provider k8s create --provider cubepath \
  --name prod-cluster \
  --location us-mia-1 \
  --node-plan gp.micro \
  --nodes 2

# Get kubeconfig and use it
sdk-ops provider k8s kubeconfig <cluster-uuid> > kubeconfig.yaml
kubectl --kubeconfig kubeconfig.yaml get nodes

# Delete the cluster
sdk-ops provider k8s delete <cluster-uuid>
```

## 9. Project Structure

```
├── agent/                # On-VPS monitoring agent (systemd or Docker)
│   ├── main.go           # Agent entrypoint: lifecycle + API server on :9000
│   ├── api.go            # HTTP API: /health, /metrics, /schedules, /exec, /inventory
│   ├── health.go         # 10 monitors: containers, disk, SSL, network, temperature
│   ├── metrics.go        # CPU, RAM, disk, Docker stats collection
│   ├── scheduler.go      # Cron scheduler (robfig/cron)
│   ├── notify.go         # Notification sending from agent
│   ├── events.go         # Docker event watcher + log pattern watcher
│   ├── update.go         # Self-update from GitHub releases
│   ├── config.go         # Agent config loading
│   └── db.go             # SQLite storage (metrics, audit, schedules)
├── cmd/sdk-ops/          # CLI entrypoint (Cobra)
│   ├── main.go           # Root command, 15 subcommands, newSSHClient
│   ├── infra.go          # infra init/join/adopt/status/remove/backup/restore/firewall/cert/logs/alerts
│   ├── node.go           # node list/info/top/exec (--all, --servers, --agents)
│   ├── deploy.go         # deploy init/push/encrypt/decrypt + service status/logs/restart/rollback/versions/rotate
│   ├── cluster.go        # cluster (16 kubectl wrappers)
│   ├── agent.go          # agent install/status/logs/uninstall/update/schedule
│   ├── config.go         # config init/add-node/list-nodes/remove-node/set-credentials
│   ├── provider.go       # provider vps/k8s/lb/dns/ssh-key
│   ├── backup.go         # backup create/restore/schedule/unschedule/list-schedules
│   ├── db.go             # db create/list/remove (postgres, mysql, redis, mongodb)
│   ├── compose.go        # compose init/service/validate
│   ├── key.go            # key generate/list/deploy
│   ├── notify.go         # notify send/test
│   ├── state.go          # state show/sync (resource inventory)
│   ├── status.go         # status (unified multi-node dashboard)
│   └── spinner.go        # CLI spinner animation
├── ssh/                  # SSH client abstraction (public SDK)
├── hardening/            # VPS hardening (public SDK)
│   ├── apply.go          # Orchestrator (calls steps in order)
│   ├── steps.go          # Individual hardening steps + node_exporter
│   ├── firewall.go       # FirewallOpen/Close/List via nftables
│   └── hconfig.go        # YAML config export/import
├── cloudinit/            # Cloud-init user-data generation
├── docker/               # Docker install + compose (public SDK)
├── k3s/                  # k3s install + join (public SDK)
├── deploy/               # Build + push + deploy engine (public SDK)
│   ├── upload.go         # Tar/SSH upload, version management, rollback
│   ├── run.go            # Service lifecycle (status, logs, restart, health check)
│   ├── backup.go         # Backup/restore services + S3 upload
│   ├── database.go       # DB provisioning (postgres, mysql, redis, mongodb)
│   ├── rotate.go         # Secrets rotation (DB passwords, env vars)
│   ├── tls.go            # Caddy TLS cert install
│   ├── logging.go        # Promtail log shipper install
│   ├── alerting.go       # Alertmanager install
│   ├── builder.go        # Builder interface + DetectBuilder, BuildImage
│   ├── builder_dockerfile.go, builder_nixpacks.go, builder_pack.go
│   ├── proxy.go          # Proxy interface + DetectProxy
│   ├── proxy_caddy.go, proxy_traefik.go, proxy_nginx.go
│   ├── bluegreen.go      # Blue/green zero-downtime deploy
│   ├── bare_runtime.go   # Bare metal systemd deploy
│   ├── swarm_runtime.go  # Docker Swarm deploy
│   └── k8s_runtime.go    # k3s Deployment + Service + Ingress
├── compose/              # Docker Compose YAML manipulation
├── monitor/              # Node dashboard + metrics (public SDK)
├── notify/               # Notifications (Slack, Discord, Telegram, Email, Webhook)
├── terraform/            # Terraform HCL generation (export)
├── secrets/              # sops encryption/decryption helpers
├── providers/            # Multi-provider interface + implementations
│   ├── provider.go       # Provider interface (21 methods)
│   ├── types.go          # VPS, K8s, LB, DNS, BareMetal, SSHKey structs
│   ├── credentials.go    # Credential file loader
│   ├── cubepath/         # CubePath (raw HTTP)
│   ├── hetzner/          # Hetzner (hcloud-go + raw HTTP)
│   ├── digitalocean/     # DigitalOcean (godo)
│   ├── vultr/            # Vultr (govultr)
│   └── aws/              # AWS (aws-sdk-go-v2)
├── server.go             # High-level ops.Server API
├── config.go             # YAML-driven config
├── docs/                 # Documentation
└── .github/              # CI/CD workflows
```

## 10. License

This project is open source under the MIT License. See [LICENSE](LICENSE) for the full text.
