package cmd

import (
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/spf13/cobra"
)

func newDockerCmd() *cobra.Command {
	dockerCmd := &cobra.Command{
		Use:   "docker",
		Short: "Manage Docker containers",
		Long:  "List, restart, stop, view logs, and show stats for Docker containers.",
	}

	dockerCmd.AddCommand(
		newDockerListCmd(),
		newDockerRestartCmd(),
		newDockerStopCmd(),
		newDockerLogsCmd(),
		newDockerStatsCmd(),
		newDockerTopCmd(),
		newDockerInspectCmd(),
	)

	return dockerCmd
}

func newDockerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List running containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			containers, err := docker.List()
			if err != nil {
				return err
			}
			return output(containers, jsonOutput)
		},
	}
}

func newDockerRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <container>",
		Short: "Restart a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := docker.Restart(args[0])
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}

func newDockerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <container>",
		Short: "Stop a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := docker.Stop(args[0])
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}

func newDockerLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <container> [lines]",
		Short: "Show container logs (default: 50 lines)",
		Long:  "Show container logs. Optionally specify number of lines (default: 50).",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			lines := "50"
			if len(args) >= 2 {
				lines = args[1]
			}
			result, err := docker.Logs(args[0], lines)
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}

func newDockerStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show resource usage for all running containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			stats, err := docker.Stats()
			if err != nil {
				return err
			}
			return output(stats, jsonOutput)
		},
	}
}

func newDockerTopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "top <container>",
		Short: "Show the processes running inside a container",
		Long:  "Show the processes running inside a container, read from the host via docker top. Read-only: no exec, no TTY.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := docker.Top(args[0])
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}

func newDockerInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <container>",
		Short: "Show a readable summary of a container's configuration and state",
		Long:  "Show a readable summary of a container's image, state, restart policy, ports, mounts, networks, and health. Environment variable values are never included.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := docker.Inspect(args[0])
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}
