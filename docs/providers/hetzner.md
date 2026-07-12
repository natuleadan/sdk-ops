# Hetzner Provider

Hetzner is a German cloud provider offering VPS (cloud servers), managed
Kubernetes, load balancers, DNS, and SSH key management. Uses the official
`hcloud-go` Go SDK. DNS uses the separate Hetzner DNS API.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `HETZNER_API_TOKEN` |
| CLI flag | `--api-key` |
| API (VPS/K8s/LB) | `https://api.hetzner.cloud/v1` |
| API (DNS) | `https://dns.hetzner.com/api/v1` |

## Supported Services

### VPS

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider hetzner` | Create a VPS |
| `sdk-ops provider vps list --provider hetzner` | List all VPS |
| `sdk-ops provider vps get <id> --provider hetzner` | Get VPS details |
| `sdk-ops provider vps delete <id> --provider hetzner` | Delete a VPS |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

### Managed Kubernetes

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider hetzner` | Create a cluster |
| `sdk-ops provider k8s list --provider hetzner` | List clusters |
| `sdk-ops provider k8s delete <id> --provider hetzner` | Delete a cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider hetzner` | Download kubeconfig |

### Load Balancer

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider hetzner` | Create a load balancer |
| `sdk-ops provider lb list --provider hetzner` | List load balancers |
| `sdk-ops provider lb delete <id> --provider hetzner` | Delete a load balancer |

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider hetzner` | List DNS zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value x.x.x.x --provider hetzner` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider hetzner` | Delete record |

### SSH Keys

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider hetzner` | Upload SSH key |
| `sdk-ops provider ssh-key list --provider hetzner` | List SSH keys |
| `sdk-ops provider ssh-key delete <id> --provider hetzner` | Delete SSH key |

## Not Implemented

These features are stubs (Hetzner API does not support them or not yet implemented):
- K8s: version update, protection toggle, addon management, node pool CRUD, LB list
- LB: listeners, health checks, targets, resize, metrics, protection
- Bare metal servers

## Code Location

All Hetzner provider code is in `providers/hetzner/`.
Uses `github.com/hetznercloud/hcloud-go/v2` SDK.
