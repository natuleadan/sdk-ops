package main

import "github.com/spf13/cobra"

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
