# DigitalOcean Provider

DigitalOcean is a US-based cloud provider offering VPS (droplets), managed
Kubernetes (DOKS), load balancers, DNS, and SSH key management. Uses the
official `godo` Go SDK.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `DIGITALOCEAN_TOKEN` |
| CLI flag | `--api-key` |
| API | `https://api.digitalocean.com/v2` |

## Supported Services

### VPS

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider digitalocean` | Create a droplet |
| `sdk-ops provider vps list --provider digitalocean` | List all droplets |
| `sdk-ops provider vps get <id> --provider digitalocean` | Get droplet details |
| `sdk-ops provider vps delete <id> --provider digitalocean` | Delete a droplet |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

### Managed Kubernetes (DOKS)

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider digitalocean` | Create a cluster |
| `sdk-ops provider k8s list --provider digitalocean` | List clusters |
| `sdk-ops provider k8s delete <id> --provider digitalocean` | Delete a cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider digitalocean` | Download kubeconfig |

### Load Balancer

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider digitalocean` | Create a load balancer |
| `sdk-ops provider lb list --provider digitalocean` | List load balancers |
| `sdk-ops provider lb delete <id> --provider digitalocean` | Delete a load balancer |

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider digitalocean` | List DNS zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value x.x.x.x --provider digitalocean` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider digitalocean` | Delete record |

### SSH Keys

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider digitalocean` | Upload SSH key |
| `sdk-ops provider ssh-key list --provider digitalocean` | List SSH keys |
| `sdk-ops provider ssh-key delete <id> --provider digitalocean` | Delete SSH key |

## Not Implemented

These features are stubs (DigitalOcean API does not support them or not yet implemented):
- K8s: version update, protection toggle, addon management, node pool CRUD, LB list
- LB: listeners, health checks, targets, resize, metrics, protection
- Bare metal servers

## Code Location

All DigitalOcean provider code is in `providers/digitalocean/`.
Uses `github.com/digitalocean/godo` SDK.
