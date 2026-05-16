package migration

import "github.com/spf13/cobra"

// MigrateCmd is the parent command for all migration subcommands.
// It does nothing on its own — use "up" or "down".
var MigrateCmd = &cobra.Command{
	Use:   "migration",
	Short: "Manage database migrations",
}

func init() {
	MigrateCmd.AddCommand(upCmd)
	MigrateCmd.AddCommand(downCmd)
}
