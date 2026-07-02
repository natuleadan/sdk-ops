package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/compose"
)

func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Manage docker-compose.yml files",
	}

	initCmd := &cobra.Command{
		Use:   "init <path> [--name app]",
		Short: "Create a new docker-compose.yml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			path := args[0]
			if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
				path += "/docker-compose.yml"
			}
			if err := compose.Init(path, name); err != nil {
				return err
			}
			fmt.Printf("  ✅ Created %s\n", path)
			return nil
		},
	}

	serviceCmd := &cobra.Command{
		Use:   "service",
		Short: "Manage services in docker-compose.yml",
	}

	addCmd := &cobra.Command{
		Use:   "add <name> --image <image> [--port N] [--file compose.yml]",
		Short: "Add a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			image, _ := cmd.Flags().GetString("image")
			port, _ := cmd.Flags().GetInt("port")
			filePath, _ := cmd.Flags().GetString("file")

			if image == "" {
				return fmt.Errorf("--image is required")
			}
			if err := compose.AddService(filePath, args[0], image, port); err != nil {
				return err
			}
			fmt.Printf("  ✅ Service %q added to %s\n", args[0], filePath)
			return nil
		},
	}

	rmCmd := &cobra.Command{
		Use:   "rm <name> [--file compose.yml]",
		Short: "Remove a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if err := compose.RemoveService(filePath, args[0]); err != nil {
				return err
			}
			fmt.Printf("  ✅ Service %q removed from %s\n", args[0], filePath)
			return nil
		},
	}

	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables of a service",
	}

	envSetCmd := &cobra.Command{
		Use:   "set <service> <key>=<value> [--file compose.yml]",
		Short: "Set an environment variable",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			service, kv := args[0], args[1]
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("format: KEY=value")
			}
			if err := compose.SetEnv(filePath, service, parts[0], parts[1]); err != nil {
				return err
			}
			fmt.Printf("  ✅ %s=%s set on service %q\n", parts[0], parts[1], service)
			return nil
		},
	}

	envUnsetCmd := &cobra.Command{
		Use:   "unset <service> <key> [--file compose.yml]",
		Short: "Unset an environment variable",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if err := compose.UnsetEnv(filePath, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("  ✅ %s unset on service %q\n", args[1], args[0])
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list [--file compose.yml]",
		Short: "List services",
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			services, err := compose.ListServices(filePath)
			if err != nil {
				return err
			}
			if len(services) == 0 {
				fmt.Printf("  No services in %s\n", filePath)
				return nil
			}
			fmt.Printf("  Services in %s:\n", filePath)
			for _, s := range services {
				fmt.Printf("    - %s\n", s)
			}
			return nil
		},
	}

	validateCmd := &cobra.Command{
		Use:   "validate [--file compose.yml]",
		Short: "Validate docker-compose.yml syntax",
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if err := compose.Validate(filePath); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}
			fmt.Printf("  ✅ %s is valid\n", filePath)
			return nil
		},
	}

	for _, sc := range []*cobra.Command{addCmd, rmCmd, listCmd, validateCmd} {
		sc.Flags().StringP("file", "f", "docker-compose.yml", "Path to docker-compose.yml")
	}
	addCmd.Flags().String("image", "", "Docker image (required)")
	addCmd.Flags().Int("port", 0, "Expose port")
	envSetCmd.Flags().StringP("file", "f", "docker-compose.yml", "Path to docker-compose.yml")
	envUnsetCmd.Flags().StringP("file", "f", "docker-compose.yml", "Path to docker-compose.yml")

	initCmd.Flags().StringP("name", "n", "app", "Service name")

	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envUnsetCmd)
	serviceCmd.AddCommand(addCmd)
	serviceCmd.AddCommand(rmCmd)
	serviceCmd.AddCommand(listCmd)
	serviceCmd.AddCommand(envCmd)
	cmd.AddCommand(initCmd)
	cmd.AddCommand(serviceCmd)
	cmd.AddCommand(validateCmd)
	return cmd
}
