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
		Short: "Backup, restore, and schedule backups for services and databases",
	}

	// --- create (services backup) ---
	createCmd := &cobra.Command{
		Use:   "create <ip>",
		Short: "Backup all services on a node to a local file",
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

			// Upload to S3 if configured
			s3Cfg := deploy.S3ConfigFromEnv()
			if s3Cfg.Bucket != "" {
				if err := deploy.UploadToS3(path, s3Cfg); err != nil {
					return fmt.Errorf("s3 upload: %w", err)
				}
			}

			fmt.Printf("✅ Backup: %s\n", path)
			return nil
		},
	}

	// --- db-backup ---
	dbBackupCmd := &cobra.Command{
		Use:   "db <type> <container-name> --node <ip>",
		Short: "Backup a database (postgres, mysql, mongodb, redis)",
		Args:  cobra.ExactArgs(2),
		Long: `Backup a database by dumping from its Docker container.

Type must be one of: postgres, mysql, mongodb, redis

Examples:
  sdk-ops backup db postgres my-postgres --node 1.2.3.4 --db-name myapp
  sdk-ops backup db mysql my-mysql --node 1.2.3.4
  sdk-ops backup db redis my-redis --node 1.2.3.4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbTypeStr := args[0]
			containerName := args[1]
			dbName, _ := cmd.Flags().GetString("db-name")
			nodeIP, _ := cmd.Flags().GetString("node")

			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			dbType := deploy.DBType(dbTypeStr)

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			path, err := deploy.BackupDatabase(conn, dbType, dbName, containerName, ".")
			if err != nil {
				return err
			}

			// Upload to S3 if configured
			s3Cfg := deploy.S3ConfigFromEnv()
			if s3Cfg.Bucket != "" {
				if err := deploy.UploadToS3(path, s3Cfg); err != nil {
					return fmt.Errorf("s3 upload: %w", err)
				}
			}

			fmt.Printf("✅ Database backup: %s\n", path)
			return nil
		},
	}

	// --- schedule ---
	scheduleCmd := &cobra.Command{
		Use:   "schedule <type> [--db-name X] [--container X] [--cron expr] [--node ip]",
		Short: "Schedule a backup via systemd timer",
		Long: `Schedule periodic backups using systemd timers.

Type: services, postgres, mysql, mongodb, redis

Examples:
  sdk-ops backup schedule services --cron "0 3 * * *" --node 1.2.3.4
  sdk-ops backup schedule postgres --db-name myapp --container my-postgres --cron "0 */6 * * *" --node 1.2.3.4
  sdk-ops backup schedule redis --container my-redis --cron "0 * * * *" --node 1.2.3.4`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupType := deploy.BackupType(args[0])

			// Validate backup type
			switch backupType {
			case deploy.BackupTypeServices, deploy.BackupTypePostgres, deploy.BackupTypeMySQL, deploy.BackupTypeMongoDB, deploy.BackupTypeRedis:
			default:
				return fmt.Errorf("invalid backup type: %s (valid: services, postgres, mysql, mongodb, redis)", backupType)
			}

			dbName, _ := cmd.Flags().GetString("db-name")
			containerName, _ := cmd.Flags().GetString("container")
			cronExpr, _ := cmd.Flags().GetString("cron")
			nodeIP, _ := cmd.Flags().GetString("node")

			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}
			if cronExpr == "" {
				cronExpr = "0 3 * * *" // default: daily at 3am
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			var s3Cfg *deploy.S3Config
			if envCfg := deploy.S3ConfigFromEnv(); envCfg.Bucket != "" {
				s3Cfg = &envCfg
			}

			if err := deploy.ScheduleBackup(conn, backupType, dbName, containerName, cronExpr, s3Cfg); err != nil {
				return err
			}
			stateRecord("backup_schedule", string(backupType), nodeIP, cronExpr, "", "active", map[string]string{
				"db_name":    dbName,
				"container":  containerName,
				"s3_bucket":  func() string { if s3Cfg != nil { return s3Cfg.Bucket }; return "" }(),
			})
			fmt.Printf("✅ Backup scheduled\n")
			return nil
		},
	}

	// --- unschedule ---
	unscheduleCmd := &cobra.Command{
		Use:   "unschedule <type> [--node ip]",
		Short: "Remove a scheduled backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupType := deploy.BackupType(args[0])

			// Validate backup type
			switch backupType {
			case deploy.BackupTypeServices, deploy.BackupTypePostgres, deploy.BackupTypeMySQL, deploy.BackupTypeMongoDB, deploy.BackupTypeRedis:
			default:
				return fmt.Errorf("invalid backup type: %s (valid: services, postgres, mysql, mongodb, redis)", backupType)
			}

			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			return deploy.UnscheduleBackup(conn, backupType)
		},
	}

	// --- list-schedules ---
	listSchedulesCmd := &cobra.Command{
		Use:   "list-schedules [--node ip]",
		Short: "List backup schedules on a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			schedules, err := deploy.ListBackupSchedules(conn)
			if err != nil {
				return err
			}
			if len(schedules) == 0 {
				fmt.Printf("  No backup schedules on %s\n", nodeIP)
				return nil
			}
			fmt.Printf("  Backup schedules on %s:\n", nodeIP)
			for _, s := range schedules {
				fmt.Printf("    %s\n", s)
			}
			return nil
		},
	}

	// --- restore (existing) ---
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

	dbBackupCmd.Flags().StringP("node", "n", "", "Target node IP")
	dbBackupCmd.Flags().String("db-name", "", "Database name (for postgres)")
	dbBackupCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	dbBackupCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	dbBackupCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	scheduleCmd.Flags().StringP("node", "n", "", "Target node IP")
	scheduleCmd.Flags().String("cron", "0 3 * * *", "Cron expression")
	scheduleCmd.Flags().String("db-name", "", "Database name (for postgres)")
	scheduleCmd.Flags().String("container", "", "Container name (for database backups)")
	scheduleCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	scheduleCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	scheduleCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	unscheduleCmd.Flags().StringP("node", "n", "", "Target node IP")
	unscheduleCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	unscheduleCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	unscheduleCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	listSchedulesCmd.Flags().StringP("node", "n", "", "Target node IP")
	listSchedulesCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	listSchedulesCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	listSchedulesCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	restoreCmd.Flags().StringVarP(&user, "user", "u", "sdkops", "SSH user")
	restoreCmd.Flags().StringVarP(&key, "key", "k", "", "SSH key path")
	restoreCmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port")

	cmd.AddCommand(createCmd)
	cmd.AddCommand(dbBackupCmd)
	cmd.AddCommand(scheduleCmd)
	cmd.AddCommand(unscheduleCmd)
	cmd.AddCommand(listSchedulesCmd)
	cmd.AddCommand(restoreCmd)
	return cmd
}


