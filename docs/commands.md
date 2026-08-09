# Commands

## sdk-ops completion — Shell completions

```bash
sdk-ops completion bash        # Generate bash completion script
sdk-ops completion zsh         # Generate zsh completion script
sdk-ops completion fish        # Generate fish completion script
```

## sdk-ops status — Unified dashboard

```bash
sdk-ops status                             # All registered nodes
sdk-ops status --node 192.0.2.100        # Single node
```

Shows per-node: hostname, runtime, agent health, CPU, memory, disk, services.

## sdk-ops state — Resource tracking

```bash
sdk-ops state show                         # All tracked resources
sdk-ops state show --type service          # Filter by type
sdk-ops state show --node 192.0.2.100    # Filter by node
sdk-ops state sync                         # Scan all nodes and update inventory
sdk-ops state sync --node 192.0.2.100    # Scan a single node
```

Resources are tracked in `~/.sdk-ops/state.yaml` and auto-recorded on
`deploy push`, `db create`, and `backup schedule`.

## sdk-ops infra — Infrastructure

### init

Provision a fresh VPS from zero.

```bash
sdk-ops infra init [ip] [flags]

Flags:
  --k3s                   Install Docker + k3s (default)
  --docker                Install Docker only
  --bare                  Only harden the OS
  --mode string           k3s, docker, or bare (default "k3s")
  -u, --user string       SSH user (default "root")
  -k, --key string        SSH private key path
  -p, --port int          SSH port (default 22)
  --insecure              Skip SSH host key verification (env: SDK_OPS_SSH_STRICT_HOST_KEY=true|1 for strict known_hosts enforcement)
  --ssh-port int          Migrate SSH to custom port (0 = keep port 22)
  --lock-root             Lock root password after creating sdkops user
  --monitor               Install Prometheus node_exporter (port 9100)
  --crowdsec              Install CrowdSec WAF/IPS
  --logs string           Install Promtail, ship logs to Loki URL
  --alerts string         Install Alertmanager with Slack webhook URL
  --firewall-allowlist string
                          Install provider IP allowlist right after hardening:
                          cf, url:<url>, dns:<fqdn>, or strict / strict:<source>
                          (normal: all ports gated except SSH; strict: all ports,
                          including SSH)
  --no-traefik            Do not install Traefik as the default reverse proxy
                          (bare/docker modes install Traefik + catch-all 404 by
                          default; k3s ships its own Traefik ingress)
  --cloud-init            Use cloud-init instead of SSH-based provisioning
  --provider string       Create VPS via provider (cubepath, hetzner, digitalocean, vultr, aws, civo)
  --plan string           VPS plan (default "gp.nano")
  --location string       VPS location (default "us-mia-1")
  --template string       OS template (default "ubuntu-24")
  --ssh-key-ids string    SSH key IDs (comma-separated)
  --api-key string        Provider API key (or env var)
  --project-id int        Provider project ID (default 4601)
  --kubeconfig string     Path to save kubeconfig (default "./kubeconfig")
  --merge                 Merge kubeconfig into ~/.kube/config
  --context string        Kubeconfig context name (default "sdk-ops-cluster")
  --disable-traefik       Disable Traefik ingress in k3s
```

**What it does:**

1. Installs packages: nftables, fail2ban, unattended-upgrades, htop
2. Creates `sdkops` user with sudo + copies SSH key
3. Kernel tuning: sysctl (syncookies, rp_filter, ptrace_scope)
4. fail2ban + unattended-upgrades
5. SSH hardening: disable password auth, restrict root login
6. nftables firewall: allow ports 22, 80, 443, 6443 (keep port 22 open); 9100
   is also opened when `--monitor` is set
7. Optional: node_exporter (--monitor), CrowdSec (--crowdsec)
8. Docker install (unless --bare)
9. k3s server + Traefik (if --k3s)
10. Optional: Promtail (--logs), Alertmanager (--alerts)
11. Fetch kubeconfig to local machine
12. Create `/opt/sdk-ops/` structure
13. Auto-register node in `~/.sdk-ops/config.yaml`
14. Optional: provider IP allowlist (--firewall-allowlist) — see the firewall
    section for profiles and behavior
15. Swap is always created first: 4x RAM, reduced (x2/x1/x0.5) while it would
    exceed 1% of the free disk, floor at 1.5x RAM (small boxes always get
    swap). Existing swapfiles are resized to the computed size.

All hardening steps run with sudo, so `infra init` can be re-run idempotently
as the hardened user (e.g. `sdkops`) after the first provision. After init,
SSH as root is disabled — connect with `--user sdkops`.

### join

```bash
sdk-ops infra join <server-ip> <agent-ip> [flags]
  --server-user    SSH user for the server (default: same as --user)
  --token          Cluster token (auto-fetched if SSH access to server)
```

### ready

Check if a node's cluster is fully operational. Exits 0 if healthy, 1 otherwise.

