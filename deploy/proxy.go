package deploy

import (
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type ProxyType string

const (
	ProxyCaddy   ProxyType = "caddy"
	ProxyTraefik ProxyType = "traefik"
	ProxyNginx   ProxyType = "nginx"
)

type ProxyConfig struct {
	Domain     string
	Email      string
	TargetPort int
	Staging    bool
}

type Proxy interface {
	Type() ProxyType
	Install(client *goss.Client, cfg ProxyConfig) error
	UpdateTargetPort(client *goss.Client, domain string, port int) error
	Status(client *goss.Client) (string, error)
	Remove(client *goss.Client) error
}

func NewProxy(p ProxyType) Proxy {
	switch p {
	case ProxyTraefik:
		return &TraefikProxy{}
	case ProxyNginx:
		return &NginxProxy{}
	default:
		return &CaddyProxy{}
	}
}

func DetectProxy(client *goss.Client) ProxyType {
	out, _, _ := ssh.Run(client, "command -v caddy && echo 'caddy' || command -v traefik && echo 'traefik' || command -v nginx && echo 'nginx' || echo 'none'")
	switch strings.TrimSpace(out) {
	case "caddy":
		return ProxyCaddy
	case "traefik":
		return ProxyTraefik
	case "nginx":
		return ProxyNginx
	default:
		return ""
	}
}
