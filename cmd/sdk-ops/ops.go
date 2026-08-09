package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

// opsComponent describes one managed cron/watchdog: where its script lives
// (for content diffing), which systemd units own it, its log, and how to
// install/remove it.
type opsComponent struct {
	name         string
	scriptPath   string
	template     func() string
	units        []string // timer + service, stopped before an update
	logFile      string
	timerName    string
	serviceName  string
	requiresYAML bool
	install      func(conn *golang_ssh.Client, pf *ProvisionFile, h ProvisionHost) error
	remove       func(conn *golang_ssh.Client) error
}

func opsComponents() map[string]opsComponent {
	return map[string]opsComponent{
		"allowlist": {
			name:         "allowlist",
			scriptPath:   "/opt/sdk-ops/firewall/allowlist.sh",
			template:     hardening.AllowlistUpdaterScript,
			units:        []string{"sdk-ops-allowlist.service", "sdk-ops-allowlist.timer"},
			logFile:      "/var/log/sdk-ops-allowlist.log",
			timerName:    "sdk-ops-allowlist.timer",
			serviceName:  "sdk-ops-allowlist.service",
			requiresYAML: true, // admin IPs come from the fleet YAML
			install: func(_ *golang_ssh.Client, pf *ProvisionFile, h ProvisionHost) error {
				return installAllowlistOn(pf, h)
			},
			remove: hardening.RemoveAllowlist,
		},
		"security": {
			name:        "security",
			scriptPath:  "/opt/sdk-ops/security/watch.sh",
			template:    hardening.SecurityWatchScript,
			units:       []string{"sdk-ops-security.service", "sdk-ops-security.timer"},
			logFile:     "/var/log/sdk-ops-security.log",
			timerName:   "sdk-ops-security.timer",
			serviceName: "sdk-ops-security.service",
			install: func(conn *golang_ssh.Client, pf *ProvisionFile, h ProvisionHost) error {
				r := resolveHostConfig(pf, h)
				return hardening.InstallSecurityWatch(conn, hardening.SecurityWatchConfig{
					Enabled:   r.security.Enabled,
					Threshold: r.security.Threshold,
				})
			},
			remove: hardening.RemoveSecurityWatch,
		},
		"state": {
			name:        "state",
			scriptPath:  "/opt/sdk-ops/firewall/state_watch.sh",
			template:    hardening.StateWatchScript,
			units:       []string{"sdk-ops-state.service", "sdk-ops-state.timer"},
			logFile:     "/var/log/sdk-ops-state.log",
			timerName:   "sdk-ops-state.timer",
			serviceName: "sdk-ops-state.service",
			install: func(conn *golang_ssh.Client, _ *ProvisionFile, _ ProvisionHost) error {
				return hardening.InstallStateWatch(conn)
			},
			remove: hardening.RemoveStateWatch,
		},
		"traefik": {
			name:        "traefik",
			scriptPath:  "/opt/sdk-ops/traefik/watch.sh",
			template:    hardening.TraefikWatchScript,
			units:       []string{"sdk-ops-traefik.service", "sdk-ops-traefik.timer"},
			logFile:     "/var/log/sdk-ops-traefik.log",
			timerName:   "sdk-ops-traefik.timer",
			serviceName: "sdk-ops-traefik.service",
			install: func(conn *golang_ssh.Client, _ *ProvisionFile, _ ProvisionHost) error {
				return hardening.InstallTraefikWatch(conn)
			},
			remove: hardening.RemoveTraefikWatch,
		},
		"logrotate": {
			name:       "logrotate",
			scriptPath: "/etc/logrotate.d/sdk-ops",
			template:   hardening.LogRotationConfig,
			logFile:    "/etc/logrotate.d/sdk-ops",
			install: func(conn *golang_ssh.Client, _ *ProvisionFile, _ ProvisionHost) error {
				return hardening.InstallLogRotation(conn)
			},
			remove: hardening.RemoveLogRotation,
		},
	}
}

