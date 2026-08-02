package hardening

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// AllowlistProfile selects how restrictive the IP allowlist is.
type AllowlistProfile int

const (
	// AllowlistNormal gates every inbound port except SSH.
	AllowlistNormal AllowlistProfile = iota
	// AllowlistStrict gates every inbound port, including SSH.
	AllowlistStrict
)

// Ports that are always gated by the allowlist regardless of profile.
var allowlistGatePorts = map[int]bool{80: true, 443: true, 6443: true}

// Source describes where provider IP ranges come from.
type Source struct {
	Kind  string // cf, url, dns
	Value string
}

// DefaultSource returns the Cloudflare source.
func DefaultSource() Source { return Source{Kind: "cf"} }

// ParseSource parses a --source flag value: cf, url:<url>, dns:<fqdn>.
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{}, fmt.Errorf("source is empty")
	}
	switch {
	case raw == "cf" || raw == "cloudflare":
		return DefaultSource(), nil
	case strings.HasPrefix(raw, "url:"):
		u := strings.TrimPrefix(raw, "url:")
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return Source{}, fmt.Errorf("url source must be http(s): %q", raw)
		}
		return Source{Kind: "url", Value: u}, nil
	case strings.HasPrefix(raw, "dns:"):
		fqdn := strings.TrimPrefix(raw, "dns:")
		if fqdn == "" || strings.ContainsAny(fqdn, " \t/") {
			return Source{}, fmt.Errorf("dns source must be a domain name: %q", raw)
		}
		return Source{Kind: "dns", Value: fqdn}, nil
	default:
		return Source{}, fmt.Errorf("unsupported source %q (use cf, url:<url>, dns:<fqdn>)", raw)
	}
}

// String renders the source as a single line for the remote sources file.
func (s Source) String() string {
	switch s.Kind {
	case "url":
		return "url:" + s.Value
	case "dns":
		return "dns:" + s.Value
	default:
		return "cf"
	}
}

// ValidateCIDR reports whether s is a valid IPv4 or IPv6 CIDR (or bare IP).
func ValidateCIDR(s string) (isV6 bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, fmt.Errorf("empty CIDR")
	}
	if strings.Contains(s, ":") {
		if _, _, err := net.ParseCIDR(s); err == nil {
			return true, nil
		}
		if ip := net.ParseIP(s); ip != nil && ip.To4() == nil {
			return true, nil
		}
		return false, fmt.Errorf("invalid IPv6 CIDR %q", s)
	}
	if ip, _, err := net.ParseCIDR(s); err == nil && ip != nil {
		return false, nil
	}
	if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
		return false, nil
	}
	return false, fmt.Errorf("invalid IPv4 CIDR %q", s)
}

// NormalizeCIDR ensures an address is stored as an explicit prefix. Bare IPs
// in nftables interval sets can be stored as single addresses that fail to
// match, so every element is normalized to /32 (IPv4) or /128 (IPv6).
func NormalizeCIDR(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return s
	}
	if strings.Contains(s, ":") {
		return s + "/128"
	}
	return s + "/32"
}

