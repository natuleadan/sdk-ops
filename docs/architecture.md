# Architecture

## Overview

sdk-ops is a single binary CLI (`sdk-ops`) that provisions and operates servers via SSH. It has no server-side agent — all operations are push-based.

```
┌────────────────┐
│   sdk-ops CLI      │
│  (your laptop) │
└───────┬────────┘
        │ SSH (golang.org/x/crypto/ssh)
        ▼
┌───────────────────────────────────────┐
│           VPS / Bare Metal            │
│  ┌─────────┐  ┌──────┐  ┌─────────┐  │
│  │ nftables │  │ k3s  │  │ Docker  │  │
│  │ fail2ban │  │      │  │         │  │
│  │ sysctl   │  │      │  │         │  │
│  │ node_exp │  │      │  │         │  │
│  └─────────┘  └──────┘  └─────────┘  │
│                                       │
│  /opt/sdk-ops/                             │
│  ├── services/    ← deployed apps     │
│  ├── backups/     ← service backups   │
│  └── logs/        ← service logs      │
└───────────────────────────────────────┘
```

## Package Structure (SDK)

```
cmd/sdk-ops/              ← Cobra CLI root + all subcommands
├── main.go          ← Root command, global --insecure flag, newSSHClient helper
├── infra.go         ← infra init/join/status/remove/backup/restore/firewall/cert/logs/alerts
├── node.go          ← node list/info/top/exec (--all flag)
├── deploy.go        ← deploy push/encrypt/decrypt, auto Docker install
├── cluster.go       ← cluster (16 kubectl wrappers, auto k3s install)
├── service.go       ← service status/logs/restart/rollback/versions
├── config.go        ← config init/add-node/list-nodes/remove-node/set-credentials
├── provider.go      ← provider vps/k8s/lb/dns/ssh-key
└── backup.go        ← backup create/restore (top-level)

server.go / config.go ← High-level ops.Server API + YAML config

cloudinit/           ← Cloud-init user-data generation (--cloud-init)

terraform/           ← Terraform HCL export (provider vps export)

secrets/             ← sops encryption/decryption helpers (deploy encrypt/decrypt)

ssh/                 ← SSH client (connect, exec, stream, PTY, agent support)
├── client.go        ← SSH Client + KnownHosts + agent auth + InsecureIgnoreHostKey

hardening/           ← Step-by-step server hardening
├── apply.go         ← Orchestrator (calls steps in order)
├── steps.go         ← 7 steps: packages, user, kernel, fail2ban, SSH, nftables, node_exporter
├── firewall.go      ← FirewallOpen/Close/List via nftables
└── hconfig.go       ← YAML config export/import

docker/              ← Docker install + health check (auto sudo support)

k3s/                 ← k3s install + join (auto sudo support)

deploy/              ← Service lifecycle
├── upload.go        ← Tar/SSH upload, version management, BuildAndPushImage
├── run.go           ← Runtime detection, docker/k3s/systemd, health check
├── backup.go        ← BackupServices/RestoreServices
├── tls.go           ← Caddy install + TLS cert provisioning
├── logging.go       ← Promtail install + Loki config
└── alerting.go      ← Alertmanager install + Slack/Email/Telegram config

monitor/             ← Remote stats (CPU, RAM, disk, k3s status, top processes)

providers/           ← Multi-provider interface
├── provider.go      ← Provider interface (21 methods: VPS, K8s, LB, DNS, BareMetal, SSHKey)
├── types.go         ← VPS, K8sCluster, LB, BareMetal, DNSZone, DNSRecord, SSHKey
├── credentials.go   ← credential file loader (~/.sdk-ops/credentials.yaml)
├── cubepath/        ← CubePath (raw HTTP, schema in cubepath-api.json)
│   ├── client.go    ← HTTP client core
│   ├── vps.go       ← VPS create/list/delete/get + wait loop
│   ├── k8s.go       ← K8s clusters + kubeconfig
│   ├── lb.go        ← Load balancers
│   ├── dns.go       ← DNS zones + records
│   ├── extra.go     ← BareMetal
│   └── sshkey.go    ← SSH key management
├── hetzner/         ← Hetzner (hcloud-go + raw HTTP)
│   ├── client.go    ← Constructor
│   ├── raw.go       ← Raw HTTP helper
│   ├── vps.go       ← VPS via hcloud-go
│   ├── k8s.go       ← K8s via raw API
│   ├── lb_dns.go    ← LB + DNS + BareMetal
│   └── sshkey.go    ← SSH key management
├── digitalocean/    ← DigitalOcean (godo)
│   ├── client.go    ← Constructor
│   ├── vps.go       ← Droplets
│   ├── k8s.go       ← DOKS
│   ├── lb_dns.go    ← LB + DNS + BareMetal
│   └── sshkey.go    ← SSH key management
├── vultr/           ← Vultr (govultr)
│   ├── client.go    ← Constructor (token transport)
│   ├── vps.go       ← Instances
│   ├── k8s.go       ← VKE
│   ├── lb_dns.go    ← LB + DNS + BareMetal
│   └── sshkey.go    ← SSH key management
└── aws/             ← AWS (aws-sdk-go-v2)
    ├── client.go    ← Constructor (4 service clients)
    ├── vps.go       ← EC2 instances
    ├── k8s.go       ← EKS + kubeconfig generation
    ├── lb_dns.go    ← ELBv2 + Route53 + BareMetal
    └── sshkey.go    ← SSH key management
```

