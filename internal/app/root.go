// Package app defines the Cobra command tree for frridge.
package app

import (
	"fmt"

	labruntime "frridge/internal/runtime"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the frridge CLI around a lab runtime service.
func NewRootCommand(service labruntime.Service) *cobra.Command {
	var topologyPath string
	var recreate bool
	var reseed bool
	var purge bool
	var shell bool

	root := &cobra.Command{
		Use:           "frridge",
		Short:         "FRR lab runner for container-based network study",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&topologyPath, "file", "f", "", "Path to lab YAML")

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Create containers, links, and initial router state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if topologyPath == "" {
				return fmt.Errorf("up requires --file")
			}
			return service.Up(cmd.Context(), topologyPath, labruntime.UpOptions{
				Recreate: recreate,
				Reseed:   reseed,
			})
		},
	}
	upCmd.Flags().BoolVar(&recreate, "recreate", false, "Delete conflicting runtime state before creating the lab")
	upCmd.Flags().BoolVar(&reseed, "reseed", false, "Re-apply vtysh seed commands and reset generated frr.conf")
	root.AddCommand(upCmd)

	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Remove containers and runtime networking for a lab",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if topologyPath == "" {
				return fmt.Errorf("down requires --file")
			}
			return service.Down(cmd.Context(), topologyPath, labruntime.DownOptions{
				Purge: purge,
			})
		},
	}
	downCmd.Flags().BoolVar(&purge, "purge", false, "Delete persisted router config in addition to runtime state")
	root.AddCommand(downCmd)

	consoleCmd := &cobra.Command{
		Use:   "console <router>",
		Short: "Open vtysh or a shell inside a running router container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return service.Console(cmd.Context(), topologyPath, args[0], labruntime.ConsoleOptions{
				Shell: shell,
			})
		},
	}
	consoleCmd.Flags().BoolVar(&shell, "shell", false, "Open /bin/sh instead of vtysh")
	root.AddCommand(consoleCmd)

	return root
}