// ParseCIDRList validates raw CIDR lines and splits them into IPv4 and IPv6 lists.
func ParseCIDRList(raw string) (v4, v6 []string, err error) {
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		isV6, err := ValidateCIDR(line)
		if err != nil {
			return nil, nil, err
		}
		if isV6 {
			v6 = append(v6, line)
		} else {
			v4 = append(v4, line)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	return v4, v6, nil
}

// resolveTXT resolves TXT records for fqdn and expands include: chains
// (Google cloud-netblocks style), with recursion and cycle guards.
func resolveTXT(ctx context.Context, fqdn string, depth int, visited map[string]bool) ([]string, error) {
	if depth > 10 {
		return nil, fmt.Errorf("dns include chain too deep at %q", fqdn)
	}
	if visited[fqdn] {
		return nil, nil
	}
	visited[fqdn] = true
	records, err := net.DefaultResolver.LookupTXT(ctx, fqdn)
	if err != nil {
		return nil, fmt.Errorf("lookup TXT %s: %w", fqdn, err)
	}
	var out []string
	for _, rec := range records {
		for _, tok := range strings.Fields(rec) {
			tok = strings.Trim(tok, `"`)
			if strings.HasPrefix(tok, "include:") {
				sub, err := resolveTXT(ctx, strings.TrimPrefix(tok, "include:"), depth+1, visited)
				if err != nil {
					return nil, err
				}
				out = append(out, sub...)
				continue
			}
			if _, err := ValidateCIDR(tok); err == nil {
				out = append(out, tok)
			}
		}
	}
	return out, nil
}

// FetchCIDRs fetches and validates the allowlist for a source.
func FetchCIDRs(ctx context.Context, src Source) (v4, v6 []string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var raw string
	switch src.Kind {
	case "cf":
		raw = fetchURL(ctx, client, "https://www.cloudflare.com/ips-v4")
		raw += "\n" + fetchURL(ctx, client, "https://www.cloudflare.com/ips-v6")
		if strings.TrimSpace(raw) == "" {
			return nil, nil, fmt.Errorf("cloudflare ips fetch failed")
		}
	case "url":
		raw = fetchURL(ctx, client, src.Value)
		if strings.TrimSpace(raw) == "" {
			return nil, nil, fmt.Errorf("fetch %s failed", src.Value)
		}
	case "dns":
		records, err := resolveTXT(ctx, src.Value, 0, map[string]bool{})
		if err != nil {
			return nil, nil, err
		}
		raw = strings.Join(records, "\n")
	default:
		return nil, nil, fmt.Errorf("unsupported source kind %q", src.Kind)
	}
	v4, v6, err = ParseCIDRList(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse source %s: %w", src.Kind, err)
	}
	return v4, v6, nil
}

func fetchURL(ctx context.Context, client *http.Client, url string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	return string(body)
}

// AllowlistConfig holds everything needed to render the allowlist nftables config.
type AllowlistConfig struct {
	Profile  AllowlistProfile
	SSHPorts []int
	Admin4   []string
	Admin6   []string
	Source   Source
}

// GenerateAllowlistConfig renders a full nftables config gated by allow4/allow6
// sets, with permanent admin4/admin6 sets for operator bootstrap access.
// The config defines only the inet filter table (no flush ruleset), so Docker
// or other iptables-nft tables are never touched when it is loaded.
//
// Sets use `flags interval` because they hold prefix elements (e.g.
// 173.245.48.0/20 for Cloudflare ranges); plain sets reject prefixes.
func GenerateAllowlistConfig(cfg AllowlistConfig) string {
	var b strings.Builder
	b.WriteString("table inet filter {\n")
	b.WriteString("    set allow4 { type ipv4_addr; flags interval; }\n")
	b.WriteString("    set allow6 { type ipv6_addr; flags interval; }\n")
	writeSetWithElements(&b, "admin4", cfg.Admin4)
	writeSetWithElements(&b, "admin6", cfg.Admin6)
	b.WriteString("    chain input {\n")
	b.WriteString("        type filter hook input priority 0; policy drop;\n")
	b.WriteString("        iif lo accept\n")
	b.WriteString("        ct state established,related accept\n")
	b.WriteString("        jump exposed\n")
	b.WriteString("        ip protocol icmp accept\n")
	b.WriteString("        ip6 nexthdr icmpv6 accept\n")
	if cfg.Profile == AllowlistNormal {
		ports := normalizePorts(cfg.SSHPorts)
		if len(ports) == 0 {
			ports = []int{22}
		}
		fmt.Fprintf(&b, "        tcp dport { %s } accept\n", joinPorts(ports))
	}
	b.WriteString("        ip saddr @admin4 accept\n")
	b.WriteString("        ip6 saddr @admin6 accept\n")
	b.WriteString("        ip saddr @allow4 accept\n")
	b.WriteString("        ip6 saddr @allow6 accept\n")
	b.WriteString("    }\n")
	b.WriteString("    chain exposed { }\n")
	b.WriteString("    chain forward {\n")
	b.WriteString("        type filter hook forward priority 0; policy drop;\n")
	b.WriteString("        ct state established,related accept\n")
	b.WriteString("        jump exposed\n")
	b.WriteString("        ip saddr 10.0.0.0/8 accept\n")
	b.WriteString("        ip saddr 172.16.0.0/12 accept\n")
	b.WriteString("        ip saddr 192.168.0.0/16 accept\n")
	b.WriteString("        ip saddr @admin4 accept\n")
	b.WriteString("        ip6 saddr @admin6 accept\n")
	b.WriteString("        ip saddr @allow4 accept\n")
	b.WriteString("        ip6 saddr @allow6 accept\n")
	b.WriteString("    }\n")
	b.WriteString("    chain output { type filter hook output priority 0; policy accept; }\n")
	b.WriteString("}\n")
	return b.String()
}

func writeSetWithElements(b *strings.Builder, name string, elements []string) {
	if len(elements) == 0 {
		fmt.Fprintf(b, "    set %s { type %s; flags interval; }\n", name, setType(name))
		return
	}
	norm := make([]string, 0, len(elements))
	for _, e := range elements {
		norm = append(norm, NormalizeCIDR(e))
	}
	sort.Strings(norm)
	fmt.Fprintf(b, "    set %s { type %s; flags interval; elements = { %s } }\n",
		name, setType(name), strings.Join(norm, ", "))
}

func setType(name string) string {
	if strings.HasSuffix(name, "6") {
		return "ipv6_addr"
	}
	return "ipv4_addr"
}

func normalizePorts(ports []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range ports {
		if p > 0 && p <= 65535 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}

func joinPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(parts, ", ")
}

// CurrentSSHPorts inspects the live input chain and returns the ports that are
// currently open via the hardening dport rule, excluding gated ports.
func CurrentSSHPorts(client *goss.Client, fallbackPort int) []int {
	out, _, err := ssh.Run(client, `sudo nft list chain inet filter input 2>/dev/null | grep 'dport' || true`)
	if err != nil {
		return []int{fallbackPort}
	}
	ports := map[int]bool{fallbackPort: true}
	for _, seg := range strings.Fields(out) {
		p := 0
		if _, err := fmt.Sscanf(strings.Trim(seg, "{},/;"), "%d", &p); err == nil && p >= 1 && p <= 65535 && !allowlistGatePorts[p] {
			ports[p] = true
		}
	}
	var result []int
	for p := range ports {
		result = append(result, p)
	}
	return normalizePorts(result)
}
