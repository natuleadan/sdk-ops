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
sdk-ops status --node 192.168.1.100        # Single node
```

Shows per-node: hostname, runtime, agent health, CPU, memory, disk, services.

## sdk-ops state — Resource tracking

```bash
sdk-ops state show                         # All tracked resources
sdk-ops state show --type service          # Filter by type
sdk-ops state show --node 192.168.1.100    # Filter by node
sdk-ops state sync                         # Scan all nodes and update inventory
sdk-ops state sync --node 192.168.1.100    # Scan a single node
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
  --db-port int      Expose on external port (0 = internal only)
  --db-user string   Database user (generated if empty)
  --db-pass string   Database password (generated if empty)
  --version string   Database version (e.g., 17-alpine, 8.0)
  -n, --node         Target node IP
sdk-ops db list [--node IP]                   # List databases on a node
sdk-ops db remove <name> [--node IP]          # Remove a database
```

## sdk-ops agent — On-VPS monitoring agent

```bash
sdk-ops agent install [--node IP] [flags]     # Deploy agent (systemd default)
  --runtime string   Runtime: bare (default), docker
sdk-ops agent status [--node IP]              # Check agent health
sdk-ops agent logs [--node IP] [--tail N]     # Show agent logs
sdk-ops agent uninstall [--node IP] [flags]   # Remove agent
  --yes              Skip confirmation prompt
  --purge            Also remove agent data (audit, metrics, schedules)
sdk-ops agent update [--node IP] [flags]      # Check and apply update
  --force            Rebuild even if no update
sdk-ops agent schedule add <name> [flags]     # Add scheduled task
  --cron string      Cron expression (required)
  --task string      Task type: shell, backup-services, backup-database, docker-cleanup
  --config string    Task configuration (JSON)
sdk-ops agent schedule list [--node IP]       # List scheduled tasks
sdk-ops agent schedule rm <id> [--node IP]    # Remove a scheduled task
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
