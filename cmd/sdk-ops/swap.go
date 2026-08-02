package main

import (
	"fmt"

	"github.com/spf13/cobra"
	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/hardening"
)

// newSwapCmd: operator-managed swap (create/update/remove/status).
// The rule is bottom-up: 0.5x RAM base (always), +0.5x RAM per 10GB of free
// disk, capped at 2x RAM.
func newSwapCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap",
		Short: "Manage swap on a node (create/update/remove/status)",
	}

	create := &cobra.Command{
		Use:   "create",
		Short: "Create or resize the swap file (0.5x base, +0.5x per 10GB free, cap 2x)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runSwapSimple(f, cobraCmd, hardening.ApplySwap)
		},
	}
	create.Flags().StringP("node", "n", "", "Target node IP")

	update := &cobra.Command{
		Use:   "update",
		Short: "Recompute and resize the swap file (alias of create, forces resize)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runSwapSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				// Force a resize even if the size matches by removing first.
				if err := hardening.RemoveSwap(conn); err != nil {
					return err
				}
				return hardening.ApplySwap(conn)
			})
		},
	}
	update.Flags().StringP("node", "n", "", "Target node IP")

	remove := &cobra.Command{
		Use:   "remove",
		Short: "Disable and delete the swap file",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runSwapSimple(f, cobraCmd, hardening.RemoveSwap)
		},
	}
	remove.Flags().StringP("node", "n", "", "Target node IP")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show the current swap state",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runSwapSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				out, err := hardening.SwapStatus(conn)
				if err != nil {
					return err
				}
				fmt.Print(out)
				return nil
			})
		},
	}
	status.Flags().StringP("node", "n", "", "Target node IP")

	cmd.AddCommand(create)
	cmd.AddCommand(update)
	cmd.AddCommand(remove)
	cmd.AddCommand(status)
	return cmd
}

func runSwapSimple(f *infraFlags, cobraCmd *cobra.Command, fn func(*golang_ssh.Client) error) error {
	node := firewalledNode(cobraCmd)
	if node == "" {
		return fmt.Errorf("no node specified. Use --node or register one")
	}
	conn, err := infraConnect(node, f)
	if err != nil {
		return err
	}
	defer closeConn(conn)
	return fn(conn)
}
