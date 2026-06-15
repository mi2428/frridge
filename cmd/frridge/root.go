package main

import (
	"fmt"
	"strings"

	"frridge/internal/buildinfo"
	labruntime "frridge/internal/runtime"

	"github.com/spf13/cobra"
)

func newRootCommand(service labruntime.Service) *cobra.Command {
	var topologyPath string
	var recreate bool
	var reseed bool
	var purge bool
	var shell bool

	root := &cobra.Command{
		Use:           "frridge",
		Short:         "FRR lab runner for container-based network study",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{printf \"%s %s\\n\" .Name .Version}}")

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

	pingCmd := &cobra.Command{
		Use:   "ping [check]",
		Short: "Run YAML-defined ping checks and print the raw ping output",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if topologyPath == "" {
				return fmt.Errorf("ping requires --file")
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			results, err := service.Ping(cmd.Context(), topologyPath, name)
			if err != nil {
				return err
			}

			failures := 0
			for i, result := range results {
				if len(results) > 1 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "== %s (%s -> %s) ==\n", result.Name, pingSource(result), result.Target)
				}
				if result.Output != "" {
					_, _ = fmt.Fprint(cmd.OutOrStdout(), result.Output)
					if !strings.HasSuffix(result.Output, "\n") {
						_, _ = fmt.Fprintln(cmd.OutOrStdout())
					}
				}
				if len(results) > 1 && i < len(results)-1 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout())
				}
				if result.ExitCode != 0 {
					failures++
				}
			}
			if failures > 0 {
				return fmt.Errorf("%d ping check(s) failed", failures)
			}
			return nil
		},
	}
	root.AddCommand(pingCmd)

	return root
}

func pingSource(result labruntime.PingResult) string {
	if result.Namespace == "" {
		return result.Router
	}
	return result.Router + "/" + result.Namespace
}
