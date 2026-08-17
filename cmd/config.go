package cmd

import (
	"fmt"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect homebutler configuration",
	}
	cmd.AddCommand(newConfigValidateCmd())
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the config file and report what homebutler makes of it",
		Long: `Validate the config file without starting a server, watcher, install, or
remote connection.

Reports which file was used and which rule selected it, what homebutler read
from each section, and anything that is wrong or silently ignored — including
keys that do not match the schema, which are otherwise dropped without a word.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Deliberately not loadConfig(): it treats an unreadable or
			// missing file as "use defaults", which is exactly the failure
			// this command exists to surface. Validate resolves the path
			// itself. For the same reason there is no remote routing —
			// the file being checked is the local one.
			result := config.Validate(cfgPath)

			if jsonOutput {
				if err := output(result, true); err != nil {
					return err
				}
			} else {
				fmt.Print(config.FormatValidation(result))
			}

			if errs := result.Errors(); errs > 0 {
				return fmt.Errorf("config has %d error(s)", errs)
			}
			if strict && result.Warnings() > 0 {
				return fmt.Errorf("config has %d warning(s)", result.Warnings())
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero when warnings are found, not just errors")

	return cmd
}
