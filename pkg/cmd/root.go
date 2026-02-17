package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile 					string
	prettyPrintLog, verbose		bool
	rootCmd = &cobra.Command{
		Use:   "gogo",
		Short: "Gogo Backend CLI",
	}
)

func Execute() {
	err := rootCmd.Execute();
	if err != nil {
		os.Exit(1)
	}
}