// scriptNeedsUpdate reports whether the installed script differs from the
// current template (plain line comparison, ignoring trailing newlines).
func scriptNeedsUpdate(remote, template string) bool {
	return strings.TrimRight(remote, "\r\n") != strings.TrimRight(template, "\r\n")
}

// parseOpsComponents validates a --components list against the known set.
func parseOpsComponents(raw string) ([]string, error) {
	known := opsComponents()
	if strings.TrimSpace(raw) == "" {
		out := make([]string, 0, len(known))
		for name := range known {
			out = append(out, name)
		}
		return out, nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		name := strings.TrimSpace(part)
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("unknown component %q (allowlist, security, state, traefik, logrotate)", name)
		}
		out = append(out, name)
	}
	return out, nil
}

// opsTargets resolves the nodes to operate on: the whole fleet from the
// provision YAML, filtered by --node, or a single --node without a YAML.
func opsTargets(pf *ProvisionFile, node string, f infraFlags) ([]ProvisionHost, *ProvisionFile, error) {
	if pf != nil {
		if node == "" {
			return pf.Hosts, pf, nil
		}
		for _, h := range pf.Hosts {
			if h.Host == node {
				return []ProvisionHost{h}, pf, nil
			}
		}
		return nil, nil, fmt.Errorf("node %s not found in the fleet YAML", node)
	}
	if node == "" {
		return nil, nil, fmt.Errorf("need --provision-yaml or --node")
	}
	h := ProvisionHost{Name: node, Host: node, User: f.user, SSHKey: f.key, Port: f.port}
	return []ProvisionHost{h}, nil, nil
}

// opsFlags holds the shared flags of the ops command.
type opsFlags struct {
	provisionYAML string
	node          string
}

func addOpsFlags(cmd *cobra.Command, of *opsFlags) {
	cmd.PersistentFlags().StringVar(&of.provisionYAML, "provision-yaml", "", "Fleet YAML (nodes + per-host config). Required for the allowlist component")
	cmd.PersistentFlags().StringVarP(&of.node, "node", "n", "", "Target node IP (filters the fleet or operates a single node)")
}

func opsConnect(h ProvisionHost, f infraFlags) (*golang_ssh.Client, error) {
	ff := f
	if h.User != "" {
		ff.user = h.User
	}
	if h.SSHKey != "" {
		ff.key = h.SSHKey
	}
	if h.Port != 0 {
		ff.port = h.Port
	}
	conn, err := infraConnect(h.Host, &ff)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func newOpsCmd() *cobra.Command {
	var f infraFlags
	var of opsFlags
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Manage the node cron/watchdog stack (allowlist, security, state, traefik, logrotate)",
		Long: `Manage the sdk-ops cron stack on one or many nodes.

  apply    install/update the cron scripts (content diff: identical scripts
           are skipped; changed scripts are stopped, rewritten and restarted)
  status   timers, last runs and last log lines
  logs     tail a cron log
  run      execute a cron once (oneshot)
  enable   enable+start a cron timer
  disable  stop+disable a cron timer
  remove   uninstall a cron

The fleet comes from --provision-yaml (per-host config inherited); --node
filters the fleet or targets a single node.`,
	}
	addOpsFlags(cmd, &of)
	cmd.PersistentFlags().StringVarP(&f.user, "user", "u", "sdkops", "SSH user")
	cmd.PersistentFlags().StringVarP(&f.key, "key", "k", "", "SSH private key path")
	cmd.PersistentFlags().IntVarP(&f.port, "port", "p", 22, "SSH port")

	cmd.AddCommand(newOpsApplyCmd(&f, &of))
	cmd.AddCommand(newOpsStatusCmd(&f, &of))
	cmd.AddCommand(newOpsLogsCmd(&f, &of))
	cmd.AddCommand(newOpsRunCmd(&f, &of))
	cmd.AddCommand(newOpsEnableCmd(&f, &of, true))
	cmd.AddCommand(newOpsEnableCmd(&f, &of, false))
	cmd.AddCommand(newOpsRemoveCmd(&f, &of))
	return cmd
}

