package providers

import "context"

// Provider defines all operations a cloud provider can support.
// Each method returns "not implemented" if the provider doesn't offer that service.
type Provider interface {
	// VPS
	CreateVPS(ctx context.Context, cfg VPSCreateConfig) (*VPS, error)
	DeleteVPS(ctx context.Context, id string) error
	ListVPS(ctx context.Context) ([]VPS, error)
	GetVPS(ctx context.Context, id string) (*VPS, error)

	// Kubernetes
	CreateK8s(ctx context.Context, cfg K8sCreateConfig) (*K8sCluster, error)
	DeleteK8s(ctx context.Context, id string) error
	ListK8s(ctx context.Context) ([]K8sCluster, error)
	GetK8s(ctx context.Context, id string) (*K8sCluster, error)
	GetKubeconfig(ctx context.Context, id string) (string, error)

	// Bare Metal
	CreateBareMetal(ctx context.Context, cfg BareMetalCreateConfig) (*BareMetal, error)
	DeleteBareMetal(ctx context.Context, id string) error
	ListBareMetal(ctx context.Context) ([]BareMetal, error)

	// Load Balancer
	CreateLB(ctx context.Context, cfg LBCreateConfig) (*LoadBalancer, error)
	DeleteLB(ctx context.Context, id string) error
	ListLB(ctx context.Context) ([]LoadBalancer, error)

	// DNS
	ListDNSZones(ctx context.Context) ([]DNSZone, error)
	CreateDNSRecord(ctx context.Context, zoneID string, r DNSRecord) error
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error

	// SSH Keys
	CreateSSHKey(ctx context.Context, cfg SSHKeyCreateConfig) (*SSHKey, error)
	ListSSHKeys(ctx context.Context) ([]SSHKey, error)
	DeleteSSHKey(ctx context.Context, id string) error
}
