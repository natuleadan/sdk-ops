# CubePath Provider

CubePath is a cloud provider offering VPS, managed Kubernetes, load balancers,
DNS, SSH keys, and bare metal servers. The provider uses raw HTTP against the
CubePath REST API (no official Go SDK).

## Authentication

| Method | Value |
|--------|-------|
| Env var | `CUBEPATH_API_KEY` |
| CLI flag | `--api-key` |
| Header | `X-API-Key` |
| Base URL | `https://api.cubepath.com` |

## Supported Services

### VPS

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider cubepath` | Create a VPS |
| `sdk-ops provider vps list --provider cubepath` | List all VPS |
| `sdk-ops provider vps get <id> --provider cubepath` | Get VPS details |
| `sdk-ops provider vps delete <id> --provider cubepath` | Delete a VPS |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

### Managed Kubernetes

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider cubepath` | Create a cluster |
| `sdk-ops provider k8s list --provider cubepath` | List clusters |
| `sdk-ops provider k8s delete <id> --provider cubepath` | Delete a cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider cubepath` | Download kubeconfig |
| `sdk-ops provider k8s update <id> --version <ver> --provider cubepath` | Upgrade version |
| `sdk-ops provider k8s protection <id> --provider cubepath` | Toggle deletion protection |

Node pool management:
| `sdk-ops provider k8s node-pool list <cluster> --provider cubepath` | List node pools |
| `sdk-ops provider k8s node-pool create <cluster> --name <n> --plan <p> --nodes <c> --provider cubepath` | Add node pool |
| `sdk-ops provider k8s node-pool scale <cluster> <pool> --nodes <c> --provider cubepath` | Scale pool |
| `sdk-ops provider k8s node-pool delete <cluster> <pool> --provider cubepath` | Delete pool |

Addon management:
| `sdk-ops provider k8s addons list <cluster> --provider cubepath` | Installed addons |
| `sdk-ops provider k8s addons available --provider cubepath` | Available addons |
| `sdk-ops provider k8s addons install <cluster> --addon <slug> --provider cubepath` | Install addon |

Cluster load balancers:
| `sdk-ops provider k8s lb-list <cluster> --provider cubepath` | List LBs in cluster |

### Load Balancer

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider cubepath` | Create a load balancer |
| `sdk-ops provider lb list --provider cubepath` | List load balancers |
| `sdk-ops provider lb delete <id> --provider cubepath` | Delete a load balancer |
| `sdk-ops provider lb listener add <lb> --port <p> --target-port <tp> --protocol <pr> --provider cubepath` | Add listener |
| `sdk-ops provider lb health-check set <lb> <listener> --protocol <pr> --path </healthz> --provider cubepath` | Set health check |
| `sdk-ops provider lb target add <lb> <listener> --type <t> --target-id <id> --port <p> --provider cubepath` | Add backend target |
| `sdk-ops provider lb resize <id> --plan <plan> --provider cubepath` | Change plan |
| `sdk-ops provider lb metrics <id> --provider cubepath` | Get metrics |
| `sdk-ops provider lb protection <id> --provider cubepath` | Toggle protection |

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider cubepath` | List DNS zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value 1.2.3.4 --ttl 120 --provider cubepath` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider cubepath` | Delete record |

### SSH Keys

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider cubepath` | Upload SSH key |
| `sdk-ops provider ssh-key list --provider cubepath` | List SSH keys |
| `sdk-ops provider ssh-key delete <id> --provider cubepath` | Delete SSH key |

### Bare Metal

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --plan <plan> --location <loc> --template <os> --provider cubepath` | Deploy bare metal (use plan from bare metal catalog) |
| `sdk-ops provider vps list --provider cubepath` | List bare metal servers |

## Known Limitations

- API rate limit: 5 requests per 5 minutes
- Load balancer cannot be deleted while in "deploying" state
- Bare metal servers cannot be destroyed via API (physical hardware)

## Code Location

All CubePath provider code is in `providers/cubepath/`.
The full OpenAPI spec is in `providers/cubepath/cubepath-api.json`.