func loadOpsProvision(yamlPath string) (*ProvisionFile, error) {
	if yamlPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Clean(yamlPath))
	if err != nil {
		return nil, fmt.Errorf("ops --provision-yaml: %w", err)
	}
	var pf ProvisionFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("ops --provision-yaml: %w", err)
	}
	if _, err := validateProvision(&pf); err != nil {
		return nil, fmt.Errorf("ops --provision-yaml: %w", err)
	}
	return &pf, nil
}

func newOpsApplyCmd(f *infraFlags, of *opsFlags) *cobra.Command {
	var components string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Install or update the cron scripts (skip when identical)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			want, err := parseOpsComponents(components)
			if err != nil {
				return err
			}
			hosts, pf, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			table := opsComponents()
			for _, h := range hosts {
				fmt.Printf("\n━━━ %s (%s) ━━━\n", h.Name, h.Host)
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("  ✗ connect: %v\n", err)
					continue
				}
				for _, name := range want {
					c := table[name]
					if c.requiresYAML && pf == nil {
						fmt.Printf("  %-9s ✗ requires --provision-yaml (admin IPs come from the fleet)\n", name)
						continue
					}
					remote, _, _ := ssh.Run(conn, `cat `+c.scriptPath+` 2>/dev/null || true`)
					if !scriptNeedsUpdate(remote, c.template()) {
						fmt.Printf("  %-9s up to date\n", name)
						continue
					}
					// Stop the units first so the running script is never
					// rewritten mid-execution, then install (which
					// re-enables and starts them).
					if len(c.units) > 0 {
						_, _, _ = ssh.Run(conn, `sudo systemctl stop `+strings.Join(c.units, " ")+` 2>/dev/null || true`)
					}
					if err := c.install(conn, pf, h); err != nil {
						fmt.Printf("  %-9s ✗ %v\n", name, err)
						continue
					}
					fmt.Printf("  %-9s updated\n", name)
				}
				closeConn(conn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&components, "components", "", "Comma-separated components (default: all): allowlist,security,state,traefik,logrotate")
	return cmd
}

func newOpsStatusCmd(f *infraFlags, of *opsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show timers, last runs and last log lines",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			hosts, _, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			table := opsComponents()
			for _, h := range hosts {
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("%s: connect: %v\n", h.Host, err)
					continue
				}
				fmt.Printf("\n=== %s (%s) ===\n", h.Name, h.Host)
				for _, name := range sortedOpsComponents() {
					c := table[name]
					if c.timerName == "" {
						out, _, _ := ssh.Run(conn, `stat -c '%y' `+c.scriptPath+` 2>/dev/null || echo missing`)
						fmt.Printf("  %-9s %s\n", name, strings.TrimSpace(out))
						continue
					}
					out, _, _ := ssh.Run(conn, `ACT=$(systemctl is-active `+c.timerName+` 2>/dev/null || echo inactive)
LAST=$(systemctl show -p LastTriggerUSec --value `+c.timerName+` 2>/dev/null || echo never)
LOG=$(tail -1 `+c.logFile+` 2>/dev/null || echo no log)
echo "timer=$ACT last=$LAST"
echo "log: $LOG"`)
					for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
						fmt.Printf("  %-9s %s\n", name, line)
					}
				}
				closeConn(conn)
			}
			return nil
		},
	}
}

