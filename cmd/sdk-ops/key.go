package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Generate and manage SSH keys locally",
	}

	cmd.AddCommand(newKeyGenerateCmd())
	cmd.AddCommand(newKeyListCmd())
	cmd.AddCommand(newKeyDeployCmd())

	return cmd
}

func newKeyGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <name> [--type ed25519|rsa] [--dir ~/.sdk-ops/keys]",
		Short: "Generate a new SSH key pair",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			keyType, _ := cmd.Flags().GetString("type")
			keyDir, _ := cmd.Flags().GetString("dir")

			if keyDir == "" {
				home, _ := os.UserHomeDir()
				keyDir = filepath.Join(home, ".sdk-ops", "keys")
			}

			if err := os.MkdirAll(keyDir, 0700); err != nil {
				return fmt.Errorf("create key dir: %w", err)
			}

			privPath := filepath.Join(keyDir, name)
			pubPath := privPath + ".pub"

			if _, err := os.Stat(privPath); err == nil {
				return fmt.Errorf("key %q already exists at %s", name, privPath)
			}

			argsCmd := []string{"-t", keyType, "-f", privPath, "-N", "", "-C", "sdk-ops " + name}
			if !validateKeyType(keyType) {
				return fmt.Errorf("unsupported key type: %q (use: ed25519, rsa, ecdsa)", keyType)
			}
			if !validateFileName(name) {
				return fmt.Errorf("invalid key name: %q", name)
			}
			cmdExec := exec.CommandContext(context.Background(), "ssh-keygen")
			cmdExec.Args = append(cmdExec.Args, argsCmd...)
			if out, err := cmdExec.CombinedOutput(); err != nil {
				return fmt.Errorf("ssh-keygen: %w\n%s", err, string(out))
			}

			fmt.Printf("  ✅ Key pair generated:\n")
			fmt.Printf("     Private: %s\n", privPath)
			fmt.Printf("     Public:  %s\n", pubPath)
			return nil
		},
	}

	cmd.Flags().StringP("type", "t", "ed25519", "Key type (ed25519, rsa)")
	cmd.Flags().StringP("dir", "d", "", "Key directory (default: ~/.sdk-ops/keys)")
	return cmd
}

func newKeyListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [--dir ~/.sdk-ops/keys]",
		Short: "List local SSH keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			keyDir, _ := cmd.Flags().GetString("dir")
			if keyDir == "" {
				home, _ := os.UserHomeDir()
				keyDir = filepath.Join(home, ".sdk-ops", "keys")
			}

			entries, err := os.ReadDir(keyDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Printf("  No keys in %s\n", keyDir)
					return nil
				}
				return fmt.Errorf("read dir: %w", err)
			}

			fmt.Printf("  Keys in %s:\n", keyDir)
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) == ".pub" {
					continue
				}
				pubPath := filepath.Join(keyDir, e.Name()+".pub")
				pubData, _ := os.ReadFile(filepath.Clean(pubPath))
				info := "no public key"
				if len(pubData) > 0 {
					parts := splitAtLast(string(pubData), " ")
					if len(parts) >= 3 {
						info = parts[2]
					}
				}
				fmt.Printf("    %-20s %s\n", e.Name(), info)
			}
			return nil
		},
	}

	cmd.Flags().StringP("dir", "d", "", "Key directory (default: ~/.sdk-ops/keys)")
	return cmd
}

func newKeyDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy <name> --server <ip> [--user root] [--port 22]",
		Short: "Deploy a local SSH key to a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			server, _ := cmd.Flags().GetString("server")
			sshUser, _ := cmd.Flags().GetString("user")
			sshPort, _ := cmd.Flags().GetInt("port")
			keyDir, _ := cmd.Flags().GetString("dir")

			if server == "" {
				return fmt.Errorf("--server is required")
			}
			if keyDir == "" {
				home, _ := os.UserHomeDir()
				keyDir = filepath.Join(home, ".sdk-ops", "keys")
			}

			pubPath := filepath.Join(keyDir, name+".pub")
			pubData, err := os.ReadFile(filepath.Clean(pubPath))
			if err != nil {
				return fmt.Errorf("read public key: %w", err)
			}

			sshCmd := exec.CommandContext(context.Background(), "ssh")
			sshCmd.Args = append(sshCmd.Args,
				fmt.Sprintf("-p%d", sshPort),
				fmt.Sprintf("%s@%s", sshUser, server),
				fmt.Sprintf("mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '%s' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys", string(pubData)))
			sshCmd.Stdout = os.Stdout
			sshCmd.Stderr = os.Stderr
			if err := sshCmd.Run(); err != nil {
				return fmt.Errorf("ssh deploy: %w", err)
			}

			fmt.Printf("  ✅ Key %q deployed to %s@%s\n", name, sshUser, server)
			return nil
		},
	}

	cmd.Flags().StringP("dir", "d", "", "Key directory (default: ~/.sdk-ops/keys)")
	cmd.Flags().StringP("server", "s", "", "Server IP (required)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

func validateKeyType(t string) bool {
	switch t {
	case "ed25519", "rsa", "ecdsa", "ecdsa-sk", "ed25519-sk", "dsa":
		return true
	}
	return false
}

func validateFileName(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\;|&`$(){}<>! '\"") {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func splitAtLast(s, sep string) []string {
	idx := -1
	for i := len(s) - len(sep); i >= 0; i-- {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}
