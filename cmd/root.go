package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "google_service_cli",
	Short: "A CLI for interacting with Google services",
	Long: `google_service_cli is a command line tool for interacting with
various Google services (Drive, Sheets, Gmail, ...).`,
}

// Execute runs the root command and is the entry point for the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
