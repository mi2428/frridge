package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"frridge/internal/config"
	"frridge/internal/multipass"

	"github.com/spf13/cobra"
)

func newRootCommand(service multipass.Service) *cobra.Command {
	var req multipass.Request
	var topologyPath string
	var recreate bool
	var reseed bool
	var purge bool
	var shell bool

	root := &cobra.Command{
		Use:           "frridge-mp",
		Short:         "Run frridge labs inside a Multipass Linux VM",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := root.PersistentFlags()
	flags.StringVar(&req.Instance.Name, "instance", "", "Multipass instance name")
	flags.StringVar(&req.Instance.Image, "image", "", "Ubuntu image used when the instance is first launched")
	flags.IntVar(&req.Instance.CPUs, "cpus", 0, "CPU count used when the instance is first launched")
	flags.StringVar(&req.Instance.Memory, "memory", "", "Memory size used when the instance is first launched")
	flags.StringVar(&req.Instance.Disk, "disk", "", "Disk size used when the instance is first launched")
	flags.StringVar(&req.RepoDir, "repo-dir", "", "Path to the frridge source tree used to build the guest binary")
	flags.StringVar(&req.HostDir, "host-dir", "", "Host directory mounted into the guest for topology files and lab assets")

	ensureCmd := &cobra.Command{
		Use:   "ensure",
		Short: "Create or update the Multipass VM and guest-local frridge binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := service.Ensure(cmd.Context(), req)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "instance: %s\nworkspace: %s\nbinary: %s\nworkdir: %s\n",
				env.InstanceName,
				env.GuestHostDir,
				env.GuestBinary,
				env.GuestWorkDir,
			)
			return nil
		},
	}
	root.AddCommand(ensureCmd)

	shellCmd := &cobra.Command{
		Use:   "shell",
		Short: "Open a shell inside the guest at the mounted host workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return service.Shell(cmd.Context(), req)
		},
	}
	root.AddCommand(shellCmd)

	execCmd := &cobra.Command{
		Use:   "exec -- <command> [args...]",
		Short: "Run an arbitrary command inside the guest workspace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return service.Exec(cmd.Context(), req, args)
		},
	}
	root.AddCommand(execCmd)

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Create containers, links, and initial router state inside the guest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(topologyPath) == "" {
				return fmt.Errorf("up requires --file")
			}

			nextReq, guestFile, err := resolveTopology(cmd, req, topologyPath)
			if err != nil {
				return err
			}

			command := []string{"--file", guestFile, "up"}
			if recreate {
				command = append(command, "--recreate")
			}
			if reseed {
				command = append(command, "--reseed")
			}
			return service.Frridge(cmd.Context(), nextReq, command)
		},
	}
	upCmd.Flags().StringVarP(&topologyPath, "file", "f", "", "Path to lab YAML below --host-dir")
	upCmd.Flags().BoolVar(&recreate, "recreate", false, "Delete conflicting runtime state before creating the lab")
	upCmd.Flags().BoolVar(&reseed, "reseed", false, "Re-apply vtysh seed commands and reset generated frr.conf")
	root.AddCommand(upCmd)

	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Remove containers and runtime networking inside the guest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(topologyPath) == "" {
				return fmt.Errorf("down requires --file")
			}

			nextReq, guestFile, err := resolveTopology(cmd, req, topologyPath)
			if err != nil {
				return err
			}

			command := []string{"--file", guestFile, "down"}
			if purge {
				command = append(command, "--purge")
			}
			return service.Frridge(cmd.Context(), nextReq, command)
		},
	}
	downCmd.Flags().StringVarP(&topologyPath, "file", "f", "", "Path to lab YAML below --host-dir")
	downCmd.Flags().BoolVar(&purge, "purge", false, "Delete persisted router config in addition to runtime state")
	root.AddCommand(downCmd)

	consoleCmd := &cobra.Command{
		Use:   "console <router>",
		Short: "Open vtysh or a shell inside a running guest-backed router container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nextReq, guestFile, err := resolveTopology(cmd, req, topologyPath)
			if err != nil {
				return err
			}

			command := make([]string, 0, 5)
			if guestFile != "" {
				command = append(command, "--file", guestFile)
			}
			command = append(command, "console")
			if shell {
				command = append(command, "--shell")
			}
			command = append(command, args[0])
			return service.Frridge(cmd.Context(), nextReq, command)
		},
	}
	consoleCmd.Flags().StringVarP(&topologyPath, "file", "f", "", "Path to lab YAML below --host-dir")
	consoleCmd.Flags().BoolVar(&shell, "shell", false, "Open /bin/sh instead of vtysh")
	root.AddCommand(consoleCmd)

	return root
}

func resolveTopology(cmd *cobra.Command, req multipass.Request, topologyPath string) (multipass.Request, string, error) {
	topologyPath = strings.TrimSpace(topologyPath)
	if topologyPath == "" {
		return req, "", nil
	}

	absFile, err := filepath.Abs(topologyPath)
	if err != nil {
		return req, "", fmt.Errorf("resolve topology file: %w", err)
	}
	if _, err := os.Stat(absFile); err != nil {
		return req, "", fmt.Errorf("stat topology file: %w", err)
	}

	if !cmd.Flags().Lookup("host-dir").Changed {
		req.HostDir = filepath.Dir(absFile)
	}

	hostDir, err := effectiveHostDir(req.HostDir)
	if err != nil {
		return req, "", err
	}

	rel, err := filepath.Rel(hostDir, absFile)
	if err != nil {
		return req, "", fmt.Errorf("relativize topology file: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return req, "", fmt.Errorf("topology file %q is outside --host-dir %q", absFile, hostDir)
	}
	if err := validateHostDirCoverage(absFile, hostDir); err != nil {
		return req, "", err
	}

	req.HostDir = hostDir
	return req, filepath.ToSlash(rel), nil
}

func effectiveHostDir(hostDir string) (string, error) {
	if strings.TrimSpace(hostDir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		return cwd, nil
	}

	abs, err := filepath.Abs(hostDir)
	if err != nil {
		return "", fmt.Errorf("resolve host dir: %w", err)
	}
	return abs, nil
}

func validateHostDirCoverage(topologyPath, hostDir string) error {
	topology, err := config.LoadFile(topologyPath)
	if err != nil {
		return err
	}

	for _, router := range topology.ResolveRouters() {
		for _, mount := range router.Mounts {
			if err := ensureBelowHostDir(hostDir, mount.Source); err != nil {
				return fmt.Errorf("router %q mount source %q is outside --host-dir %q", router.Name, mount.Source, hostDir)
			}
		}
	}
	return nil
}

func ensureBelowHostDir(hostDir, candidate string) error {
	rel, err := filepath.Rel(hostDir, candidate)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is outside %q", candidate, hostDir)
	}
	return nil
}