## Deploy Flow

```
 1. go build (linux/amd64)       ──local──  Compile for target arch
 2. docker login on node          ──remote── Register container registry auth
 3. docker buildx + push          ──registry─ Build & push Docker image
 4. tar files + SSH pipe          ──network── Upload to /opt/sdk-ops/services/<name>/v{N}/
 5. symlink: current → v{N}       ──local──  Atomic version switch
 6. docker compose up -d          ──remote──  Start the service
 7. Health check (HTTP :8080)     ──remote──  Verify + auto-rollback
```

## Hardening Steps

| Step | Description | Optional? |
|------|-------------|-----------|
| 1. Install packages | nftables, fail2ban, unattended-upgrades, htop | no |
| 2. Create user | `sdkops` with sudo + SSH key from root | no |
| 3. Kernel tuning | sysctl: syncookies, rp_filter, ptrace_scope | no |
| 4. fail2ban | SSH protection, 1h ban after 5 retries | no |
| 5. SSH config | Keep port 22, disable password auth, restrict root login | no |
| 6. nftables | Drop by default, allow 22/80/443/6443 | no |
| 7. node_exporter | Prometheus node_exporter on port 9100 | `--monitor` |
| 8. SSH port migration | Add new port + keep port 22 | `--ssh-port N` |
| 9. Lock root | Lock root password after user creation | `--lock-root` |

**Port 22 is never removed** from nftables. The default SSH port stays on 22. If `--ssh-port N` is used, the new port is added alongside port 22 (both remain open).

## Configuration

### Node registry (`~/.sdk-ops/config.yaml`)

```yaml
nodes:
  - ip: 192.168.1.100
    user: sdkops
    key: /home/user/.ssh/id_ed25519
    port: 22
    mode: k3s
```

### Provider credentials (`~/.sdk-ops/credentials.yaml`)

```yaml
cubepath_api_key: "your-key"
hetzner_api_token: "your-token"
digitalocean_token: "your-token"
vultr_api_key: "your-key"
aws_region: "us-east-1"
aws_profile: "default"
```

Credentials are loaded in this priority order: `--api-key` flag > env var > credentials file.

## VPS Directory Structure

```
/opt/sdk-ops/
├── services/
│   ├── healthz-svc/
│   │   ├── v1/
│   │   │   ├── docker-compose.yml
│   │   │   └── service.yaml
│   │   ├── v2/
│   │   ├── current → v2
│   │   └── previous → v1
│   └── ...
├── backups/
└── logs/
```
