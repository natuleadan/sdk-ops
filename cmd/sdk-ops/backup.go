package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/deploy"
)

func newBackupCmd() *cobra.Command {
	var user, key string
	var port int

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore services on a node",
	}

	createCmd := &cobra.Command{
		Use:   "create <ip>",
		Short: "Create a backup of all services on a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSSHClient(args[0], user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			path, err := deploy.BackupServices(conn, ".")
			if err != nil {
				return err
			}
			fmt.Printf("✅ Backup: %s\n", path)
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <ip> <backup-file>",
		Short: "Restore services from a backup file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSSHClient(args[0], user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			if err := deploy.RestoreServices(conn, args[1]); err != nil {
				return err
			}
			fmt.Println("✅ Restore complete")
			return nil
		},
	}

	createCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	createCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	createCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")
	restoreCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	restoreCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	restoreCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	cmd.AddCommand(createCmd)
	cmd.AddCommand(restoreCmd)
	return cmd
}
