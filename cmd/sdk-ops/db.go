package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/deploy"
)

func newDbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Provision and manage databases on nodes",
	}

	cmd.AddCommand(newDbCreateCmd())
	cmd.AddCommand(newDbListCmd())
	cmd.AddCommand(newDbRemoveCmd())

	return cmd
}

func nodeFromConfig() (ip, user, key string, port int) {
	cfg, err := loadConfig()
	if err != nil {
		return "", "", "", 0
	}
	if len(cfg.Nodes) == 0 {
		return "", "", "", 0
	}
	return cfg.Nodes[0].IP, cfg.Nodes[0].User, cfg.Nodes[0].Key, cfg.Nodes[0].Port
}

func newDbCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <type> [--name name] [--port N] [--version X] [--node IP]",
		Short: "Create a database (postgres, mysql, redis, mongodb)",
		Args:  cobra.ExactArgs(1),
		Long: `Provision a database container and return connection string.

Supported types: postgres, mysql, redis, mongodb

If --db-port is omitted, the database is only accessible inside Docker
networking (internal only). Use --db-port to expose externally.

Examples:
  sdk-ops db create postgres --name mydb
  sdk-ops db create postgres --name mydb --db-port 5432
  sdk-ops db create redis --db-port 6379
  sdk-ops db create mysql --version 8.0 --db-port 3306
  sdk-ops db create mongodb --db-port 27017`,
		RunE: dbCreateRunE,
	}

	cmd.Flags().String("name", "", "Database name (default: type name)")
	cmd.Flags().String("version", "", "Database version (e.g., 17-alpine, 8.0)")
	cmd.Flags().Int("db-port", 0, "Expose on external port (0 = internal only)")
	cmd.Flags().String("db-user", "", "Database user (generated if empty)")
	cmd.Flags().String("db-pass", "", "Database password (generated if empty)")
	cmd.Flags().StringP("node", "n", "", "Target node IP (default: first registered)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

func dbCreateRunE(cmd *cobra.Command, args []string) error {
	dbType := deploy.DBType(args[0])
	name, _ := cmd.Flags().GetString("name")
	version, _ := cmd.Flags().GetString("version")
	exposePort, _ := cmd.Flags().GetInt("db-port")
	nodeIP, _ := cmd.Flags().GetString("node")
	dbUser, _ := cmd.Flags().GetString("db-user")
	dbPass, _ := cmd.Flags().GetString("db-pass")
	user, _ := cmd.Flags().GetString("user")
	key, _ := cmd.Flags().GetString("key")
	port, _ := cmd.Flags().GetInt("port")

	if nodeIP == "" {
		nip, nu, nk, np := nodeFromConfig()
		if nip == "" {
			return fmt.Errorf("no nodes registered. Use --node <ip>")
		}
		nodeIP = nip
		if user == "" {
			user = nu
		}
		if key == "" {
			key = nk
		}
		if port == 0 {
			port = np
		}
		fmt.Printf("  Using first registered node: %s\n", nodeIP)
	}

	client := newSSHClient(nodeIP, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "db: conn close error: %v\n", err) } }()

	cfg := deploy.DBConfig{
		Type:    dbType,
		Name:    name,
		Version: version,
		Port:    exposePort,
		User:    dbUser,
		Pass:    dbPass,
	}

	result, err := deploy.ProvisionDatabase(conn, cfg)
	if err != nil {
		return fmt.Errorf("provision database: %w", err)
	}

	fmt.Println()
	fmt.Printf("  ✅ %s database ready\n", result.Image)
	fmt.Printf("     Container: %s\n", result.ContainerName)
	fmt.Printf("     Connection: %s\n", result.ConnString)
	if result.ExposedPort > 0 {
		fmt.Printf("     Port:      %d\n", result.ExposedPort)
	} else {
		fmt.Printf("     Port:      internal only (Docker networking)\n")
	}
	stateRecord("database", result.ContainerName, nodeIP, result.Image, "docker", "ok", map[string]string{
		"type":    string(dbType),
		"port":    fmt.Sprintf("%d", result.ExposedPort),
		"connstr": result.ConnString,
	})

	fmt.Println()
	fmt.Println("  To connect from another container on the same node:")
	fmt.Printf("    docker run --rm --link %s alpine env\n", result.ContainerName)
	fmt.Println()
	fmt.Printf("  Connection string (copy-paste):\n")
	fmt.Printf("    %s\n", result.ConnString)

	return nil
}

func newDbListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [--node IP]",
		Short: "List databases on a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")

			if nodeIP == "" {
				nip, nu, nk, np := nodeFromConfig()
				if nip == "" {
					return fmt.Errorf("no nodes registered")
				}
				nodeIP = nip
				if user == "" {
					user = nu
				}
				if key == "" {
					key = nk
				}
				if port == 0 {
					port = np
				}
			}
			if port == 0 {
				port = 22
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "db: conn close error: %v\n", err) } }()

			dbs, err := deploy.ListDatabases(conn)
			if err != nil {
				return err
			}
			if len(dbs) == 0 {
				fmt.Printf("  No databases on %s\n", nodeIP)
				return nil
			}
			fmt.Printf("  Databases on %s:\n", nodeIP)
			for _, db := range dbs {
				fmt.Printf("    %s\n", db)
			}
			return nil
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP (default: first registered)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

func newDbRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name> [--node IP]",
		Short: "Remove a database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			nodeIP, _ := cmd.Flags().GetString("node")
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")

			if nodeIP == "" {
				nip, nu, nk, np := nodeFromConfig()
				if nip == "" {
					return fmt.Errorf("no nodes registered")
				}
				nodeIP = nip
				if user == "" {
					user = nu
				}
				if key == "" {
					key = nk
				}
				if port == 0 {
					port = np
				}
			}
			if port == 0 {
				port = 22
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "db: conn close error: %v\n", err) } }()

			if err := deploy.RemoveDatabase(conn, name); err != nil {
				return err
			}
			fmt.Printf("  ✅ Database %s removed\n", name)
			return nil
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP (default: first registered)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}
