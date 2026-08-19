package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"
)

// certSyncConfig is the per-domain sync config written by `certs issue` and
// consumed by `certs sync` on the node. The certificate is issued by Traefik's
// own ACME resolver (acme.json) — pure Go, no shell scripts, no provider keys.
type certSyncConfig struct {
	Domain   string   `json:"domain"`
	Store    string   `json:"store"`
	ACMEJSON string   `json:"acme_json"`
	Services []string `json:"services"`
}

const (
	certRenewLog  = "/var/log/sdk-ops-certs.log"
	certRenewPort = "8082"
)

func renewLogf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	log.Println(msg)
	if f, err := os.OpenFile(certRenewLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), msg)
		_ = f.Close()
	}
}

// acmeFile mirrors Traefik's acme.json: the certificate and key are base64 PEM.
type acmeFile struct {
	LetsEncrypt acmeResolver `json:"letsencrypt"`
}

type acmeResolver struct {
	Certificates []acmeCert `json:"Certificates"`
}

type acmeCert struct {
	Domain      acmeDomain `json:"domain"`
	Certificate string     `json:"certificate"`
	Key         string     `json:"key"`
}

type acmeDomain struct {
	Main string `json:"main"`
}

// extractTraefikCert returns the fullchain and private key PEM for a domain
// from Traefik's acme.json.
func extractTraefikCert(acmeJSON, domain string) ([]byte, []byte, bool, error) {
	data, err := os.ReadFile(filepath.Clean(acmeJSON))
	if err != nil {
		return nil, nil, false, err
	}
	var af acmeFile
	if err := json.Unmarshal(data, &af); err != nil {
		return nil, nil, false, fmt.Errorf("parse acme.json: %w", err)
	}
	for _, c := range af.LetsEncrypt.Certificates {
		if c.Domain.Main != domain {
			continue
		}
		cert, err := base64.StdEncoding.DecodeString(c.Certificate)
		if err != nil {
			return nil, nil, false, fmt.Errorf("decode cert: %w", err)
		}
		key, err := base64.StdEncoding.DecodeString(c.Key)
		if err != nil {
			return nil, nil, false, fmt.Errorf("decode key: %w", err)
		}
		return cert, key, true, nil
	}
	return nil, nil, false, nil
}

// syncCert copies Traefik's issued certificate for the domain into the store
// and refreshes the consuming services (idempotent: skips when unchanged).
func syncCert(cfg certSyncConfig) error {
	if !certDomainRe.MatchString(cfg.Domain) {
		return fmt.Errorf("invalid domain %q in config", cfg.Domain)
	}
	// Directory and fullchain/cert are public certificate material that the
	// health/backup scripts (running as the non-root sdkops user) must read for
	// --tlsca and expiry checks. The privkey below stays 0600 (the real secret).
	if err := os.MkdirAll(filepath.Clean(cfg.Store), 0o755); err != nil { //nolint:gosec // public cert store, readable by monitoring user
		return err
	}
	fullchain, key, ok, err := extractTraefikCert(cfg.ACMEJSON, cfg.Domain)
	if err != nil {
		return err
	}
	if !ok {
		renewLogf("no cert for %s in acme.json yet (Traefik still issuing?)", cfg.Domain)
		return nil
	}
	cur, _ := os.ReadFile(filepath.Join(cfg.Store, "fullchain.pem"))
	if bytes.Equal(cur, fullchain) {
		renewLogf("cert unchanged for %s, skip", cfg.Domain)
		return nil
	}
	//nolint:gosec // fullchain/cert are public cert data (see MkdirAll above)
	_ = os.WriteFile(filepath.Join(cfg.Store, "fullchain.pem"), fullchain, 0o644)
	_ = os.WriteFile(filepath.Join(cfg.Store, "cert.pem"), fullchain, 0o644) //nolint:gosec // public cert data
	_ = os.WriteFile(filepath.Join(cfg.Store, "privkey.pem"), key, 0o600)
	renewLogf("cert synced for %s", cfg.Domain)
	refreshServices(cfg.Store, cfg.Domain, cfg.Services)
	return nil
}

