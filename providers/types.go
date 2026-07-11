package providers

// --- VPS ---

type VPSCreateConfig struct {
	Label      string
	Plan       string
	Location   string
	Template   string
	Hostname   string
	SSHKeyIDs  []string
	EnableIPv4 bool
	EnableIPv6 bool
	Backups    bool
	User       string
	Password   string
	UserData   string
}

type VPS struct {
	ID       string
	Name     string
	Label    string
	IP       string
	IPv6     string
	Status   string
	Plan     string
	Location string
	Template string
}

// --- Kubernetes ---

type K8sCreateConfig struct {
	Name       string
	Label      string
	Location   string
	Version    string
	NodePlan   string
	NodeCount  int
	AutoScale  bool
	MinNodes   int
	MaxNodes   int
}

type K8sCluster struct {
	ID          string
	Name        string
	Label       string
	Status      string
	Location    string
	Version     string
	NodeCount   int
	Endpoint    string
	Kubeconfig  string
}

// --- Bare Metal ---

type BareMetalCreateConfig struct {
	Label      string
	Plan       string
	Location   string
	Template   string
	Hostname   string
	SSHKeyIDs  []string
	Password   string
	UserData   string
	EnableIPv4 bool
	EnableIPv6 bool
	Backups    bool
}

type BareMetal struct {
	ID       string
	Name     string
	Label    string
	IP       string
	Status   string
	Plan     string
	Location string
	Template string
}

// --- Load Balancer ---

type LBCreateConfig struct {
	Name       string
	Label      string
	Location   string
	Plan       string
	VPCID      string
	Algorithm  string // round_robin, least_connections, etc.
}

type LoadBalancer struct {
	ID        string
	Name      string
	Label     string
	Status    string
	IP        string
	Location  string
	Plan      string
}

// --- DNS ---

type DNSZone struct {
	ID    string
	Name  string
	DNSSec bool
}

type DNSRecord struct {
	ID      string
	Type    string // A, AAAA, CNAME, MX, TXT, NS, etc.
	Name    string
	Value   string
	TTL     int
	Priority int // for MX
}

// --- K8s Addons ---

type K8sAddon struct {
	ID        string
	Name      string
	Slug      string
	Version   string
	Status    string
	Installed bool
}

// --- K8s Node Pools ---

type K8sNodePoolConfig struct {
	Name      string
	Plan      string
	NodeCount int
}

type K8sNodePool struct {
	ID      string
	Name    string
	Plan    string
	Nodes   int
	Status  string
}

// --- LB Advanced ---

type LBListenerConfig struct {
	Port       int
	TargetPort int
	Protocol   string
}

type LBListener struct {
	ID          string
	Port        int
	TargetPort  int
	Protocol    string
	HealthCheck *LBHealthCheckConfig
}

type LBHealthCheckConfig struct {
	Protocol string
	Port     int
	Path     string
	Interval int
	Timeout  int
	Retries  int
}

type LBTargetConfig struct {
	Type     string // vps, ip, baremetal
	TargetID string
	Port     int
	Weight   int
}

type LBTarget struct {
	ID      string
	Type    string
	TargetID string
	Port    int
	Weight  int
	Status  string
}

// --- SSH Keys ---

type SSHKey struct {
	ID          string
	Name        string
	Fingerprint string
	PublicKey   string
	Created     string
}

type SSHKeyCreateConfig struct {
	Name      string
	PublicKey string
}
