package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Set at build time via -ldflags "-X github.com/bennv14/google_service_cli/cmd.version=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func versionString() string {
	return fmt.Sprintf("gsvc %s (commit %s, built %s)", version, commit, date)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
		},
	}
}
