package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gsvc",
	Short: "A CLI for interacting with Google services",
	Long: `gsvc is a command line tool for interacting with
various Google services (Drive, Sheets, Gmail, ...).`,
}

// Execute runs the root command and is the entry point for the CLI.
func Execute() {
	rootCmd.AddCommand(newVersionCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
