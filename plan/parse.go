package plan

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func ParseFile(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan file: %w", err)
	}

	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}

	p.fillDefaults()
	return &p, nil
}

func (p *Plan) Validate() error {
	if len(p.Hosts) == 0 {
		return fmt.Errorf("plan must have at least one host")
	}

	servers := 0
	for i, h := range p.Hosts {
		if h.Name == "" {
			return fmt.Errorf("host %d: name is required", i)
		}
		if h.Role != "server" && h.Role != "agent" {
			return fmt.Errorf("host %q: role must be 'server' or 'agent', got %q", h.Name, h.Role)
		}
		if h.Host == "" {
			return fmt.Errorf("host %q: host (IP) is required", h.Name)
		}
		if h.Role == "server" {
			servers++
		}
	}

	if servers == 0 {
		return fmt.Errorf("plan must have at least one server")
	}
	return nil
}

func (p *Plan) fillDefaults() {
	if p.Mode == "" {
		p.Mode = "k3s"
	}
	if p.Parallel <= 0 {
		p.Parallel = 5
	}

	fillOptDefaults := func(o *Options) {
		if o.User == "" {
			o.User = "root"
		}
		if o.SSHPort <= 0 {
			o.SSHPort = 22
		}
		if o.K3sChannel == "" {
			o.K3sChannel = "stable"
		}
	}
	fillOptDefaults(&p.ServerOptions)
	fillOptDefaults(&p.AgentOptions)

	for i, h := range p.Hosts {
		if h.Port <= 0 {
			p.Hosts[i].Port = p.ServerOptions.SSHPort
			if h.Role == "agent" {
				p.Hosts[i].Port = p.AgentOptions.SSHPort
			}
		}
		if h.User == "" {
			p.Hosts[i].User = p.ServerOptions.User
			if h.Role == "agent" {
				p.Hosts[i].User = p.AgentOptions.User
			}
		}
		if h.SSHKey == "" {
			p.Hosts[i].SSHKey = p.ServerOptions.SSHKey
			if h.Role == "agent" {
				p.Hosts[i].SSHKey = p.AgentOptions.SSHKey
			}
		}
	}
}

func (p *Plan) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Mode:      %s\n", p.Mode))
	b.WriteString(fmt.Sprintf("Parallel:  %d\n", p.Parallel))
	b.WriteString("Servers:\n")
	for _, h := range p.Hosts {
		if h.Role == "server" {
			b.WriteString(fmt.Sprintf("  - %s (%s)\n", h.Name, h.Host))
		}
	}
	b.WriteString("Agents:\n")
	for _, h := range p.Hosts {
		if h.Role == "agent" {
			b.WriteString(fmt.Sprintf("  - %s (%s)\n", h.Name, h.Host))
		}
	}
	return b.String()
}
