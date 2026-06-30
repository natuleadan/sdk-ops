# Commands

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
  --insecure              Skip SSH host key verification
  --ssh-port int          Migrate SSH to custom port (0 = keep port 22)
  --lock-root             Lock root password after creating sdkops user
  --monitor               Install Prometheus node_exporter (port 9100)
  --crowdsec              Install CrowdSec WAF/IPS
  --logs string           Install Promtail, ship logs to Loki URL
  --alerts string         Install Alertmanager with Slack webhook URL
  --cloud-init            Use cloud-init instead of SSH-based provisioning
  --provider string       Create VPS via provider (cubepath, hetzner, digitalocean, vultr, aws)
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
6. nftables firewall: allow ports 22, 80, 443, 6443 (keep port 22 open)
7. Optional: node_exporter (--monitor), CrowdSec (--crowdsec)
8. Docker install (unless --bare)
9. k3s server + Traefik (if --k3s)
10. Optional: Promtail (--logs), Alertmanager (--alerts)
11. Fetch kubeconfig to local machine
12. Create `/opt/sdk-ops/` structure
13. Auto-register node in `~/.sdk-ops/config.yaml`

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
    host: 192.168.1.10
  - name: agent-1
    role: agent
    host: 192.168.1.11
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
  --template string   Template name: html, node, wordpress, go
  --name string       Service name (default "app")

sdk-ops deploy push <dir> --node <ip> [flags]
  --name             Service name (default: directory name)
  --git              Git repository URL (clones and deploys)
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
sdk-ops deploy init ./my-site --template html        # Static HTML + Nginx
sdk-ops deploy init ./my-blog --template wordpress    # WordPress + MySQL
sdk-ops deploy init ./my-api --template node          # Node.js Express
sdk-ops deploy init ./my-svc --template go           # Go HTTP server
```

Each template generates a docker-compose.yml, service.yaml, and any required config files.

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
sdk-ops service rollback <name>                # Rollback to previous version
sdk-ops service versions <name>                # List deployed versions
```

## sdk-ops cluster — k3s cluster operations

```bash
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
sdk-ops provider k8s create [flags]
  --name string           Cluster name
  --location string       Location (default "us-mia-1")
  --version string        K8s version
  --node-plan string      Node plan
  --nodes int             Number of nodes (default 3)

sdk-ops provider k8s list
sdk-ops provider k8s delete <id>
sdk-ops provider k8s kubeconfig <id>           # Download kubeconfig YAML
```

### lb

```bash
sdk-ops provider lb create [flags]
  --name string           LB name
  --location string       Location (default "us-mia-1")
  --plan string           LB plan

sdk-ops provider lb list
sdk-ops provider lb delete <id>
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

## sdk-ops version

```bash
sdk-ops version                               # Show version
```
