package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the current build version of the CLI.
const version = "0.1.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("google_service_cli v%s\n", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
