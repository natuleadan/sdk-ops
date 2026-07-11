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

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish>",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for bash, zsh, or fish.

Usage:
  sdk-ops completion bash  > /etc/bash_completion.d/sdk-ops
  sdk-ops completion zsh   > /usr/local/share/zsh/site-functions/_sdk-ops
  sdk-ops completion fish  > ~/.config/fish/completions/sdk-ops.fish`,
	}
	for _, s := range []struct{ name, desc string }{
		{"bash", "Generate bash completion script"},
		{"zsh", "Generate zsh completion script"},
		{"fish", "Generate fish completion script"},
	} {
		shell := s.name
		cmd.AddCommand(&cobra.Command{
			Use:   s.name,
			Short: s.desc,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				switch shell {
				case "bash":
					return root.GenBashCompletion(cmd.OutOrStdout())
				case "zsh":
					return root.GenZshCompletion(cmd.OutOrStdout())
				case "fish":
					return root.GenFishCompletion(cmd.OutOrStdout(), true)
				}
				return nil
			},
		})
	}
	return cmd
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
