package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

var certDomainRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

type certsFlags struct {
	node string
}

func newCertsCmd() *cobra.Command {
	var f infraFlags
	var cf certsFlags
	cmd := &cobra.Command{
		Use:   "certs",
		Short: "Issue Let's Encrypt certs via Traefik and sync them to services (pure Go)",
		Long: `Manage Let's Encrypt certificates with Traefik's own ACME resolver
(Traefik issues and renews, no provider keys) plus a pure-Go worker that syncs
the certificate from acme.json into each consuming service.

  issue    add the domain to Traefik (certResolver), deploy the sync worker,
           install the daily timer, and sync the first certificate
  status   timer state, last run and certificate expiry
  logs     tail the sync log
  run      run the sync once (oneshot)
  remove   uninstall the timer, worker and cert store
  import   install an existing private certificate into the store`,
	}
	cmd.PersistentFlags().StringVarP(&cf.node, "node", "n", "", "Target node IP (required)")
	cmd.PersistentFlags().StringVarP(&f.user, "user", "u", "sdkops", "SSH user")
	cmd.PersistentFlags().StringVarP(&f.key, "key", "k", "", "SSH private key path")
	cmd.PersistentFlags().IntVarP(&f.port, "port", "p", 22, "SSH port")

	cmd.AddCommand(newCertsIssueCmd(&f, &cf))
	cmd.AddCommand(newCertsImportCmd(&f, &cf))
	cmd.AddCommand(newCertsStatusCmd(&f, &cf))
	cmd.AddCommand(newCertsLogsCmd(&f, &cf))
	cmd.AddCommand(newCertsRunCmd(&f, &cf))
	cmd.AddCommand(newCertsRemoveCmd(&f, &cf))
	cmd.AddCommand(newCertsSyncCmd())
	return cmd
}

func certsConnect(cf *certsFlags, f infraFlags) (*golang_ssh.Client, error) {
	if cf.node == "" {
		return nil, fmt.Errorf("--node is required")
	}
	h := ProvisionHost{Name: cf.node, Host: cf.node, User: f.user, SSHKey: f.key, Port: f.port}
	return opsConnect(h, f)
}

// findRepoRoot walks up from the working directory to the sdk-ops module root.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found — run `certs issue` from inside the sdk-ops repository")
		}
		dir = parent
	}
}

// crossCompileWorker builds the sdk-ops binary for linux/amd64 (contains the
// hidden `certs sync` worker). The binary is written to /dev/stdout (constant
// args, no dynamic path in the exec) and captured in memory. Returns the temp
// binary path.
func crossCompileWorker() (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", "/dev/stdout", "./cmd/sdk-ops/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	bin, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cross-compile worker: %w", err)
	}
	tmp, err := os.CreateTemp("", "sdk-ops-worker-*.bin")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	_ = tmp.Close()
	return tmp.Name(), nil
}

// certResolverRouter renders the Traefik router that triggers the domain's
// Let's Encrypt certificate via the existing letsencrypt certResolver. The
// backend is Traefik's notfound service (the 404 page): the domain is only
// used for the certificate, NATS stays on its raw TLS port.
func certResolverRouter(domain string) string {
	name := "acme-" + strings.ReplaceAll(domain, ".", "-")
	return fmt.Sprintf(`http:
  routers:
    %s:
      rule: "Host(`+"`"+`%s`+"`"+`)"
      service: notfound
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
`, name, domain)
}

func newCertsIssueCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	var domain, services string
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Add a domain to Traefik (certResolver), deploy the sync worker and install the daily timer",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if !certDomainRe.MatchString(domain) {
				return fmt.Errorf("invalid --domain %q", domain)
			}
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)

			fmt.Println("  cross-compiling the sync worker (linux/amd64)...")
			binPath, err := crossCompileWorker()
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(binPath) }()

			binData, err := os.ReadFile(filepath.Clean(binPath))
			if err != nil {
				return fmt.Errorf("read worker binary: %w", err)
			}

			store := "/etc/sdk-ops/certs/" + domain
			cfg := certSyncConfig{
				Domain:   domain,
				Store:    store,
				ACMEJSON: "/opt/traefik/acme.json",
				Services: splitCertServices(services),
			}
			cfgData, _ := json.MarshalIndent(cfg, "", "  ")

			if out, _, err := ssh.RunWithStdin(conn,
				`sudo mkdir -p /opt/sdk-ops/certs && sudo tee /opt/sdk-ops/certs/sdk-ops > /dev/null && sudo chmod 0750 /opt/sdk-ops/certs/sdk-ops`,
				string(binData)); err != nil {
				return fmt.Errorf("upload worker: %w\n%s", err, out)
			}
			fmt.Printf("  worker uploaded (%d bytes)\n", len(binData))

			cfgRemote := "sudo mkdir -p " + store + "\n" +
				"sudo tee /etc/sdk-ops/certs/" + domain + ".json > /dev/null << 'CFGEOF'\n" +
				string(cfgData) + "\nCFGEOF\n" +
				"sudo chmod 600 /etc/sdk-ops/certs/" + domain + ".json\n"
			if out, _, err := ssh.Run(conn, cfgRemote); err != nil {
				return fmt.Errorf("write config: %w\n%s", err, out)
			}

			router := certResolverRouter(domain)
			installUnits := fmt.Sprintf(`
sudo tee /etc/traefik/conf.d/acme-%s.yml > /dev/null << 'ROUTEREOF'
%sROUTEREOF
sudo tee /etc/systemd/system/sdk-ops-certs.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops Let's Encrypt cert sync (Traefik acme.json)

[Service]
Type=oneshot
User=root
ExecStart=/opt/sdk-ops/certs/sdk-ops certs sync --all
SERVICEEOF
sudo tee /etc/systemd/system/sdk-ops-certs.timer > /dev/null << 'TIMEREOF'
[Unit]
Description=sdk-ops cert sync timer (daily)

[Timer]
OnCalendar=*-*-* 04:17:00
Persistent=true

[Install]
WantedBy=timers.target
TIMEREOF
sudo systemctl daemon-reload
sudo systemctl enable --now sdk-ops-certs.timer 2>/dev/null
echo "units + traefik router installed"
`, domain, router)
			if out, _, err := ssh.Run(conn, installUnits); err != nil {
				return fmt.Errorf("install units: %w\n%s", err, out)
			}

			fmt.Println("  waiting for Traefik to issue the certificate...")
			for range 12 {
				_, _, _ = ssh.Run(conn, `sudo systemctl start sdk-ops-certs.service 2>&1 || true`)
				has, _, _ := ssh.Run(conn, fmt.Sprintf(`test -f /etc/sdk-ops/certs/%s/cert.pem && echo yes || echo no`, domain))
				if strings.Contains(has, "yes") {
					break
				}
				time.Sleep(10 * time.Second)
			}
			out, _, _ := ssh.Run(conn, `tail -3 /var/log/sdk-ops-certs.log 2>/dev/null`)

			status, err := hardening.CertRenewStatus(conn, domain)
			if err != nil {
				return err
			}
			fmt.Printf("=== cert status (%s) ===\n%s\n", domain, status)
			if !strings.Contains(status, "notAfter") {
				fmt.Println("  (if no cert yet, Traefik is still issuing — the daily timer will sync it)")
			}
			_ = out
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Domain (Traefik certResolver, per-certificate)")
	cmd.Flags().StringVar(&services, "services", "nats", "Comma list of services to refresh: nats,traefik")
	_ = cmd.MarkFlagRequired("domain")
	return cmd
}

func newCertsImportCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	var domain, certFile, keyFile, services string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing certificate (private/own CA) into the store and refresh services",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if !certDomainRe.MatchString(domain) {
				return fmt.Errorf("invalid --domain %q", domain)
			}
			certData, err := os.ReadFile(filepath.Clean(certFile))
			if err != nil {
				return fmt.Errorf("read --cert-file: %w", err)
			}
			keyData, err := os.ReadFile(filepath.Clean(keyFile))
			if err != nil {
				return fmt.Errorf("read --key-file: %w", err)
			}
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)

			store := "/etc/sdk-ops/certs/" + domain
			remote := "sudo mkdir -p " + store + "\n" +
				"sudo tee " + store + "/fullchain.pem > /dev/null << 'CERTEOF'\n" +
				string(certData) + "\nCERTEOF\n" +
				"sudo tee " + store + "/privkey.pem > /dev/null << 'KEYEOF'\n" +
				string(keyData) + "\nKEYEOF\n" +
				"sudo cp " + store + "/fullchain.pem " + store + "/cert.pem\n" +
				"sudo chmod 600 " + store + "/privkey.pem\n" +
				refreshServicesBash(domain, splitCertServices(services))
			if out, _, err := ssh.Run(conn, remote); err != nil {
				return fmt.Errorf("import cert: %w\n%s", err, out)
			}
			fmt.Printf("  imported %s -> %s (no renewal timer: private certs are rotated manually)\n", domain, store)
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Domain (store dir /etc/sdk-ops/certs/<domain>)")
	cmd.Flags().StringVar(&certFile, "cert-file", "", "Certificate file (PEM, fullchain)")
	cmd.Flags().StringVar(&keyFile, "key-file", "", "Private key file (PEM)")
	cmd.Flags().StringVar(&services, "services", "nats", "Comma list of services to refresh: nats,traefik")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("cert-file")
	_ = cmd.MarkFlagRequired("key-file")
	return cmd
}

