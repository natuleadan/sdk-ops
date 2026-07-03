package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/ssh"
)

func newClusterCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "cluster",
		Short: "Operate k3s cluster (no kubectl needed)",
		Long: `Operate a k3s cluster without needing kubectl.
All commands run via SSH on the k3s server node.

Examples:
  sdk-ops cluster nodes
  sdk-ops cluster pods
  sdk-ops cluster top
  sdk-ops cluster logs my-pod-xxx -f
  sdk-ops cluster scale deploy/products-svc --replicas 5`,
	}

	kubecmd := func(use, short, k8sArgs string) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runClusterKubectl(k8sArgs, cmd, args)
			},
		}
	}

	cmd.AddCommand(kubecmd("nodes", "List cluster nodes", "get nodes -o wide"))
	cmd.AddCommand(kubecmd("pods", "List all pods", "get pods --all-namespaces"))
	cmd.AddCommand(kubecmd("services", "List all services", "get services --all-namespaces"))
	cmd.AddCommand(kubecmd("deployments", "List all deployments", "get deployments --all-namespaces"))
	cmd.AddCommand(kubecmd("ingresses", "List all ingresses", "get ingress --all-namespaces"))
	cmd.AddCommand(kubecmd("configmaps", "List all configmaps", "get configmaps --all-namespaces"))
	cmd.AddCommand(kubecmd("secrets", "List all secrets", "get secrets --all-namespaces"))
	cmd.AddCommand(kubecmd("info", "Show cluster info", "cluster-info"))
	cmd.AddCommand(kubecmd("version", "Show k3s version", "version"))

	topCmd := &cobra.Command{
		Use:   "top",
		Short: "Show resource usage (nodes + pods)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterTop(cmd)
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs <pod>",
		Short: "Show pod logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			tail, _ := cmd.Flags().GetInt("tail")
			follow, _ := cmd.Flags().GetBool("follow")
			return runClusterLogs(args[0], ns, tail, follow, cmd)
		},
	}
	logsCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	logsCmd.Flags().IntP("tail", "t", 50, "Lines to show")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow logs")

	execCmd := &cobra.Command{
		Use:   "exec <pod> -- <command>",
		Short: "Execute a command in a pod",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			return runClusterExec(args[0], args[1:], ns, cmd)
		},
	}
	execCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	scaleCmd := &cobra.Command{
		Use:   "scale <resource> --replicas N",
		Short: "Scale a deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			replicas, _ := cmd.Flags().GetInt("replicas")
			ns, _ := cmd.Flags().GetString("namespace")
			if replicas < 0 {
				return fmt.Errorf("--replicas is required")
			}
			return runClusterScale(args[0], replicas, ns, cmd)
		},
	}
	scaleCmd.Flags().IntP("replicas", "r", 0, "Number of replicas")
	if err := scaleCmd.MarkFlagRequired("replicas"); err != nil { panic(err) }
	scaleCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	applyCmd := &cobra.Command{
		Use:   "apply <file>",
		Short: "Apply a YAML file to the cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterApply(args[0], cmd)
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <resource> <name>",
		Short: "Delete a resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			return runClusterDelete(args[0], args[1], ns, cmd)
		},
	}
	deleteCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	describeCmd := &cobra.Command{
		Use:   "describe <resource> <name>",
		Short: "Describe a resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			return runClusterDescribe(args[0], args[1], ns, cmd)
		},
	}
	describeCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	// --- New commands 15-24 ---

	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Show cluster join token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterToken(cmd)
		},
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart k3s service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterRestart(cmd)
		},
	}

	eventsCmd := &cobra.Command{
		Use:   "events",
		Short: "Show cluster events",
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			eventType, _ := cmd.Flags().GetString("type")
			return runClusterEvents(ns, eventType, cmd)
		},
	}
	eventsCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	eventsCmd.Flags().String("type", "", "Filter by type (Normal, Warning)")

	cordonCmd := &cobra.Command{
		Use:   "cordon <node>",
		Short: "Mark node as unschedulable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterNodeOp("cordon", args[0], cmd)
		},
	}

	uncordonCmd := &cobra.Command{
		Use:   "uncordon <node>",
		Short: "Mark node as schedulable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterNodeOp("uncordon", args[0], cmd)
		},
	}

	drainCmd := &cobra.Command{
		Use:   "drain <node>",
		Short: "Drain node for maintenance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterNodeOp("drain --ignore-daemonsets --delete-emptydir-data", args[0], cmd)
		},
	}

	labelCmd := &cobra.Command{
		Use:   "label <node> <key=value>",
		Short: "Label a node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterNodeOp(fmt.Sprintf("label node %s %s", args[0], args[1]), "", cmd)
		},
	}

	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade k3s to a specific version",
		RunE: func(cmd *cobra.Command, args []string) error {
			version, _ := cmd.Flags().GetString("version")
			return runClusterUpgrade(version, cmd)
		},
	}
	upgradeCmd.Flags().String("version", "", "Target version (default: latest stable)")

	etcdSnapshotCmd := &cobra.Command{
		Use:   "etcd-snapshot",
		Short: "Create an etcd snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterEtcdSnapshot(cmd)
		},
	}

	certRotateCmd := &cobra.Command{
		Use:   "cert-rotate",
		Short: "Rotate k3s certificates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterCertRotate(cmd)
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <type> <name>",
		Short: "Get a resource as YAML",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			format, _ := cmd.Flags().GetString("output")
			return runClusterGet(args[0], args[1], ns, format, cmd)
		},
	}
	getCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	getCmd.Flags().StringP("output", "o", "yaml", "Output format (yaml, json, wide)")

	// --- Commands 25-29 ---

	helmCmd := &cobra.Command{
		Use:   "helm",
		Short: "Manage Helm charts",
	}
	helmRepoAddCmd := &cobra.Command{
		Use:   "repo-add <name> <url>",
		Short: "Add a Helm repository",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterHelm(fmt.Sprintf("repo add %s %s", args[0], args[1]), cmd)
		},
	}
	helmRepoListCmd := &cobra.Command{
		Use:   "repo-list",
		Short: "List Helm repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterHelm("repo list", cmd)
		},
	}
	helmInstallCmd := &cobra.Command{
		Use:   "install <name> <chart>",
		Short: "Install a Helm chart",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			kargs := fmt.Sprintf("install %s %s", args[0], args[1])
			if ns != "" {
				kargs += fmt.Sprintf(" --namespace=%s", ns)
			}
			return runClusterHelm(kargs, cmd)
		},
	}
	helmInstallCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	helmUpgradeCmd := &cobra.Command{
		Use:   "upgrade <name> <chart>",
		Short: "Upgrade a Helm release",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			kargs := fmt.Sprintf("upgrade %s %s", args[0], args[1])
			if ns != "" {
				kargs += fmt.Sprintf(" --namespace=%s", ns)
			}
			return runClusterHelm(kargs, cmd)
		},
	}
	helmUpgradeCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	helmListCmd := &cobra.Command{
		Use:   "list",
		Short: "List Helm releases",
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			kargs := "list"
			if ns != "" {
				kargs += fmt.Sprintf(" --namespace=%s", ns)
			}
			return runClusterHelm(kargs, cmd)
		},
	}
	helmListCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")
	helmCmd.AddCommand(helmRepoAddCmd)
	helmCmd.AddCommand(helmRepoListCmd)
	helmCmd.AddCommand(helmInstallCmd)
	helmCmd.AddCommand(helmUpgradeCmd)
	helmCmd.AddCommand(helmListCmd)

	nodeSSHCmd := &cobra.Command{
		Use:   "node-ssh <node-name>",
		Short: "SSH into a cluster node",
		Args:  cobra.ExactArgs(1),
		Long: `Get the internal IP of a cluster node and SSH into it.
Uses the same SSH key as the k3s server connection.

Example:
  sdk-ops cluster node-ssh node-1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterNodeSSH(args[0], cmd)
		},
	}

	portForwardCmd := &cobra.Command{
		Use:   "port-forward <pod> <local:remote>",
		Short: "Forward a port to a pod",
		Args:  cobra.ExactArgs(2),
		Long: `Forward a local port to a port on a pod via SSH tunnel.

Example:
  sdk-ops cluster port-forward my-pod 8080:3000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			return runClusterPortForward(args[0], args[1], ns, cmd)
		},
	}
	portForwardCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	etcdRestoreCmd := &cobra.Command{
		Use:   "etcd-restore <snapshot-file>",
		Short: "Restore etcd from a snapshot",
		Args:  cobra.ExactArgs(1),
		Long: `Restore etcd from a snapshot file on the server.
Stops k3s, restores, and starts with --cluster-reset.

Example:
  sdk-ops cluster etcd-restore /var/lib/rancher/k3s/server/db/snapshots/on-demand/server-xxx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterEtcdRestore(args[0], cmd)
		},
	}

	// Add global flags to all subcommands
	for _, sc := range []*cobra.Command{
		topCmd, logsCmd, execCmd, scaleCmd, applyCmd, deleteCmd, describeCmd,
		tokenCmd, restartCmd, eventsCmd, cordonCmd, uncordonCmd, drainCmd,
		labelCmd, upgradeCmd, etcdSnapshotCmd, certRotateCmd, getCmd,
		helmCmd, helmRepoAddCmd, helmRepoListCmd, helmInstallCmd, helmUpgradeCmd, helmListCmd,
		nodeSSHCmd, portForwardCmd, etcdRestoreCmd,
	} {
		sc.Flags().StringP("node", "N", "", "k3s server node IP (default: first registered)")
		sc.Flags().StringP("user", "u", "root", "SSH user")
		sc.Flags().StringP("key", "k", "", "SSH private key path")
		sc.Flags().IntP("port", "p", 22, "SSH port")
	}

	// Add global flags to kubecmd subcommands too
	for _, name := range []string{
		"nodes", "pods", "services", "deployments", "ingresses",
		"configmaps", "secrets", "info", "version",
	} {
		sc := cmd.Commands()[findCmdIndex(cmd.Commands(), name)]
		sc.Flags().StringP("node", "N", "", "k3s server node IP (default: first registered)")
		sc.Flags().StringP("user", "u", "root", "SSH user")
		sc.Flags().StringP("key", "k", "", "SSH private key path")
		sc.Flags().IntP("port", "p", 22, "SSH port")
	}

	cmd.AddCommand(topCmd)
	cmd.AddCommand(logsCmd)
	cmd.AddCommand(execCmd)
	cmd.AddCommand(scaleCmd)
	cmd.AddCommand(applyCmd)
	cmd.AddCommand(deleteCmd)
	cmd.AddCommand(describeCmd)
	cmd.AddCommand(tokenCmd)
	cmd.AddCommand(restartCmd)
	cmd.AddCommand(eventsCmd)
	cmd.AddCommand(cordonCmd)
	cmd.AddCommand(uncordonCmd)
	cmd.AddCommand(drainCmd)
	cmd.AddCommand(labelCmd)
	cmd.AddCommand(upgradeCmd)
	cmd.AddCommand(etcdSnapshotCmd)
	cmd.AddCommand(certRotateCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(helmCmd)
	cmd.AddCommand(nodeSSHCmd)
	cmd.AddCommand(portForwardCmd)
	cmd.AddCommand(etcdRestoreCmd)

	return cmd
}

func findCmdIndex(commands []*cobra.Command, name string) int {
	for i, c := range commands {
		if c.Name() == name {
			return i
		}
	}
	return -1
}

func getClusterFlags(cmd *cobra.Command) (ip, user, key string, port int) {
	ip, _ = cmd.Flags().GetString("node")
	user, _ = cmd.Flags().GetString("user")
	key, _ = cmd.Flags().GetString("key")
	port, _ = cmd.Flags().GetInt("port")

	if ip == "" {
		cfg, err := loadConfig()
		if err == nil {
			for _, n := range cfg.Nodes {
				if n.Mode == "k3s" || strings.HasPrefix(n.Mode, "k3s") {
					ip = n.IP
					user = n.User
					key = n.Key
					port = n.Port
					break
				}
			}
			if ip == "" && len(cfg.Nodes) > 0 {
				ip = cfg.Nodes[0].IP
				user = cfg.Nodes[0].User
				key = cfg.Nodes[0].Key
				port = cfg.Nodes[0].Port
			}
		}
	}
	if ip == "" {
		fmt.Fprintln(os.Stderr, "  error: no k3s node specified. Use --node <ip> or register a node.")
		os.Exit(1)
	}
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	return
}

func k3sExec(ip, user, key string, port int, kubectlCmd string) error {
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	// Auto-install k3s if not present
	k3sOut, _, _ := ssh.Run(conn, "command -v k3s || echo 'no-k3s'")
	if strings.TrimSpace(k3sOut) == "no-k3s" {
		fmt.Println("  → k3s not found, installing...")
		installCfg := k3s.DefaultInstallConfig(ip)
		if err := k3s.Install(conn, installCfg); err != nil {
			return fmt.Errorf("auto-install k3s: %w", err)
		}
	}

	fullCmd := fmt.Sprintf("sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl %s", kubectlCmd)
	out, _, err := ssh.Run(conn, fullCmd)
	if err != nil {
		return fmt.Errorf("kubectl error: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func k3sExecPTY(ip, user, key string, port int, kubectlCmd string) error {
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	fullCmd := fmt.Sprintf("sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl %s", kubectlCmd)
	return ssh.RunPTY(conn, fullCmd)
}

func runClusterKubectl(args string, cmd *cobra.Command, cmdArgs []string) error {
	ip, user, key, port := getClusterFlags(cmd)
	fullArgs := args
	if len(cmdArgs) > 0 {
		fullArgs = args + " " + strings.Join(cmdArgs, " ")
	}
	return k3sExec(ip, user, key, port, fullArgs)
}

func runClusterTop(cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	fmt.Println("  Nodes:")
	if err := k3sExec(ip, user, key, port, "top nodes"); err != nil {
		return err
	}
	fmt.Println("\n  Pods:")
	return k3sExec(ip, user, key, port, "top pods --all-namespaces")
}

func runClusterLogs(pod, namespace string, tail int, follow bool, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("logs --tail=%d", tail)
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	if follow {
		kargs += " -f"
	}
	kargs += " " + pod

	if follow {
		return k3sExecPTY(ip, user, key, port, kargs)
	}
	return k3sExec(ip, user, key, port, kargs)
}

func runClusterExec(pod string, cmdArgs []string, namespace string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := "exec -it"
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	kargs += " " + pod + " -- " + strings.Join(cmdArgs, " ")
	return k3sExecPTY(ip, user, key, port, kargs)
}

func runClusterScale(resource string, replicas int, namespace string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("scale --replicas=%d %s", replicas, resource)
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	return k3sExec(ip, user, key, port, kargs)
}

func runClusterApply(file string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	return k3sExec(ip, user, key, port, fmt.Sprintf("apply -f %s", file))
}

func runClusterDelete(resource, name, namespace string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("delete %s %s", resource, name)
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	return k3sExec(ip, user, key, port, kargs)
}

func runClusterDescribe(resource, name, namespace string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("describe %s %s", resource, name)
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	return k3sExec(ip, user, key, port, kargs)
}

// 15. cluster token
func runClusterToken(cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()
	out, _, err := ssh.Run(conn, "sudo cat /var/lib/rancher/k3s/server/token")
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	fmt.Printf("  Token: %s", out)
	return nil
}

// 16. cluster restart
func runClusterRestart(cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()
	fmt.Println("  → Restarting k3s...")
	out, _, err := ssh.Run(conn, "sudo systemctl restart k3s && echo 'k3s restarted'")
	if err != nil {
		return fmt.Errorf("restart: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// 17+24. cluster events
func runClusterEvents(namespace, eventType string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := "get events"
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	} else {
		kargs += " --all-namespaces"
	}
	if eventType != "" {
		kargs += fmt.Sprintf(" --field-selector type=%s", eventType)
	}
	return k3sExec(ip, user, key, port, kargs)
}

// 18. cluster node cordon/uncordon/drain
func runClusterNodeOp(op, node string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("%s %s", op, node)
	if op == "label" {
		kargs = op + " " + node
		return k3sExec(ip, user, key, port, kargs)
	}
	return k3sExec(ip, user, key, port, kargs)
}

// 20. cluster upgrade
func runClusterUpgrade(version string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	fmt.Println("  → Upgrading k3s...")
	installCmd := "curl -sfL https://get.k3s.io | sudo sh -"
	if version != "" {
		installCmd = fmt.Sprintf("INSTALL_K3S_VERSION=%s %s", version, installCmd)
	}
	out, _, err := ssh.Run(conn, installCmd)
	if err != nil {
		return fmt.Errorf("upgrade: %w\n%s", err, out)
	}
	fmt.Print(out)
	fmt.Println("  → k3s upgraded successfully")
	return nil
}

// 21. cluster etcd-snapshot
func runClusterEtcdSnapshot(cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()
	fmt.Println("  → Creating etcd snapshot...")
	out, _, err := ssh.Run(conn, "sudo k3s etcd-snapshot save && echo 'etcd-snapshot: OK' || echo 'etcd-snapshot: FAIL'")
	if err != nil {
		return fmt.Errorf("etcd-snapshot: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// 22. cluster cert-rotate
func runClusterCertRotate(cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()
	fmt.Println("  → Rotating certificates...")
	out, _, err := ssh.Run(conn, "sudo k3s certificate rotate && sudo systemctl restart k3s && echo 'cert-rotate: OK'")
	if err != nil {
		return fmt.Errorf("cert-rotate: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// 23. cluster get <type> <name>
func runClusterGet(resType, name, namespace, format string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	kargs := fmt.Sprintf("get %s %s -o %s", resType, name, format)
	if namespace != "" {
		kargs += fmt.Sprintf(" --namespace=%s", namespace)
	}
	return k3sExec(ip, user, key, port, kargs)
}

// 25-26. cluster helm
func runClusterHelm(kargs string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	// Auto-install helm if not present
	out, _, _ := ssh.Run(conn, "command -v helm || echo 'no-helm'")
	if strings.TrimSpace(out) == "no-helm" {
		fmt.Println("  → Installing helm...")
		installOut, _, err := ssh.Run(conn, `curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | sudo bash 2>&1 | tail -1`)
		if err != nil {
			return fmt.Errorf("helm install: %w\n%s", err, installOut)
		}
	}

	fullCmd := fmt.Sprintf("sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml helm %s 2>&1 || true", kargs)
	out, _, err = ssh.Run(conn, fullCmd)
	if err != nil {
		log.Printf("helm command warning (non-fatal): %v", err)
	}
	output := strings.TrimSpace(out)
	if output != "" {
		fmt.Println(output)
	}
	return nil
}

// 27. cluster node-ssh
func runClusterNodeSSH(nodeName string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	// Get node internal IP (first one, prefer IPv4)
	nodeIP, _, err := ssh.Run(conn, fmt.Sprintf(
		`sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl get node %s -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || echo "not-found"`,
		nodeName))
	if err != nil {
		log.Printf("node lookup warning: %v", err)
	}
	nodeIP = strings.TrimSpace(nodeIP)
	if nodeIP == "" || nodeIP == "not-found" || strings.HasPrefix(nodeIP, "not-found") {
		return fmt.Errorf("node %s not found or has no InternalIP", nodeName)
	}
	// Take first IP if multiple are returned
	if idx := strings.IndexAny(nodeIP, " \t\n"); idx >= 0 {
		nodeIP = nodeIP[:idx]
	}
	fmt.Printf("  → SSH to %s (%s)...\n", nodeName, nodeIP)
	fmt.Printf("  ssh %s@%s -p %d -i %s\n", user, nodeIP, port, key)

	// Try SSH into the node directly
	nodeClient := newSSHClient(nodeIP, user, port, key)
	nodeConn, err := nodeClient.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", nodeIP, err)
	}
	defer func() { if err := nodeConn.Close(); err != nil { log.Printf("cluster: node conn close error: %v", err) } }()

	// Interactive shell via PTY
	return ssh.RunPTY(nodeConn, "bash -l")
}

// 28. cluster port-forward
func runClusterPortForward(pod, portMapping, namespace string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	kargs := fmt.Sprintf("port-forward %s %s", pod, portMapping)
	if namespace != "" {
		kargs += fmt.Sprintf(" -n %s", namespace)
	}
	fullCmd := fmt.Sprintf("sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl %s", kargs)
	fmt.Printf("  → Forwarding %s on %s\n", portMapping, ip)
	fmt.Println("  Press Ctrl+C to stop")
	return ssh.RunPTY(conn, fullCmd)
}

// 29. cluster etcd-restore
func runClusterEtcdRestore(snapshotFile string, cmd *cobra.Command) error {
	ip, user, key, port := getClusterFlags(cmd)
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "cluster: conn close error: %v\n", err) } }()

	fmt.Printf("  → Restoring etcd from %s...\n", snapshotFile)
	script := fmt.Sprintf(`
sudo systemctl stop k3s
sudo k3s server --cluster-reset --cluster-reset-restore-path=%s 2>&1
sudo systemctl start k3s
echo "etcd-restore: OK"
`, snapshotFile)
	out, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("etcd-restore: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
