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
