package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
	vercheck "github.com/bennv14/google_service_cli/internal/version"
)

// Set at build time via -ldflags "-X github.com/bennv14/google_service_cli/cmd.version=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const envNoUpdateCheck = "GSVC_NO_UPDATE_CHECK"

// Package-level hooks for dependency injection in tests.
var (
	newVersionChecker = func(currentVersion string) vercheck.Checker {
		return vercheck.NewGitHubChecker(currentVersion)
	}
	resolveExecutablePath = func() string {
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return resolved
		}
		return exe
	}
)

func versionString() string {
	return vercheck.NewInfo(version, commit, date, false).SummaryLine()
}

func skipUpdateCheckEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envNoUpdateCheck))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func newVersionCmd(d *service.Deps) *cobra.Command {
	var noCheck bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number and check for updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			homebrew := vercheck.IsHomebrewInstall(resolveExecutablePath())
			info := vercheck.NewInfo(version, commit, date, homebrew)

			if !noCheck && !skipUpdateCheckEnv() {
				baseCtx := cmd.Context()
				if baseCtx == nil {
					baseCtx = context.Background()
				}
				ctx, cancel := context.WithTimeout(baseCtx, vercheck.DefaultCheckTimeout)
				defer cancel()

				rel, err := newVersionChecker(version).Latest(ctx)
				if err != nil {
					info.Status = vercheck.StatusCheckFailed
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: update check failed: %v\n", err)
				} else {
					info.ApplyRelease(rel)
				}
			}

			var out output.Writer
			switch {
			case d != nil && d.OutputExplicit && d.Out != nil:
				out = d.Out
			case d != nil && d.NewOut != nil:
				out = d.NewOut("text")
			default:
				out = output.NewWriter("text", cmd.OutOrStdout())
			}
			return out.Render(info)
		},
	}

	cmd.Flags().BoolVar(&noCheck, "no-check", false, "skip checking GitHub for a newer release")
	return cmd
}