// refreshServicesBash copies the store cert into each consuming service and
// reloads it. Used by `certs import` (one-shot manual certs).
func refreshServicesBash(domain string, services []string) string {
	var b strings.Builder
	for _, s := range services {
		switch s {
		case "nats":
			b.WriteString("if [ -d /opt/sdk-ops/nats/certs ]; then\n")
			b.WriteString("  cp " + "/etc/sdk-ops/certs/" + domain + "/fullchain.pem /opt/sdk-ops/nats/certs/server.pem\n")
			b.WriteString("  cp " + "/etc/sdk-ops/certs/" + domain + "/privkey.pem /opt/sdk-ops/nats/certs/server.key\n")
			b.WriteString("  for c in $(sudo docker ps -q --filter name=nats 2>/dev/null); do sudo docker kill -s HUP \"$c\" 2>/dev/null || true; done\n")
			b.WriteString("fi\n")
		case "traefik":
			b.WriteString("sudo mkdir -p /opt/traefik/certs/" + domain + "\n")
			b.WriteString("sudo cp /etc/sdk-ops/certs/" + domain + "/fullchain.pem /opt/traefik/certs/" + domain + "/fullchain.pem\n")
			b.WriteString("sudo cp /etc/sdk-ops/certs/" + domain + "/privkey.pem /opt/traefik/certs/" + domain + "/privkey.pem\n")
			b.WriteString("sudo docker restart traefik 2>/dev/null || true\n")
		}
	}
	return b.String()
}

func splitCertServices(raw string) []string {
	var out []string
	for s := range strings.SplitSeq(raw, ",") {
		if t := strings.ToLower(strings.TrimSpace(s)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func newCertsStatusCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	var domain string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show timer state, last run and cert expiry",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)
			status, err := hardening.CertRenewStatus(conn, domain)
			if err != nil {
				return err
			}
			fmt.Printf("=== %s ===\n%s\n", cf.node, status)
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Domain to show cert expiry for (optional)")
	return cmd
}

func newCertsLogsCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail the sync log",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)
			out, _, err := ssh.Run(conn, fmt.Sprintf(`sudo tail -n %d /var/log/sdk-ops-certs.log 2>/dev/null || echo "no log"`, lines))
			if err != nil {
				return err
			}
			fmt.Print(strings.TrimRight(out, "\n"))
			if !strings.HasSuffix(out, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&lines, "lines", 10, "Number of lines")
	return cmd
}

func newCertsRunCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the sync once (oneshot)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)
			out, _, err := ssh.Run(conn, `sudo systemctl start sdk-ops-certs.service 2>&1; tail -5 /var/log/sdk-ops-certs.log 2>/dev/null`)
			if err != nil {
				return err
			}
			fmt.Print(strings.TrimRight(out, "\n"))
			if !strings.HasSuffix(out, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
}

func newCertsRemoveCmd(f *infraFlags, cf *certsFlags) *cobra.Command {
	var domain string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a domain's cert (config, Traefik router, store) or the whole cert stack",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			conn, err := certsConnect(cf, *f)
			if err != nil {
				return err
			}
			defer closeConn(conn)
			if domain != "" {
				remote := "sudo rm -f /etc/sdk-ops/certs/" + domain + ".json\n" +
					"sudo rm -f /etc/traefik/conf.d/acme-" + domain + ".yml\n" +
					"sudo rm -rf /etc/sdk-ops/certs/" + domain + "\n" +
					"echo 'domain removed'"
				if out, _, err := ssh.Run(conn, remote); err != nil {
					return fmt.Errorf("remove domain: %w\n%s", err, out)
				}
				fmt.Printf("  removed %s (timer + worker kept for the other domains)\n", domain)
				return nil
			}
			return hardening.RemoveCertRenew(conn)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Remove only this domain (config + router + store), keeping the timer/worker")
	return cmd
}