func sortedOpsComponents() []string {
	known := opsComponents()
	out := make([]string, 0, len(known))
	for name := range known {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func newOpsLogsCmd(f *infraFlags, of *opsFlags) *cobra.Command {
	var component string
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail a cron log",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			c, ok := opsComponents()[component]
			if !ok {
				return fmt.Errorf("unknown component %q (allowlist, security, state, traefik, logrotate)", component)
			}
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			hosts, _, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			for _, h := range hosts {
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("%s: connect: %v\n", h.Host, err)
					continue
				}
				out, _, _ := ssh.Run(conn, fmt.Sprintf(`sudo tail -n %d %s 2>/dev/null || echo "no log"`, lines, c.logFile))
				fmt.Printf("=== %s (%s) %s ===\n%s", h.Name, h.Host, component, strings.TrimRight(out, "\n"))
				if !strings.HasSuffix(out, "\n") {
					fmt.Println()
				}
				closeConn(conn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&component, "component", "", "Component (allowlist, security, state, traefik, logrotate)")
	cmd.Flags().IntVar(&lines, "lines", 10, "Number of lines")
	_ = cmd.MarkFlagRequired("component")
	return cmd
}

func newOpsRunCmd(f *infraFlags, of *opsFlags) *cobra.Command {
	var component string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a cron once (oneshot)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			c, ok := opsComponents()[component]
			if !ok {
				return fmt.Errorf("unknown component %q", component)
			}
			if c.serviceName == "" {
				return fmt.Errorf("component %q has no service to run", component)
			}
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			hosts, _, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			for _, h := range hosts {
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("%s: connect: %v\n", h.Host, err)
					continue
				}
				out, _, _ := ssh.Run(conn, `sudo systemctl start `+c.serviceName+` 2>&1; sudo systemctl is-active `+c.serviceName+` 2>/dev/null || true`)
				fmt.Printf("%s %s: %s\n", h.Host, component, strings.TrimSpace(out))
				closeConn(conn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&component, "component", "", "Component (allowlist, security, state, traefik)")
	_ = cmd.MarkFlagRequired("component")
	return cmd
}

func newOpsEnableCmd(f *infraFlags, of *opsFlags, enable bool) *cobra.Command {
	var component string
	action := "disable"
	verb := "Disabled"
	if enable {
		action = "enable"
		verb = "Enabled"
	}
	cmd := &cobra.Command{
		Use:   action,
		Short: verb + " a cron timer",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			c, ok := opsComponents()[component]
			if !ok {
				return fmt.Errorf("unknown component %q", component)
			}
			if c.timerName == "" {
				return fmt.Errorf("component %q has no timer", component)
			}
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			hosts, _, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			for _, h := range hosts {
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("%s: connect: %v\n", h.Host, err)
					continue
				}
				out, _, _ := ssh.Run(conn, `sudo systemctl `+action+` --now `+c.timerName+` 2>&1; systemctl is-active `+c.timerName+` 2>/dev/null || true`)
				fmt.Printf("%s %s: %s\n", h.Host, component, strings.TrimSpace(out))
				closeConn(conn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&component, "component", "", "Component (allowlist, security, state, traefik)")
	_ = cmd.MarkFlagRequired("component")
	return cmd
}

func newOpsRemoveCmd(f *infraFlags, of *opsFlags) *cobra.Command {
	var component string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Uninstall a cron (DELETE)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			c, ok := opsComponents()[component]
			if !ok {
				return fmt.Errorf("unknown component %q", component)
			}
			pf, err := loadOpsProvision(of.provisionYAML)
			if err != nil {
				return err
			}
			hosts, _, err := opsTargets(pf, of.node, *f)
			if err != nil {
				return err
			}
			for _, h := range hosts {
				conn, err := opsConnect(h, *f)
				if err != nil {
					fmt.Printf("%s: connect: %v\n", h.Host, err)
					continue
				}
				if len(c.units) > 0 {
					_, _, _ = ssh.Run(conn, `sudo systemctl stop `+strings.Join(c.units, " ")+` 2>/dev/null || true`)
				}
				if err := c.remove(conn); err != nil {
					fmt.Printf("%s %s: ✗ %v\n", h.Host, component, err)
					closeConn(conn)
					continue
				}
				fmt.Printf("%s %s: removed\n", h.Host, component)
				closeConn(conn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&component, "component", "", "Component (allowlist, security, state, traefik, logrotate)")
	_ = cmd.MarkFlagRequired("component")
	return cmd
}
