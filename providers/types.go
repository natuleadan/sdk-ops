package providers

// --- VPS ---

type VPSCreateConfig struct {
	Label      string
	Plan       string
	Location   string
	Template   string
	Hostname   string
	SSHKeyIDs  []int
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
	SSHKeyIDs  []int
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
