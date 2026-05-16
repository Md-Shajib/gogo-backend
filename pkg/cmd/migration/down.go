package migration

import (
	"log/slog"

	"github.com/md-shajib/gogo-backend/internal/database"
	"github.com/md-shajib/gogo-backend/migrations"
	"github.com/md-shajib/gogo-backend/pkg/config"
	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back the last migration",
	RunE:  runDown,
}

func runDown(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrations.Run(db, "down"); err != nil {
		return err
	}

	slog.Info("migration rolled back successfully")
	return nil
}