// containerIDRe matches docker container IDs (from `docker ps -q`): the value
// is a hex identifier, never a free-form string.
var containerIDRe = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

// refreshServices copies the fresh cert into each consuming service and
// reloads it (NATS via HUP, Traefik via restart).
func refreshServices(store, domain string, services []string) {
	if !certDomainRe.MatchString(domain) {
		renewLogf("refresh skipped: invalid domain %q", domain)
		return
	}
	for _, s := range services {
		switch s {
		case "nats":
			if st, err := os.Stat("/opt/sdk-ops/nats/certs"); err == nil && st.IsDir() {
				copyPEM("/opt/sdk-ops/nats/certs/server.pem", filepath.Join(store, "fullchain.pem"), 0o600)
				copyPEM("/opt/sdk-ops/nats/certs/server.key", filepath.Join(store, "privkey.pem"), 0o600)
				if out, err := exec.CommandContext(context.Background(), "docker", "ps", "-q", "--filter", "name=nats").Output(); err == nil {
					for _, c := range splitLines(out) {
						if containerIDRe.MatchString(c) {
							_ = exec.CommandContext(context.Background(), "docker", "kill", "-s", "HUP", c).Run() //nolint:gosec // G204: c is docker ps output, strictly validated by containerIDRe hex 12-64, not user input
						}
					}
				}
				renewLogf("nats cert refreshed + HUP")
			}
		case "traefik":
			dir := "/opt/traefik/certs/" + domain
			_ = os.MkdirAll(dir, 0o750)
			copyPEM(filepath.Join(dir, "fullchain.pem"), filepath.Join(store, "fullchain.pem"), 0o644)
			copyPEM(filepath.Join(dir, "privkey.pem"), filepath.Join(store, "privkey.pem"), 0o600)
			_ = exec.CommandContext(context.Background(), "docker", "restart", "traefik").Run()
			renewLogf("traefik cert refreshed + restart")
		}
	}
}

func copyPEM(dst, src string, mode os.FileMode) {
	root, err := os.OpenRoot(filepath.Dir(dst))
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	data, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return
	}
	out, err := root.OpenFile(filepath.Base(dst), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return
	}
	defer func() { _ = out.Close() }()
	_, _ = out.Write(data)
}

func splitLines(b []byte) []string {
	var out []string
	cur := ""
	for _, c := range b {
		if c == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func loadSyncConfig(path string) (certSyncConfig, error) {
	var cfg certSyncConfig
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func newCertsSyncCmd() *cobra.Command {
	var configPath string
	var all bool
	cmd := &cobra.Command{
		Use:    "sync",
		Hidden: true,
		Short:  "Sync Traefik-issued certs from acme.json into the services (run on the node by the timer)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if all {
				files, err := filepath.Glob("/etc/sdk-ops/certs/*.json")
				if err != nil {
					return err
				}
				for _, f := range files {
					cfg, cerr := loadSyncConfig(f)
					if cerr != nil {
						renewLogf("sync %s: %v", f, cerr)
						continue
					}
					if cerr := syncCert(cfg); cerr != nil {
						renewLogf("sync %s FAILED: %v", f, cerr)
					}
				}
				return nil
			}
			if configPath == "" {
				return fmt.Errorf("--config or --all is required")
			}
			cfg, err := loadSyncConfig(configPath)
			if err != nil {
				return err
			}
			if err := syncCert(cfg); err != nil {
				renewLogf("sync FAILED for %s: %v", cfg.Domain, err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Sync config JSON (written by `certs issue`)")
	cmd.Flags().BoolVar(&all, "all", false, "Sync every /etc/sdk-ops/certs/*.json config")
	return cmd
}
