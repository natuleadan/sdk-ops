# Civo Provider

Civo is a UK-based cloud provider offering VPS, managed Kubernetes, DNS,
load balancers, and SSH key management. Uses the official `civogo` Go SDK.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `CIVO_API_KEY` |
| CLI flag | `--api-key` |
| Header | `Authorization: bearer <token>` |
| API | `https://api.civo.com/v2` |

## Supported Services

### VPS

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider civo` | Create a VPS |
| `sdk-ops provider vps list --provider civo` | List all VPS |
| `sdk-ops provider vps get <id> --provider civo` | Get VPS details |
| `sdk-ops provider vps delete <id> --provider civo` | Delete a VPS |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

Available plans (Standard series):
- `g4s.xsmall` — 1c/1GB/25GB
- `g4s.small` — 1c/2GB/25GB
- `g4s.medium` — 2c/4GB/50GB
- `g4s.large` — 4c/8GB/100GB

### Managed Kubernetes

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider civo` | Create a cluster |
| `sdk-ops provider k8s list --provider civo` | List clusters |
| `sdk-ops provider k8s delete <id> --provider civo` | Delete a cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider civo` | Download kubeconfig |
| `sdk-ops provider k8s update <id> --version <v> --provider civo` | Upgrade cluster version |

Node pool management:
| `sdk-ops provider k8s node-pool list <cluster> --provider civo` | List node pools |
| `sdk-ops provider k8s node-pool create <cluster> --name <n> --plan <p> --nodes <c> --provider civo` | Add node pool |
| `sdk-ops provider k8s node-pool scale <cluster> <pool> --nodes <c> --provider civo` | Scale pool |
| `sdk-ops provider k8s node-pool delete <cluster> <pool> --provider civo` | Delete pool |

Available addons:
| `sdk-ops provider k8s addons available --provider civo` | List available marketplace apps |

### Load Balancer

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider civo` | Create a load balancer |
| `sdk-ops provider lb list --provider civo` | List load balancers |
| `sdk-ops provider lb delete <id> --provider civo` | Delete a load balancer |
| `sdk-ops provider lb listener add <lb> --port <p> --target-port <tp> --provider civo` | Add listener/backend |
| `sdk-ops provider lb listener delete <lb> <listener> --provider civo` | Delete listener |
| `sdk-ops provider lb listener update <lb> <listener> --port <p> --target-port <tp> --provider civo` | Update listener |
| `sdk-ops provider lb target add <lb> <listener> --type <t> --target-id <ip> --port <p> --provider civo` | Add backend target |
| `sdk-ops provider lb target list <lb> <listener> --provider civo` | List backends |
| `sdk-ops provider lb target drain <lb> <listener> <target> --provider civo` | Remove a backend |

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider civo` | List DNS zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value x.x.x.x --provider civo` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider civo` | Delete record |

### SSH Keys

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider civo` | Upload SSH key |
| `sdk-ops provider ssh-key list --provider civo` | List SSH keys |
| `sdk-ops provider ssh-key delete <id> --provider civo` | Delete SSH key |

## Known Limitations

- No bare metal servers available
- No addon install/uninstall for K8s (only listing available)
- LB health checks and metrics not exposed via API
- `--location` required for VPS create and K8s kubeconfig/delete

## Code Location

All Civo provider code is in `providers/civo/`.
Uses `github.com/civo/civogo` SDK.
