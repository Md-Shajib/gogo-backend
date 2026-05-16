package cmd

import (
	"fmt"
	"os"

	"github.com/md-shajib/gogo-backend/pkg/cmd/migration"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gogo-backend",
	Short: "Single product e-commerce backend",
}

// Execute is the entry point called by main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migration.MigrateCmd)
}
