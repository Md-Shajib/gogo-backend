package main

import "github.com/md-shajib/gogo-backend/pkg/cmd"

// main is the entry point of the application.
// It calls the Execute function from the cmd package to start the CLI application.
func main() {
	// Execute the root command which will:
	// 1. Parse command line arguments
	// 2. Load configuration from flags
	// 3. Execute the appropriate subcommand (e.g., serve, migration)
	cmd.Execute()
}