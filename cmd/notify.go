package cmd

import "github.com/spf13/cobra"

func newNotifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Notification helpers",
		Long: `Notification helpers for testing configured delivery providers.

Use this command to verify that your Telegram, Slack, Discord, or webhook
configuration can actually send messages before relying on alerts or watch
notifications.`,
	}

	cmd.AddCommand(newNotifyTestCmd())
	return cmd
}

func newNotifyTestCmd() *cobra.Command {
	cmd := newAlertsTestNotifyCmd()
	cmd.Use = "test"
	cmd.Short = "Send a test notification to configured providers"
	cmd.Long = `Send a test notification using the configured notify providers.

This is the user-friendly entry point for checking notification delivery.
The legacy command 'homebutler alerts test-notify' still works for backward
compatibility.`
	return cmd
}
