package cmd

import (
	"log"

	"github.com/md-shajib/gogo-backend/internal/database"
	"github.com/md-shajib/gogo-backend/internal/migration"
	"github.com/spf13/cobra"
)


var migrateCmd = &cobra.Command{
	Use:   "migration [up|down]",
	Short: "Run Database Migrations",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		direction := args[0]

		db := database.NewPostgresDb()
		defer db.Close()

		runner := migration.NewRunner(db)
		if err := runner.Run(direction); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}

		log.Println("Migration executed successfully")
	},
}

func init(){
	rootCmd.AddCommand(migrateCmd)
}