package main

import (
	"fmt"
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
	scaleCmd.MarkFlagRequired("replicas")
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

	// Add global flags to all subcommands
	for _, sc := range []*cobra.Command{topCmd, logsCmd, execCmd, scaleCmd, applyCmd, deleteCmd, describeCmd} {
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
	defer conn.Close()

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
	defer conn.Close()

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
