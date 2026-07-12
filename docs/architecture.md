# Architecture

## Overview

sdk-ops is a single binary CLI (`sdk-ops`) that provisions and operates servers via SSH. It has an optional on-VPS monitoring agent (systemd or Docker) for health checks, metrics, and scheduled tasks.

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
│  │ nftables │  │ k3s  │  │ Agent   │  │
│  │ fail2ban │  │      │  │ :9000   │  │
│  │ sysctl   │  │      │  │ 10 mon  │  │
│  │ node_exp │  │ Docker│  │ SQLite  │  │
│  └─────────┘  └──────┘  └─────────┘  │
│                                       │
│  /opt/sdk-ops/                        │
│  ├── services/    ← deployed apps     │
│  ├── agent-data/  ← metrics + audit   │
│  ├── backups/     ← service backups   │
│  └── logs/        ← service logs      │
└───────────────────────────────────────┘
```

## Package Structure (SDK)

```
agent/               ← On-VPS monitoring agent
├── main.go          ← Entrypoint: lifecycle + API server (:9000)
├── api.go           ← HTTP API: /health, /metrics, /audit, /schedules, /events, /exec, /inventory
├── health.go        ← 10 monitors: containers, disk, SSL, network, temperature
├── metrics.go       ← CPU, RAM, disk, Docker stats (gopsutil)
├── scheduler.go     ← Cron scheduler (robfig/cron)
├── notify.go        ← Notification sending from agent
├── events.go        ← Docker event watcher + log pattern watcher
├── update.go        ← GitHub release checker
├── config.go        ← Config loading
└── db.go            ← SQLite storage (metrics, audit, schedules, events)

cmd/sdk-ops/          ← Cobra CLI root + all subcommands (18 commands)
├── main.go          ← Root command, 15 subcommands
├── infra.go         ← infra init/join/adopt/ready/plan/apply/status/remove/backup/restore
│                       firewall/cert/proxy/logs/alerts
├── node.go          ← node list/info/top/exec (--all, --servers, --agents)
├── deploy.go        ← deploy init/push/encrypt/decrypt + service status/logs/restart/
│                       rollback/versions/rotate db/env
├── cluster.go       ← cluster (16 kubectl wrappers, auto k3s install)
├── agent.go         ← agent install/status/logs/uninstall/update/schedule
├── config.go        ← config init/add-node/list-nodes/remove-node/set-credentials
├── provider.go      ← provider vps/k8s/lb/dns/ssh-key
├── backup.go        ← backup create/restore/schedule/unschedule/list-schedules
├── db.go            ← db create/list/remove (postgres, mysql, redis, mongodb)
├── compose.go       ← compose init/service/validate
├── key.go           ← key generate/list/deploy
├── notify.go        ← notify send/test
├── state.go         ← state show/sync (resource inventory)
├── status.go        ← status (unified multi-node dashboard)
├── spinner.go       ← CLI spinner animation
├── bunny.go         ← bunny (app/dns/pullzone/script/storage/stream/shield – 7 groups)
└── vultr_cmds.go    ← provider firewall/object-storage/cdn/block-storage (vultr-specific)

server.go / config.go ← High-level ops.Server API + YAML config

hooks/               ← Pre/post trigger system
├── hooks.go         ← Run(phase, vars), InitHooksDir, InstallHook

templates/           ← Project scaffolding
├── templates.go     ← Scaffold, List, InitServiceYAML, InitCICD
├── content.go       ← 9 templates: html, node, wordpress, go, nextjs, python-fastapi, django
│                       + CI/CD: github-actions, gitlab-ci

plan/                ← Multi-node declarative provisioning
├── types.go         ← Plan, Host, Options structs
├── parse.go         ← ParseFile, Validate, fillDefaults, Summary
└── apply.go         ← Apply: SSH verify → install servers → join agents → register

cloudinit/           ← Cloud-init user-data generation (--cloud-init)

terraform/           ← Terraform HCL export (provider vps export)

secrets/             ← sops encryption/decryption helpers (deploy encrypt/decrypt)

ssh/                 ← SSH client (connect, exec, stream, PTY, agent support)
├── client.go        ← SSH Client + KnownHosts + agent auth + configurable HostKeyCallback (env: SDK_OPS_SSH_STRICT_HOST_KEY)

hardening/           ← Step-by-step server hardening (10 steps)
├── apply.go         ← Orchestrator (calls steps in order)
├── steps.go         ← 10 steps: packages, user, kernel, remove_unused, fail2ban, SSH, nftables, auditd, lynis, node_exporter
├── firewall.go      ← FirewallOpen/Close/List via nftables
└── hconfig.go       ← YAML config export/import

docker/              ← Docker install + health check (auto sudo support)

k3s/                 ← k3s install + join (auto sudo support)

deploy/              ← Service lifecycle
├── upload.go        ← Tar/SSH upload, version management
├── run.go           ← Runtime detection, docker/k3s/systemd, health check (configurable)
├── backup.go        ← BackupServices/RestoreServices + S3 upload
├── database.go      ← DB provisioning (postgres, mysql, redis, mongodb)
├── rotate.go        ← Secrets rotation (DB passwords, env vars)
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
├── bare_runtime.go       ← Bare metal systemd deploy
├── swarm_runtime.go      ← Docker Swarm deploy
└── k8s_runtime.go        ← k3s Deployment + Service + Ingress generation