```bash
sdk-ops infra ready <ip> [flags]
```

Runs k3s diagnostics, verifies all nodes are Ready, and checks that core system pods are Running.

### adopt

Scan an existing server and register it without reprovisioning. Read-only — detects Docker, k3s, containers, services, and hardening, then prompts before registering.

```bash
sdk-ops infra adopt <ip> [flags]
  --force           Skip confirmation prompt
  --mode string     Override detected mode (k3s, docker, bare)
```

### status

```bash
sdk-ops infra status <ip> [flags]
```

Shows: hostname, kernel, uptime, CPU, memory, disk, nftables, fail2ban, Docker, k3s, pods.

### plan

Validate and preview a multi-node infrastructure plan before applying it.

```bash
sdk-ops infra plan <file.yaml> [flags]
```

Parses a YAML plan defining servers and agents, validates all hosts are reachable, and prints a summary of what will be provisioned.

Example `plan.yaml`:

```yaml
mode: k3s
parallel: 5
server_options:
  user: root
  ssh_key: ~/.ssh/id_ed25519
  k3s_extra_args: "--disable traefik"
agent_options:
  user: root
hosts:
  - name: server-1
    role: server
    host: 192.0.2.10
  - name: agent-1
    role: agent
    host: 192.0.2.11
```

### apply

Execute a multi-node infrastructure plan. Installs servers first, then joins agents — all in parallel.

```bash
sdk-ops infra apply <plan.yaml> [flags]
```

### remove

```bash
sdk-ops infra remove <ip> [flags]
```

Uninstalls k3s, Docker, and cleans `/opt/sdk-ops/`.

### backup

```bash
sdk-ops infra backup <ip> [flags]
```

Creates a tar.gz backup of `/opt/sdk-ops/services/` and downloads it locally.

### restore

```bash
sdk-ops infra restore <ip> <backup-file> [flags]
```

Uploads a backup tar.gz and restores services on the node.

### firewall

```bash
sdk-ops infra firewall open <port> --node <ip> [flags]
  --proto string    Protocol: tcp, udp (default "tcp")
  -n, --node        Target node IP

sdk-ops infra firewall close <port> --node <ip> [flags]
  --proto string    Protocol: tcp, udp (default "tcp")
  -n, --node        Target node IP

sdk-ops infra firewall list --node <ip> [flags]
  -n, --node        Target node IP
```

Add, remove, or list nftables firewall rules on a remote node.

#### Provider IP allowlist (cf-normal / cf-strict)

Restrict inbound traffic to provider IP ranges (Cloudflare by default), kept
up to date by a systemd timer on the node that refreshes the ranges daily.

```bash
sdk-ops infra firewall cf-normal --node <ip> [flags]
  --source string          IP list source (default "cf")
                           cf | url:<url> | dns:<fqdn>
  --cloud-firewall string  Also sync allowlist to a provider cloud firewall
                           (vultr, digitalocean, hetzner, ...)
  --no-self                Do not add your public IP as a permanent admin entry
  --open-web               Open ports 80/443 to every IP (DNS-only hosts not
                           fronted by a CDN). Default: gated to the allowlist
  -n, --node               Target node IP

sdk-ops infra firewall cf-strict --node <ip> [flags]
  --source string          IP list source (default "cf")
  --cloud-firewall string  Also sync allowlist to a provider cloud firewall
  --no-self                Do not add your public IP as a permanent admin entry
  --open-web               Open ports 80/443 to every IP (DNS-only hosts)
  --yes                    Confirm the lockout risk warning and proceed
  -n, --node               Target node IP
```

- `cf-normal` — gates every inbound port to the allowlist **except SSH**, which
  stays open from any IP. Custom SSH ports are detected from the live ruleset.
  With `--open-web`, ports 80/443 are opened to every IP (for hosts whose DNS
  is not fronted by a CDN — the web entry is the reverse proxy on those ports,
  everything else stays gated). Without it, 80/443 are gated to the allowlist
  (Cloudflare-mode: the CDN is the only public entry).
- `cf-strict` — gates every inbound port **including SSH**. Prints a lockout
  warning and requires `--yes`. Your public IP is always seeded as a permanent
  admin entry (the install aborts if it cannot be detected, unless `--no-self`).
