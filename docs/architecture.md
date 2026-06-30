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
├── infra.go         ← infra init/join/ready/plan/apply/status/remove/backup/restore
│                       firewall/cert/proxy/logs/alerts
├── node.go          ← node list/info/top/exec (--all, --servers, --agents)
├── deploy.go        ← deploy init/push/encrypt/decrypt, auto Docker install, blue/green
├── cluster.go       ← cluster (16 kubectl wrappers, auto k3s install)
├── service.go       ← service status/logs/restart/rollback/versions
├── config.go        ← config init/add-node/list-nodes/remove-node/set-credentials
│                       NodeConfig with Role, Arch
├── provider.go      ← provider vps/k8s/lb/dns/ssh-key
└── backup.go        ← backup create/restore (top-level)

server.go / config.go ← High-level ops.Server API + YAML config

hooks/               ← Pre/post trigger system
├── hooks.go         ← Run(phase, vars), InitHooksDir, InstallHook

templates/           ← Project scaffolding
├── templates.go     ← Scaffold, List, InitServiceYAML
├── content.go       ← 4 templates: html, node, wordpress, go

plan/                ← Multi-node declarative provisioning
├── types.go         ← Plan, Host, Options structs
├── parse.go         ← ParseFile, Validate, fillDefaults, Summary
└── apply.go         ← Apply: SSH verify → install servers → join agents → register

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
├── upload.go        ← Tar/SSH upload, version management
├── run.go           ← Runtime detection, docker/k3s/systemd, health check
├── backup.go        ← BackupServices/RestoreServices
├── tls.go           ← Cert providers: letsencrypt, cloudflare, manual + k3s runtime support
├── logging.go       ← Promtail install + Loki config
├── alerting.go      ← Alertmanager install + Slack/Email/Telegram config
├── builder.go       ← Builder interface + DetectBuilder, BuildImage
├── builder_dockerfile.go ← Dockerfile builder (default)
├── builder_nixpacks.go   ← Nixpacks builder (auto-detect language)
├── builder_pack.go       ← Pack builder (CNB buildpacks)
├── proxy.go              ← Proxy interface + DetectProxy
├── proxy_caddy.go        ← Caddy proxy implementation
├── proxy_traefik.go      ← Traefik proxy implementation (Docker)
├── proxy_nginx.go        ← Nginx proxy implementation
├── bluegreen.go          ← Blue/green zero-downtime deploy logic
└── k8s_runtime.go        ← k3s Deployment + Service + Ingress generation

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

## Deploy Flows

### Docker runtime (default)

```
 1. Auto-detect builder         ──local──  dockerfile / nixpacks / pack / skip (compose)
 2. Build & push image          ──local──  docker buildx → registry
 3. Upload files                ──network── tar + SSH pipe → /opt/sdk-ops/services/<name>/v{N}/
 4. docker compose up -d        ──remote──  Start containers
 5. Health check                ──remote──  GET /health or /healthz → auto-rollback
```

For projects with a `docker-compose.yml` using public images, steps 1-2 are skipped.

### k3s runtime

```
 1. Upload files                ──network── tar + SSH pipe → /opt/sdk-ops/services/<name>/v{N}/
 2. Generate YAML               ──remote──  Deployment + Service + Ingress from service.yaml
 3. kubectl apply -f            ──remote──  Create k8s resources
 4. Service accessible          ──network──  http://<domain>/ via Traefik ingress
```

### Blue/green zero-downtime

```
 1. Upload new version          ──network── v{N+1} alongside v{N}
 2. Start green container       ──remote──  On a different port
 3. Health check green          ──remote──  Verify before switching traffic
 4. Switch proxy                ──remote──  Update Caddy/Traefik to point to green
 5. Stop blue container         ──remote──  Old version retired
```

### Hooks lifecycle

```
 pre-init → [hardening + Docker + k3s install] → post-init
 pre-join → [join agent to cluster]            → post-join
 pre-deploy → [upload + start + health check]  → post-deploy
 pre-remove → [uninstall k3s + Docker]         → post-remove
```

Hooks are executable scripts placed in `/opt/sdk-ops/hooks/<phase>/` on the VPS. They receive context via environment variables (`SDK_OPS_PHASE`, `SDK_OPS_IP`, etc.). Pre-hooks can abort the operation by exiting non-zero.

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
    role: server         # server or agent (auto-detected on init/join)
    arch: x86_64         # x86_64, aarch64 (auto-detected on init/join)
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
