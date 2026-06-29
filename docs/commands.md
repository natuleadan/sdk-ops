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

### status

```bash
sdk-ops infra status <ip> [flags]
```

Shows: hostname, kernel, uptime, CPU, memory, disk, nftables, fail2ban, Docker, k3s, pods.

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
  --domain string   Domain to provision TLS for (required)
  --email string    Email for Let's Encrypt (required)
  --port int        Local port to proxy (default 8080)
  --staging         Use Let's Encrypt staging environment
  -n, --node        Target node IP

sdk-ops infra cert info [flags]
  --domain string   Domain to check
  -n, --node        Target node IP
```

Install Caddy and provision TLS certificates via Let's Encrypt.

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
sdk-ops node exec --all -- <command>            # Run on all registered nodes
```

## sdk-ops deploy — Service deployment

```bash
sdk-ops deploy push <dir> --node <ip> [flags]
  --name             Service name (default: directory name)
  --git              Git repository URL (clones and deploys)
  --sops-key         Auto-decrypt service.yaml with sops (age key)
  --all              Deploy to all registered nodes in parallel
  -u, --user         SSH user
  -k, --key          SSH private key path
  -p, --port         SSH port

sdk-ops deploy encrypt <file> [flags]
  --age-key          Age public key for encryption

sdk-ops deploy decrypt <file>
```

**Deploy flow:**

1. Decrypt service.yaml (if --sops-key)
2. Build Go binary for linux/amd64 (if Go source found)
3. Auto-install Docker on node if not present
4. Docker login to registry on node
5. Build Docker image, push to registry
6. Generate docker-compose.yml with optional postgres sidecar
7. Upload files to `/opt/sdk-ops/services/<name>/v{N}/`
8. `docker compose up -d` or run as systemd service
9. Health check (HTTP GET /health or /healthz)
10. Auto-rollback on failure

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