monitor/             ← Remote stats (CPU, RAM, disk, k3s status, top processes)

notify/              ← Notifications (Slack, Discord, Telegram, Email, Webhook)

compose/             ← Docker Compose YAML manipulation

providers/           ← Multi-provider interface
├── provider.go      ← Provider interface (49 methods: VPS, K8s, LB advanced, DNS, SSHKey)
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
│   ├── k8s.go       ← VKE + node pools + upgrade
│   ├── lb_dns.go    ← LB + forwarding rules + DNS + BareMetal
│   ├── sshkey.go    ← SSH key management
│   ├── firewall.go  ← Firewall groups + rules
│   ├── object_storage.go ← S3-compatible storage
│   ├── cdn.go       ← CDN pull zones
│   └── block_storage.go ← Block storage volumes
├── aws/             ← AWS (aws-sdk-go-v2)
    ├── client.go    ← Constructor (4 service clients)
    ├── vps.go       ← EC2 instances
    ├── k8s.go       ← EKS + kubeconfig generation
    ├── lb_dns.go    ← ELBv2 + Route53 + BareMetal
    └── sshkey.go    ← SSH key management

bunny/             ← Bunny.net SDK (standalone, not a provider)
├── client.go    ← HTTP client (AccessKey auth, 6 API base URLs)
├── types.go     ← All shared types (MC, DNS, CDN, Shield, Stream, Storage...)
├── dns.go       ← DNS zones + records + geo-routing
├── pullzone.go  ← CDN Pull Zones + edge rules + cache purge
├── mc_app.go    ← Magic Containers app CRUD
├── mc_container.go ← Container templates
├── mc_deploy.go ← Deploy flow with registry auto-detect
├── mc_endpoint.go  ← CDN + Anycast endpoints
├── mc_region.go ← Regions, autoscaling, nodes
├── mc_registry.go  ← Container registries
├── mc_volume.go ← Persistent volumes, log forwarding
├── storage.go   ← Edge Storage zones + files
├── stream.go    ← Stream video libraries + videos
├── shield.go    ← Shield WAF zones + rate limits
├── edgescript.go ← Edge Scripting (23 endpoints)
├── logging.go   ← CDN logging query
├── origin_errors.go ← Origin error retrieval
└── spec/        ← 8 OpenAPI JSON specs

cmd/sdk-ops/
├── bunny.go     ← CLI: sdk-ops bunny <app|dns|pullzone|script|storage|stream|shield>
└── vultr_cmds.go ← CLI: sdk-ops provider firewall|object-storage|cdn|block-storage

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
| 3. Kernel tuning | sysctl: syncookies, rp_filter, send_redirects=0, ptrace_scope | no |
| 4. Remove unused services | telnet, ftp, rsh, rpcbind, avahi, cups | no |
| 5. fail2ban | SSH protection, 1h ban after 5 retries | no |
| 6. SSH config | CIS: PermitRootLogin no, MaxAuthTries 3, no password auth | no |
| 7. nftables | Drop by default, allow 22/80/443/6443 | no |
| 8. auditd | System auditing daemon | `--auditd` |
| 9. lynis | Security auditor | `--lynis` |
| 10. usg | Ubuntu Security Guide (CIS Level 1/2) | `--usg` |
| 11. node_exporter | Prometheus node_exporter on port 9100 | `--monitor` |
| 11. SSH port migration | Add new port + keep port 22 | `--ssh-port N` |
| 12. Lock root | Lock root password after user creation | `--lock-root` |

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

## Infrastructure Templates

sdk-ops provides directory-based infrastructure templates under `templates/`:

```
templates/
├── pg-dockerized/         # PostgreSQL 18 + PgDog + pgbackrest + replica
│   ├── Dockerfile       # Custom image with pgbackrest pre-installed
│   ├── docker-compose.yml
│   ├── init.sh          # SSL + primary + replica + PgDog
│   ├── backup.sh        # pgbackrest full backup
│   ├── restore.sh       # Full/PITR restore (--delta, --set, --yes)
│   ├── validate.sh      # Health checks (inside Docker)
│   ├── gen-certs.sh     # SSL certificate generation
│   └── test/test.sh     # PITR integration test
├── kv-dockerized/         # Dragonfly KV + HAProxy TLS + replica
│   ├── docker-compose.yml
│   ├── haproxy.cfg      # TLS termination (workaround for tini bug)
│   ├── init.sh          # SSL + cluster + REPLICAOF
│   ├── backup.sh        # BGSAVE → local + S3
│   ├── restore.sh       # .dfs snapshot restore
│   └── test/test.sh     # PITR integration test
└── libsql-dockerized/     # libSQL + HAProxy TLS + WAL snapshots
```

Infrastructure templates deploy via copy + `bash init.sh`, not `deploy push`:

```bash
sdk-ops deploy init ./pg --template pg-dockerized
scp -r ./pg root@<ip>:/root/pg
ssh root@<ip> "cd /root/pg && bash init.sh"
```

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
