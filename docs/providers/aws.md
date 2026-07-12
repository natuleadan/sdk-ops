# AWS Provider

AWS is a cloud provider offering EC2 (VPS), EKS (managed Kubernetes), ELB/ALB
(load balancers), Route53 (DNS), and SSH key pairs. Uses the official
`aws-sdk-go-v2` Go SDK.

## Authentication

| Method | Value |
|--------|-------|
| Env var | `AWS_REGION` + `AWS_PROFILE` (or default credential chain) |
| API | `https://{service}.{region}.amazonaws.com` |

AWS uses the standard SDK credential chain: environment variables, shared
config file (`~/.aws/config`), or IAM instance profile. Only the region
is required via `AWS_REGION`.

## Supported Services

### VPS (EC2)

| Command | Description |
|---------|-------------|
| `sdk-ops provider vps create --provider aws` | Create an EC2 instance |
| `sdk-ops provider vps list --provider aws` | List EC2 instances |
| `sdk-ops provider vps get <id> --provider aws` | Get instance details |
| `sdk-ops provider vps delete <id> --provider aws` | Terminate an instance |

Flags: `--plan`, `--location`, `--template`, `--hostname`, `--ssh-key-ids`

### Managed Kubernetes (EKS)

| Command | Description |
|---------|-------------|
| `sdk-ops provider k8s create --provider aws` | Create an EKS cluster |
| `sdk-ops provider k8s list --provider aws` | List EKS clusters |
| `sdk-ops provider k8s delete <id> --provider aws` | Delete an EKS cluster |
| `sdk-ops provider k8s kubeconfig <id> --provider aws` | Generate kubeconfig |

### Load Balancer (ALB/ELB)

| Command | Description |
|---------|-------------|
| `sdk-ops provider lb create --provider aws` | Create an ALB |
| `sdk-ops provider lb list --provider aws` | List load balancers |
| `sdk-ops provider lb delete <id> --provider aws` | Delete a load balancer |

### DNS (Route53)

| Command | Description |
|---------|-------------|
| `sdk-ops provider dns list-zones --provider aws` | List hosted zones |
| `sdk-ops provider dns add-record <zone> --type A --name @ --value x.x.x.x --provider aws` | Add record |
| `sdk-ops provider dns delete-record <zone> <record> --provider aws` | Delete record |

### SSH Keys (EC2 Key Pairs)

| Command | Description |
|---------|-------------|
| `sdk-ops provider ssh-key upload <name> --pub-key <path> --provider aws` | Import a key pair |
| `sdk-ops provider ssh-key list --provider aws` | List key pairs |
| `sdk-ops provider ssh-key delete <id> --provider aws` | Delete a key pair |

### Bare Metal

AWS maps bare metal to dedicated EC2 instances (same as VPS commands).

## Not Implemented

These features are stubs (not yet implemented):
- K8s: version update, protection toggle, addon management, node pool CRUD, LB list
- LB: listeners, health checks, targets, resize, metrics, protection

## Code Location

All AWS provider code is in `providers/aws/`.
Uses `github.com/aws/aws-sdk-go-v2` SDK.
