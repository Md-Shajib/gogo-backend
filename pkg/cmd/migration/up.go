package migration

import (
	"log/slog"

	"github.com/md-shajib/gogo-backend/internal/database"
	"github.com/md-shajib/gogo-backend/migrations"
	"github.com/md-shajib/gogo-backend/pkg/config"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Run all pending migrations",
	RunE:  runUp,
}

func runUp(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrations.Run(db, "up"); err != nil {
		return err
	}

	slog.Info("all migrations applied successfully")
	return nil
}
