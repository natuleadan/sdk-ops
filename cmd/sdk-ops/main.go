package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/ssh"
)

var version = "dev"
var gInsecure bool

func main() {
	var rootCmd = &cobra.Command{
		Use:   "sdk-ops",
		Short: "sdk-ops — VPS provisioning and operations CLI",
		Long: `sdk-ops provisions, hardens, and operates servers.

Three targets:
  --k3s       Install Docker + k3s (default)
  --docker    Install Docker only (no k3s)
  --bare      Install nothing, just harden the OS

Examples:
  sdk-ops infra init 188.xxx.xxx.xxx --user root --key ~/.ssh/id_ed25519
  sdk-ops infra init 188.xxx.xxx.xxx --docker
  sdk-ops infra init 188.xxx.xxx.xxx --bare
  sdk-ops infra init 188.xxx.xxx.xxx --k3s --crowdsec`,
		Version: version,
	}

	rootCmd.PersistentFlags().BoolVar(&gInsecure, "insecure", false, "Skip SSH host key verification")

	rootCmd.AddCommand(newInfraCmd())
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newDeployCmd())
	rootCmd.AddCommand(newServiceCmd())
	rootCmd.AddCommand(newClusterCmd())
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newProviderCmd())
	rootCmd.AddCommand(newNotifyCmd())
	rootCmd.AddCommand(newDbCmd())
	rootCmd.AddCommand(newAgentCmd())
	rootCmd.AddCommand(newComposeCmd())
	rootCmd.AddCommand(newKeyCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newCompletionCmd(rootCmd))
	rootCmd.AddCommand(newStateCmd())
	rootCmd.AddCommand(newBunnyCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show sdk-ops version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sdk-ops version %s\n", version)
		},
	}
}

func newSSHClient(host, user string, port int, keyPath string) *ssh.Client {
	opts := []ssh.Option{ssh.WithPort(port)}
	if keyPath != "" {
		opts = append(opts, ssh.WithKey(keyPath))
	}
	if gInsecure {
		opts = append(opts, ssh.WithInsecure())
	}
	return ssh.New(host, user, opts...)
}
