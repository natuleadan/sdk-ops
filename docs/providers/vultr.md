# Vultr Provider

Vultr is a cloud provider offering VPS, managed Kubernetes (VKE), load
balancers, DNS, firewalls, S3-compatible object storage, CDN, and block
storage. Uses the official `govultr/v3` Go SDK.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `VULTR_API_KEY` |
| CLI flag | `--api-key` |
| Header | `Authorization: Bearer <token>` |
| API | `https://api.vultr.com/v2` |

## Supported Services

### VPS

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider vultr` | Create a VPS |
| `sdk-ops provider vps list --provider vultr` | List all VPS |
| `sdk-ops provider vps get <id> --provider vultr` | Get VPS details |
| `sdk-ops provider vps delete <id> --provider vultr` | Delete a VPS |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

Available plans by type:
- `vc2-*` — Regular shared CPU (e.g. `vc2-1c-1gb`, `vc2-2c-2gb`)
- `vhf-*` — High Frequency Intel (e.g. `vhf-1c-1gb`, `vhf-3c-8gb`)
- `vhp-*` — High Performance AMD/Intel dedicated (e.g. `vhp-4c-8gb-amd`)

### Managed Kubernetes (VKE)

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider vultr` | Create a cluster |
| `sdk-ops provider k8s list --provider vultr` | List clusters |
| `sdk-ops provider k8s delete <id> --provider vultr` | Delete a cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider vultr` | Download kubeconfig |
| `sdk-ops provider k8s update <id> --version <v> --provider vultr` | Upgrade cluster version |

Node pool management:
| `sdk-ops provider k8s node-pool list <cluster> --provider vultr` | List node pools |
| `sdk-ops provider k8s node-pool create <cluster> --name <n> --plan <p> --nodes <c> --provider vultr` | Add node pool |
| `sdk-ops provider k8s node-pool scale <cluster> <pool> --nodes <c> --provider vultr` | Scale pool |
| `sdk-ops provider k8s node-pool delete <cluster> <pool> --provider vultr` | Delete pool |

**Note:** VKE requires minimum `vc2-2c-2gb` plan for node pools. The 1c plans
are not supported for Kubernetes.

### Load Balancer

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider vultr` | Create a load balancer |
| `sdk-ops provider lb list --provider vultr` | List load balancers |
| `sdk-ops provider lb delete <id> --provider vultr` | Delete a load balancer |

**Note:** Vultr uses forwarding rules instead of CubePath-style listeners:

| `sdk-ops provider lb listener add <lb> --port <p> --target-port <tp> --protocol <pr> --provider vultr` | Add forwarding rule |
| `sdk-ops provider lb listener delete <lb> <listener> --provider vultr` | Delete forwarding rule |

The balancing algorithm is `leastconn` (Vultr does not support `round_robin`).

### DNS

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider vultr` | List DNS zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value x.x.x.x --provider vultr` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider vultr` | Delete record |

### Firewall

| Command | Description |
|---------|-------------|
| `sdk-ops provider firewall group create <desc> --provider vultr` | Create a firewall group |
| `sdk-ops provider firewall group list --provider vultr` | List firewall groups |
| `sdk-ops provider firewall group delete <id> --provider vultr` | Delete a group |
| `sdk-ops provider firewall rule list <group> --provider vultr` | List rules in a group |
| `sdk-ops provider firewall rule add <group> --provider vultr` | Add a rule (SSH allow by default) |
| `sdk-ops provider firewall rule delete <group> <rule> --provider vultr` | Delete a rule |

### Object Storage (S3-compatible)

| Command | Description |
|---------|-------------|
| `sdk-ops provider object-storage clusters --provider vultr` | List available S3 clusters per region |
| `sdk-ops provider object-storage list --provider vultr` | List existing S3 buckets |
| `sdk-ops provider object-storage create --provider vultr` | Create an S3 bucket (uses `--nodes` as cluster_id) |

**Note:** New buckets require both `cluster_id` and `tier_id`. Use the raw
API or dashboard for initial setup, then manage with CLI.

### CDN

| Command | Description |
|---------|-------------|
| `sdk-ops provider cdn list --provider vultr` | List CDN pull zones |
| `sdk-ops provider cdn create <name> <origin> --provider vultr` | Create a CDN zone |
| `sdk-ops provider cdn delete <id> --provider vultr` | Delete a CDN zone |
| `sdk-ops provider cdn purge <id> --provider vultr` | Purge CDN cache |

### Block Storage

| Command | Description |
|---------|-------------|
| `sdk-ops provider block-storage list --provider vultr` | List block storage volumes |
| `sdk-ops provider block-storage create <label> <region> <size-gb> --provider vultr` | Create a volume |
| `sdk-ops provider block-storage delete <id> --provider vultr` | Delete a volume |
| `sdk-ops provider block-storage attach <id> <instance-id> --provider vultr` | Attach to a VPS |
| `sdk-ops provider block-storage detach <id> --provider vultr` | Detach from a VPS |

### SSH Keys

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider vultr` | Upload SSH key |
| `sdk-ops provider ssh-key list --provider vultr` | List SSH keys |
| `sdk-ops provider ssh-key delete <id> --provider vultr` | Delete SSH key |

### Bare Metal

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --plan <plan> --location <loc> --template <os> --provider vultr` | Deploy bare metal server |

## Known Limitations

- LB algorithm must be `leastconn` (not `round_robin`)
- Kubernetes requires minimum `vc2-2c-2gb` node plan
- Object Storage creation requires both `cluster_id` and `tier_id`
- No K8s addon management (Vultr does not support addons)
- No LB protection toggle or metrics endpoint

## Code Location

All Vultr provider code is in `providers/vultr/`.
Uses `github.com/vultr/govultr/v3` SDK.
Vultr-specific CLI commands are in `cmd/sdk-ops/vultr_cmds.go`.
