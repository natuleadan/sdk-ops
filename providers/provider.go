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
	UpdateK8s(ctx context.Context, id, version string) (*K8sCluster, error)
	ToggleK8sProtection(ctx context.Context, id string) (*K8sCluster, error)
	ListK8sAddons(ctx context.Context, id string) ([]K8sAddon, error)
	ListAvailableAddons(ctx context.Context) ([]K8sAddon, error)
	InstallK8sAddon(ctx context.Context, id, slug string) error
	UninstallK8sAddon(ctx context.Context, id, addonID string) error
	ListK8sNodePools(ctx context.Context, id string) ([]K8sNodePool, error)
	CreateK8sNodePool(ctx context.Context, id string, cfg K8sNodePoolConfig) (*K8sNodePool, error)
	ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error
	DeleteK8sNodePool(ctx context.Context, id, poolID string) error

	// Bare Metal
	CreateBareMetal(ctx context.Context, cfg BareMetalCreateConfig) (*BareMetal, error)
	DeleteBareMetal(ctx context.Context, id string) error
	ListBareMetal(ctx context.Context) ([]BareMetal, error)

	// Load Balancer
	CreateLB(ctx context.Context, cfg LBCreateConfig) (*LoadBalancer, error)
	DeleteLB(ctx context.Context, id string) error
	ListLB(ctx context.Context) ([]LoadBalancer, error)
	CreateLBListener(ctx context.Context, lbID string, cfg LBListenerConfig) (*LBListener, error)
	UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg LBListenerConfig) (*LBListener, error)
	DeleteLBListener(ctx context.Context, lbID, listenerID string) error
	SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg LBHealthCheckConfig) error
	AddLBTarget(ctx context.Context, lbID, listenerID string, cfg LBTargetConfig) (*LBTarget, error)
	ListLBTargets(ctx context.Context, lbID, listenerID string) ([]LBTarget, error)
	DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error
	ResizeLB(ctx context.Context, lbID, plan string) (*LoadBalancer, error)
	GetLBMetrics(ctx context.Context, lbID string) (string, error)
	ToggleLBProtection(ctx context.Context, lbID string) (*LoadBalancer, error)

	// DNS
	ListDNSZones(ctx context.Context) ([]DNSZone, error)
	CreateDNSRecord(ctx context.Context, zoneID string, r DNSRecord) error
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error

	// SSH Keys
	CreateSSHKey(ctx context.Context, cfg SSHKeyCreateConfig) (*SSHKey, error)
	ListSSHKeys(ctx context.Context) ([]SSHKey, error)
	DeleteSSHKey(ctx context.Context, id string) error
}
