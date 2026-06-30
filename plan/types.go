package plan

type Options struct {
	User         string `yaml:"user" json:"user"`
	SSHKey       string `yaml:"ssh_key" json:"ssh_key"`
	SSHPort      int    `yaml:"ssh_port" json:"ssh_port"`
	K3sExtraArgs string `yaml:"k3s_extra_args" json:"k3s_extra_args"`
	K3sChannel   string `yaml:"k3s_channel" json:"k3s_channel"`
	K3sVersion   string `yaml:"k3s_version" json:"k3s_version"`
	DisableTraefik bool `yaml:"disable_traefik" json:"disable_traefik"`
}

type Host struct {
	Name     string `yaml:"name" json:"name"`
	Role     string `yaml:"role" json:"role"`
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
	User     string `yaml:"user,omitempty" json:"user,omitempty"`
	SSHKey   string `yaml:"ssh_key,omitempty" json:"ssh_key,omitempty"`
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
}

type Plan struct {
	Mode          string  `yaml:"mode" json:"mode"`
	Parallel      int     `yaml:"parallel" json:"parallel"`
	ServerOptions Options `yaml:"server_options" json:"server_options"`
	AgentOptions  Options `yaml:"agent_options" json:"agent_options"`
	Hosts         []Host  `yaml:"hosts" json:"hosts"`
}