- Sources: `cf` (Cloudflare `ips-v4`/`ips-v6`), `url:<url>` (plain-text CIDR
  list, one per line), `dns:<fqdn>` (DNS TXT records, resolving `include:`
  chains like Google's `_cloud-netblocks.googleusercontent.com`).
- The refresh is fail-open: if fetching or validation fails, the last good
  list is kept and the error is logged to `/var/log/sdk-ops-allowlist.log`.
- Every install is verified: the CLI opens a **new SSH connection** after
  applying the config and only then commits. If the new connection fails, a
  node-side auto-rollback (`systemd-run`, 30s window) restores the previous
  firewall automatically. The daily refresh also self-heals the permanent
  `admin4`/`admin6` entries if they are ever missing.
- The config only manages the `inet filter` table — Docker/iptables tables are
  never flushed. The `nftables` service is enabled so the allowlist survives
  reboots.

```bash
sdk-ops infra firewall allowlist refresh --node <ip>   # fetch and apply now
sdk-ops infra firewall allowlist status --node <ip>    # last sync + live sets
sdk-ops infra firewall allowlist remove --node <ip>    # restore pre-allowlist config
sdk-ops infra firewall allowlist admin add <ip> --node <ip>
sdk-ops infra firewall allowlist admin remove <ip> --node <ip>
sdk-ops infra firewall allowlist expose <port> --node <ip>
  -s, --scope string    admin (default), global, ips, traefik
  -g, --global          Open to all IPs (shorthand for --scope global)
      --ips string      Explicit IP/CIDR list (comma-separated, v4 and v6)
                        — shorthand for --scope ips
  -P, --proto           tcp, udp (default "tcp")
sdk-ops infra firewall allowlist unexpose <port> --node <ip>
sdk-ops infra firewall allowlist ports --node <ip>
```

Admin entries (`admin4`/`admin6` sets) are permanent bootstrap IPs and are
never touched by the daily refresh — use them to regain access if your IP
changes under `cf-strict`.

**Port exposure policy:** `allowlist expose` opens a port under a scope:
- `admin` (default) — reachable only from the permanent admin IPs, IPv4 and
  IPv6 (recommended for databases like PgDog/PostgreSQL; blocks Cloudflare
  too).
- `ips` / `--ips "ip1,ip2"` — reachable only from an explicit array of
  IPv4/IPv6 addresses or CIDRs.
- `global` — reachable from every IP.
- `traefik` — registered only (no firewall rule); HTTP services are expected
  to be routed through Traefik instead of exposing ports.

Exposed ports are recorded in `/etc/sdk-ops/firewall/ports.yaml` and
re-applied automatically after an allowlist reinstall. `allowlist ports`
lists the registry. `db create` applies this policy automatically: `--db-port`
exposes admin-only by default, `--db-global` opens to all IPs, and
`--db-ips` restricts to an explicit IP list.

**Security prechecks:** `db create` and `allowlist expose` verify the node
before any operation: nftables is enabled, and if the provider allowlist is
missing it is installed automatically (cf-normal, with the operator IP) so a
port can never be exposed without the allowlist base.

When `--cloud-firewall` is set, the allowlist is also pushed to the provider's
own cloud firewall (ports 80/443) via the provider API. Providers without
`CloudFirewall` support are skipped with a warning. This is a one-shot sync;
the daily timer only updates nftables on the node.

Note: `cf-normal`/`cf-strict` replace the nftables config (a backup is kept at
`/etc/sdk-ops/firewall/nftables.conf.bak` and restored by `allowlist remove`).
The pre-apply live table is snapshotted for the auto-rollback in case the new
config is applied but connectivity verification fails.

**IP policy — no auto-detection:** every IP (admin, peer, ban) is ALWAYS
passed explicitly via CLI or YAML. There is no IP auto-detection anywhere in
the code (no ipify/ifconfig.co/SSH-session sniffing). This prevents ban
lockouts caused by ISPs that rotate egress IPs between flows: the operator
must list their own IPs in `admin_ips` / `--admin-ips`.

```bash
# Ban/unban an explicit IP via the node's fail2ban jail
sdk-ops infra firewall ban <ip> --node <ip>
sdk-ops infra firewall unban <ip> --node <ip>
sdk-ops infra firewall bans --node <ip>
```

### provision

Provision an entire fleet (N VPSes) from one YAML file: each host runs the
full init (hardening + swap + Docker + Traefik + optional allowlist) in
parallel, then peers and bans are applied.

```yaml
mode: docker                # k3s | docker | bare
parallel: 3
firewall_allowlist: cf      # cf | url:... | dns:... | strict | "" (skip)
admin_ips: "203.0.113.10,2001:db8::1"   # explicit, never auto-detected
https_mode: cf              # cf (web 80/443 gated to the CDN allowlist, default)
                            # all (web 80/443 open to every IP — DNS-only hosts)
hosts:
  - name: edge-01
    host: 203.0.113.20    # SSH address
    peer_ip: 2001:db8::20   # peer channel (IPv6)
    user: sdkops            # root on first provision, sdkops afterwards
    ssh_key: ~/.ssh/id_ed25519
    https_mode: cf          # per-host override (host > group > global)
    traefik:                # per-host domains (host > group > global)
      enabled: true
      domains:
        - { domain: "web.com", service: hello, port: 8088, container_port: 80 }
        - { domain: "*.api.com", service: api, port: 8088, container_port: 80, wildcard: true }
  - name: edge-02
    host: 2001:db8::30
    user: sdkops
    ssh_key: ~/.ssh/id_ed25519
    https_mode: all         # v6-only host without a CDN front
ssl:
  email: "admin@x.com"      # Let's Encrypt contact (traefik domains require it)
  dns01:                    # wildcard certificates (wildcard: true needs this)
    provider: cloudflare    # cloudflare | bunny
    api_token: "..."        # CF_DNS_API_TOKEN / BUNNY_API_KEY
fail2ban:                   # jails owned by the provision (idempotent)
  sshd_bantime: 3600        # 1h first-offence ban
  recidive_bantime: 82800   # 23h repeat offenders (3 bans in 24h)
  maxretry: 5               # the fleet admin_ips are always ignored
peers:                      # per-port, per-peer access (restricted by peer_ip)
                            # peers use peer_ip (fallback: host) — IPv6 between
                            # servers keeps IPv4 free for public 80/443
  - from: edge-02
    to: edge-01
    ports: [6000]
bans:                       # explicit IPs banned on every host (fail2ban)
  - 198.51.100.7
telegram:                   # alerts when an allowlist refresh fails
  enabled: true
  api_key: "123456:ABC..."  # bot token
  chat_id: "-1001234567890"
```

Traefik router targets are derived from how Traefik runs on the node: bridge
mode routes to `http://<service>:<container_port>` over the shared
`sdk-ops-net` docker network; host-network nodes (IPv6-only) route to
`http://localhost:<port>`. Router config is picked up by the file provider
(watch: true) — no restarts on domain changes.

```bash
sdk-ops infra provision <file.yaml> --insecure
```

The fleet files live in `backend/vps-config/` (per-project convention), so the
whole VPS network is reproducible with one command.

### ops (cron/watchdog stack)

Manage the node cron stack (allowlist, security, state, traefik, logrotate).
`apply` compares the installed script against the template: identical scripts
are skipped; changed scripts are stopped, rewritten and restarted (never
written mid-execution).

```bash
sdk-ops ops apply --provision-yaml fleet.yaml [--node <ip>] [--components a,b,c]
sdk-ops ops status --provision-yaml fleet.yaml            # timers + last runs + logs
sdk-ops ops logs --node <ip> --component security --lines 20
sdk-ops ops run --node <ip> --component state             # oneshot once
sdk-ops ops enable|disable --node <ip> --component state  # toggle a timer
sdk-ops ops remove --node <ip> --component logrotate      # uninstall a cron
```

- Components: `allowlist | security | state | traefik | logrotate` (default:
  all). `--provision-yaml` targets the whole fleet; `--node` filters it or
  operates a single node without a YAML.
- `allowlist` requires `--provision-yaml` (its admin IPs come from the fleet).
- `remove allowlist` restores the pre-allowlist firewall (heavy operation).

### certs (Let's Encrypt certificates)

Issue Let's Encrypt certificates using **Traefik's own ACME resolver**
(`certResolver: letsencrypt`): Traefik obtains and renews the
certificate with HTTP-01 — no acme.sh, no shell scripts, no provider keys.
A pure-Go worker syncs the certificate from Traefik's acme.json into each
consuming service. Per-certificate (no wildcards).

```bash
sdk-ops certs issue --domain example.com --node <ip> [--services nats,traefik]
sdk-ops certs status --node <ip> [--domain example.com]           # timer + expiry
sdk-ops certs logs --node <ip> --lines 20
sdk-ops certs run --node <ip>                                     # sync once
sdk-ops certs remove --node <ip>                                  # uninstall timer + worker + store
sdk-ops certs import --domain example.com --cert-file c.pem --key-file k.pem --node <ip>
```

- `issue` writes a Traefik router (`Host(example.com)` → the `notfound` 404
  service, `certResolver: letsencrypt` on websecure) so Traefik issues the
  certificate, cross-compiles + uploads the sync worker (`/opt/sdk-ops/certs/sdk-ops`),
  installs the daily systemd timer and syncs the first certificate.
- The worker (`certs sync`, hidden, run on the node) reads `/opt/traefik/acme.json`,
  extracts the domain's certificate+key (base64 PEM), writes the central store
  `/etc/sdk-ops/certs/<domain>/` and refreshes services (idempotent: skips when
  unchanged). `--services nats` copies to `/opt/sdk-ops/nats/certs/server.{pem,key}`
  + HUP; `--services traefik` copies to `/opt/traefik/certs/<domain>/` + restart.
- Port 80 must be reachable by Let's Encrypt (HTTP-01 validation). `import` is
  for private/own-CA certificates (no renewal timer).

### uninstall

```bash
sdk-ops infra uninstall <component> --node <ip>
# components: docker | traefik | allowlist | swap | fail2ban | node-exporter | k3s | security | all
```

Selective removal of sdk-ops components (e.g. only swap, or only traefik)
without touching the rest of the stack.

### security

The SSH brute force watcher runs every 5 minutes and notifies Telegram ONLY
when attempts are detected (IP + attempt count + provider, plus a DDoS alert
for >50 unique IPs or >100 attempts in the window). Installed automatically
when `security.enabled: true` in the provision YAML; removed with
`sdk-ops infra uninstall security --node <ip>`.

### swap

```bash
sdk-ops infra swap create --node <ip>    # create/resize: 0.5x base, +0.5x per 10GB free, cap 2x
sdk-ops infra swap update --node <ip>    # force resize to the computed size
sdk-ops infra swap remove --node <ip>    # disable and delete the swap file
sdk-ops infra swap status --node <ip>    # current swap state
```

The swap rule is bottom-up: base 0.5x RAM (always created — small VPSes OOM
otherwise), +0.5x RAM for every 10GB of free disk, capped at 2x RAM. The
hardening runs it first, and the operator can re-run/create/remove it anytime.

### cert

```bash
sdk-ops infra cert install [flags]
  --domain string     Domain to provision TLS for (required)
  --email string      Email for Let's Encrypt
  --port int          Local port to proxy (default 8080)
  --provider string   Cert provider: letsencrypt, cloudflare, manual (default "letsencrypt")
  --cert-file string  Path to cert file (for --provider manual)
  --key-file string   Path to key file (for --provider manual)
  --runtime string    Runtime: docker or k3s (default "docker")
  --staging           Use Let's Encrypt staging environment
  -n, --node          Target node IP

sdk-ops infra cert info [flags]
  --domain string   Domain to check
  -n, --node        Target node IP
```

Examples:

```bash
# Let's Encrypt via Caddy (docker runtime)
sdk-ops infra cert install --domain example.com --email admin@x.com --node <ip>

# Let's Encrypt via Traefik (k3s runtime)
sdk-ops infra cert install --domain example.com --email admin@x.com --runtime k3s --node <ip>

# Upload existing cert
sdk-ops infra cert install --cert-file ./server.crt --key-file ./server.key --node <ip>
```

Install TLS certificates and configure the reverse proxy.

Providers:
- `letsencrypt` (default) — auto cert via Let's Encrypt
- `cloudflare` — Cloudflare Origin CA (detects if domain is proxied by CF)
- `manual` — upload existing cert and key files

Runtime affects how the cert is installed:
- `docker` — installs Caddy with the cert (default)
- `k3s` — configures Traefik Ingress with Let's Encrypt

### proxy

Manage reverse proxy backends on a node. Supports Caddy, Traefik, and Nginx.

```bash
sdk-ops infra proxy set --backend <type> [flags]
  --backend string    Proxy backend: caddy, traefik, nginx (required)
  --domain string     Domain name (required)
  --email string      Email for Let's Encrypt
  -n, --node          Target node IP
  -u, --user          SSH user
  -k, --key           SSH key path
  -p, --port          SSH port

sdk-ops infra proxy status [flags]
  -n, --node          Target node IP
```

Examples:

```bash
sdk-ops infra proxy set --backend caddy --domain example.com --node <ip>
sdk-ops infra proxy set --backend traefik --domain example.com --node <ip>
sdk-ops infra proxy status --node <ip>
```

### logs

```bash
sdk-ops infra logs install [flags]
  --loki string     Loki URL (required, e.g. http://loki:3100)
  -N, --name        Node name label
  --port int        Promtail HTTP port (default 9080)
  -n, --node        Target node IP

sdk-ops infra logs remove [flags]
  -n, --node        Target node IP
```

Install or remove Promtail log shipper.

### alerts

```bash
sdk-ops infra alerts install [flags]
  --slack string      Slack webhook URL
  --email string      Email for alerts
  --bot-token string  Telegram bot token
  --chat-id string    Telegram chat ID
  -n, --node          Target node IP

sdk-ops infra alerts remove [flags]
  -n, --node        Target node IP

sdk-ops infra alerts rule add <rule-file> [flags]
  -n, --node        Target node IP
```

Install Alertmanager with Slack, Email, or Telegram notifications.

## sdk-ops node — Monitoring

```bash
sdk-ops node list                              # List registered nodes
sdk-ops node info <ip>                         # Dashboard: CPU, RAM, DISK, k3s, pods
sdk-ops node top <ip>                          # Interactive htop via SSH
sdk-ops node exec [ip] -- <command>            # Run command remotely
sdk-ops node exec --all -- <command>           # Run on all registered nodes
sdk-ops node exec --servers -- <command>       # Run only on server nodes
sdk-ops node exec --agents -- <command>        # Run only on agent nodes
```

## sdk-ops deploy — Service deployment

```bash
sdk-ops deploy init <dir> --template <name> [flags]
  --template string   Template name (run without --template to list all)
  --name string       Service name (default "app")
  --ci string         Generate CI/CD config: github, gitlab
  --tested            Run integration test after scaffold (requires deployed services)

sdk-ops deploy push <dir> --node <ip> [flags]
  --name             Service name (default: directory name)
  --git              Git repository URL (clones and deploys)
  --branch string    Git branch to clone (requires --git)
  --ssh-key string   SSH key for git clone (requires --git)
  --sops-key         Auto-decrypt service.yaml with sops (age key)
  --builder string   Build method: dockerfile, nixpacks, pack (default: auto-detect)
  --runtime string   Runtime: docker (default), k3s, bare
  --domain string    Domain for k3s Ingress (required with --runtime k3s)
  --zero-downtime    Blue/green deploy with zero downtime
  --all              Deploy to all registered nodes in parallel
  -u, --user         SSH user
  -k, --key          SSH private key path
  -p, --port         SSH port

sdk-ops deploy encrypt <file> [flags]
  --age-key          Age public key for encryption

sdk-ops deploy decrypt <file>
```

**Templates:**

Generate project scaffolding with a single command:

```bash
sdk-ops deploy init ./my-site --template html           # Static HTML + Nginx
sdk-ops deploy init ./my-blog --template wordpress       # WordPress + MySQL
sdk-ops deploy init ./my-api --template node             # Node.js Express
sdk-ops deploy init ./my-svc --template go              # Go HTTP server
sdk-ops deploy init ./my-app --template nextjs           # Next.js (standalone)
sdk-ops deploy init ./my-app --template python-fastapi   # FastAPI + uvicorn
sdk-ops deploy init ./my-app --template django           # Django + gunicorn
sdk-ops deploy init ./pg --template pg-dockerized           # PostgreSQL + PgDog + pgbackrest
sdk-ops deploy init ./kv --template kv-dockerized           # Dragonfly KV + HAProxy TLS
sdk-ops deploy init ./ls --template libsql-dockerized        # libSQL + HAProxy TLS

# Infrastructure templates deploy via docker compose (not deploy push)
sdk-ops deploy init ./pg --template pg-dockerized
sdk-ops deploy init ./kv --template kv-dockerized
sdk-ops deploy init ./ls --template libsql-dockerized
cp -r ./pg /root/pg
ssh root@<ip> "cd /root/pg && bash init.sh"

# Test interactively (requires running services):
sdk-ops deploy init ./pg --template pg-dockerized --tested

# Also generate CI/CD pipeline
sdk-ops deploy init ./my-app --template go --ci github   # + .github/workflows/deploy.yml
sdk-ops deploy init ./my-app --template node --ci gitlab # + .gitlab-ci.yml
```

Each template generates a docker-compose.yml, service.yaml, and any required config files. GitHub Actions and GitLab CI templates are available via `--ci`.

**Builder backends:**

When building custom images, sdk-ops auto-detects the best builder. Override with `--builder`:

```bash
sdk-ops deploy push ./my-app --builder dockerfile    # Docker build (default if Dockerfile exists)
sdk-ops deploy push ./my-app --builder nixpacks      # Nixpacks (auto-detect language)
sdk-ops deploy push ./my-app --builder pack          # CNB buildpacks (heroku/builder:24)
```

For projects with a docker-compose.yml using public images (nginx:alpine, etc.), the builder is skipped automatically.

**Runtimes:**

```bash
sdk-ops deploy push ./my-app --runtime docker        # docker-compose up -d (default)
sdk-ops deploy push ./my-app --runtime k3s --domain app.example.com  # k3s Deployment + Service + Ingress
sdk-ops deploy push ./my-app --runtime swarm         # Docker Stack deploy
sdk-ops deploy push ./my-app --runtime bare           # Upload files only, no service start
```

**Zero-downtime deploy:**

```bash
sdk-ops deploy push ./my-app --zero-downtime         # Blue/green: start new, health check, switch traffic, stop old
```

**Deploy flow (docker runtime):**

1. Decrypt service.yaml (if --sops-key)
2. Auto-detect builder (dockerfile, nixpacks, pack) or skip for compose
3. Build image and push to registry (if builder detected)
4. Auto-install Docker on node if not present
5. Docker login to registry on node
6. Generate docker-compose.yml with optional postgres sidecar
7. Upload files to `/opt/sdk-ops/services/<name>/v{N}/`
8. `docker compose up -d` or run as systemd service
9. Health check (HTTP GET /health or /healthz)
10. Auto-rollback on failure

**Deploy flow (k3s runtime):**

1. Upload files to `/opt/sdk-ops/services/<name>/v{N}/`
2. Read service.yaml for domain, port, and image
3. Generate Deployment + Service + Ingress YAML
4. `kubectl apply -f` on the remote cluster
5. Service accessible at `http://<domain>/` via Traefik ingress

## sdk-ops service — Service management

```bash
sdk-ops service status [name]                  # Status of all or one service
sdk-ops service logs <name> [-f]               # Tail logs
sdk-ops service restart <name>                 # Restart service
sdk-ops service rollback <name> [--version v3] [--diff]  # Rollback or show diff
  --version string   Target version to rollback to (e.g. v3)
  --diff             Show changes between versions without rolling back
sdk-ops service versions <name>                # List deployed versions
sdk-ops service rotate db <container> [flags]  # Rotate DB password
  --type string      Database type: postgres, mysql, redis, mongodb (required)
  --new-pass string  Explicit password (auto-generated if empty)
sdk-ops service rotate env <service> [flags]   # Rotate env var value
  --name string      Environment variable name (required)
  --value string     Explicit value (auto-generated if empty)
```

## sdk-ops cluster — k3s cluster operations (29 commands)

```bash
# Kubectl wrappers (16)
sdk-ops cluster nodes                          # kubectl get nodes -o wide
sdk-ops cluster pods                           # kubectl get pods --all-namespaces
sdk-ops cluster services                       # kubectl get services
sdk-ops cluster deployments                    # kubectl get deployments
sdk-ops cluster ingresses                      # kubectl get ingress
sdk-ops cluster configmaps                     # kubectl get configmaps
sdk-ops cluster secrets                        # kubectl get secrets
sdk-ops cluster info                           # kubectl cluster-info
sdk-ops cluster version                        # kubectl version
sdk-ops cluster top                            # kubectl top nodes + pods
sdk-ops cluster logs <pod> [-n ns] [-f]        # kubectl logs
sdk-ops cluster exec <pod> -- <cmd>            # kubectl exec -it
sdk-ops cluster scale <res> --replicas N       # kubectl scale
sdk-ops cluster apply -f <file>               # kubectl apply
sdk-ops cluster delete <res> <name>            # kubectl delete
sdk-ops cluster describe <res> <name>          # kubectl describe

# Cluster management (7)
sdk-ops cluster token                          # Show cluster join token
sdk-ops cluster restart                        # Restart k3s service
sdk-ops cluster events [--type W] [--namespace N]  # Show cluster events (--type: Normal, Warning)
sdk-ops cluster cordon <node>                  # Mark node unschedulable
sdk-ops cluster uncordon <node>                # Mark node schedulable
sdk-ops cluster drain <node>                   # Drain node for maintenance
sdk-ops cluster label <node> <key>=<value>     # Label a node

# Upgrades and maintenance (4)
sdk-ops cluster upgrade --version X            # Upgrade k3s to a specific version
sdk-ops cluster etcd-snapshot                  # Create an etcd snapshot
sdk-ops cluster etcd-restore <snapshot-file>   # Restore etcd from snapshot
sdk-ops cluster cert-rotate                    # Rotate k3s certificates

# Resource inspection (1)
sdk-ops cluster get <type> <name> [-o yaml|json|wide]  # Get resource as YAML

# Helm (5)
sdk-ops cluster helm repo-add <name> <url>     # Add Helm repository
sdk-ops cluster helm repo-list                 # List Helm repositories
sdk-ops cluster helm install <name> <chart>    # Install a Helm chart
sdk-ops cluster helm upgrade <name> <chart>    # Upgrade a Helm release
sdk-ops cluster helm list [--namespace N]      # List Helm releases

# Advanced (2)
sdk-ops cluster node-ssh <node-name>           # SSH into a cluster node (resolves InternalIP)
sdk-ops cluster port-forward <pod> <local:remote> [-n ns]  # Forward port via SSH tunnel
```

Auto-installs k3s on the target node if not already present.

## sdk-ops backup — Backup management

```bash
sdk-ops backup create <ip> [flags]             # Backup all services from a node
sdk-ops backup restore <ip> <backup-file>       # Restore services from a backup
```

## sdk-ops config — Configuration

```bash
sdk-ops config init                           # Create ~/.sdk-ops/config.yaml
sdk-ops config add-node <ip>                  # Register a node
sdk-ops config list-nodes                     # List registered nodes
sdk-ops config remove-node <ip>               # Remove a node
sdk-ops config set-credentials                # Save provider credentials from env vars
```

## sdk-ops provider — Cloud provider resources

See [providers/](providers/) for provider-specific commands (bunny, firewall,
object-storage, cdn, block-storage).

### vps

```bash
sdk-ops provider vps create [flags]
  --plan string           VPS plan (default "gp.nano")
  --location string       Location (default "us-mia-1")
  --template string       OS template (default "ubuntu-24")
  --hostname string       Hostname
  --ssh-key-ids string    SSH key IDs (comma-separated)
  --ipv4                  Enable IPv4 (default true)
  --ipv6                  Enable IPv6 (default true)

sdk-ops provider vps list
sdk-ops provider vps delete <id>
sdk-ops provider vps export <id>               # Export as Terraform HCL
```

### k8s

```bash
# Cluster lifecycle
sdk-ops provider k8s create [flags]
  --name string           Cluster name
  --location string       Location (default "us-mia-1")
  --version string        K8s version
  --node-plan string      Node plan
  --nodes int             Number of nodes (default 3)
sdk-ops provider k8s list
sdk-ops provider k8s delete <id>
sdk-ops provider k8s kubeconfig <id>           # Download kubeconfig YAML
sdk-ops provider k8s update <id> --version X   # Upgrade K8s version
sdk-ops provider k8s protection <id>            # Toggle deletion protection

# Addons
sdk-ops provider k8s addons list <id>           # List installed addons
sdk-ops provider k8s addons available           # List available addons
sdk-ops provider k8s addons install <id> <slug> # Install an addon
sdk-ops provider k8s addons uninstall <id> <addon-id>  # Uninstall an addon

# Node pools
sdk-ops provider k8s node-pool list <id>       # List node pools
sdk-ops provider k8s node-pool add <id> --plan X --nodes N  # Add a node pool
sdk-ops provider k8s node-pool scale <id> <pool-id> --nodes N  # Scale a node pool
sdk-ops provider k8s node-pool delete <id> <pool-id>  # Delete a node pool
sdk-ops provider k8s lb-list <id>                      # List LBs attached to cluster
```

### lb

```bash
# Lifecycle
sdk-ops provider lb create [flags]
  --name string           LB name
  --location string       Location (default "us-mia-1")
  --plan string           LB plan
sdk-ops provider lb list
sdk-ops provider lb delete <id>
sdk-ops provider lb resize <id> --plan lb.medium  # Change LB plan
sdk-ops provider lb protection <id>               # Toggle deletion protection
sdk-ops provider lb metrics <id>                  # Show LB metrics

# Listeners
sdk-ops provider lb listener add <lb-id> --port 80 --target-port 8080  # Add listener
sdk-ops provider lb listener update <lb-id> <listener-id> --port 443   # Update listener
sdk-ops provider lb listener delete <lb-id> <listener-id>              # Delete listener
sdk-ops provider lb health-check <lb-id> <listener-id> --path /health # Set health check

# Targets
sdk-ops provider lb target add <lb-id> <listener-id> --type vps --uuid X --port 8080  # Add target
sdk-ops provider lb target list <lb-id> <listener-id>  # List targets
sdk-ops provider lb target drain <lb-id> <listener-id> <target-id>  # Drain a target
```

### dns

```bash
sdk-ops provider dns list-zones
sdk-ops provider dns add-record <zone-id> <type> <name> <value>
sdk-ops provider dns delete-record <zone-id> <record-id>
```

### ssh-key

```bash
sdk-ops provider ssh-key upload <name> [flags]
  --pub-key string    Path to public key file (default: ~/.ssh/id_ed25519.pub)

sdk-ops provider ssh-key list
sdk-ops provider ssh-key delete <id>
```



## sdk-ops db — Database provisioning

```bash
sdk-ops db create postgres [flags]            # Provision PostgreSQL
sdk-ops db create mysql [flags]               # Provision MySQL
sdk-ops db create redis [flags]               # Provision Redis
sdk-ops db create mongodb [flags]             # Provision MongoDB
  --name string      Database name (default: type name)
  --db-port int      Expose on external port (0 = internal only; admin-only by default)
  --db-global        Open the exposed port to all IPs (default: only admin IPs)
  --db-ips string    Explicit IP/CIDR list allowed to reach the port (comma-separated, v4 and v6)
  --db-user string   Database user (generated if empty)
  --db-pass string   Database password (generated if empty)
  --version string   Database version (e.g., 17-alpine, 8.0)
  -n, --node         Target node IP
sdk-ops db list [--node IP]                   # List databases on a node
sdk-ops db remove <name> [--node IP]          # Remove a database
```

## sdk-ops agent — Remote daemon status

```bash
sdk-ops agent status [--node IP]              # Check daemon health
sdk-ops agent status [--node IP] --agt    # Check remote daemon health (REST API)
```

## sdk-ops compose — Docker Compose management

```bash
sdk-ops compose init <path>                   # Create new docker-compose.yml
sdk-ops compose service add <name> --image X  # Add a service
sdk-ops compose service rm <name>             # Remove a service
sdk-ops compose service list                  # List services
sdk-ops compose service env set <svc> <key>=<val>  # Set env var
sdk-ops compose service env unset <svc> <key>       # Unset env var
sdk-ops compose validate                      # Validate docker-compose.yml syntax
```

## sdk-ops key — SSH key management

```bash
sdk-ops key generate <name>                   # Generate SSH key pair locally
sdk-ops key list                              # List local SSH keys
sdk-ops key deploy <name> [--node IP]         # Deploy SSH key to server
```

## sdk-ops notify — Notifications

```bash
sdk-ops notify send <title> <message> [flags]  # Send notification
sdk-ops notify test [flags]                    # Test all configured notifiers
```

Uses env vars for channels: `SLACK_WEBHOOK`, `DISCORD_WEBHOOK`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `SMTP_*`.

## sdk-ops version

```bash
sdk-ops version                               # Show version
```
