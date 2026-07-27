package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bennv/google_service_cli/internal/auth"
	"github.com/bennv/google_service_cli/internal/config"
	"github.com/bennv/google_service_cli/internal/gclient"
	"github.com/bennv/google_service_cli/internal/output"
	"github.com/bennv/google_service_cli/internal/service"
	"github.com/bennv/google_service_cli/internal/service/drive"
)

// services is the service registry. Adding a service here wires up both its
// command subtree and its OAuth scopes.
var services = []service.Service{drive.New()}

// serviceScopes returns the deduplicated, sorted union of every service's scopes.
func serviceScopes(svcs []service.Service) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range svcs {
		for _, sc := range s.Scopes() {
			if !seen[sc] {
				seen[sc] = true
				out = append(out, sc)
			}
		}
	}
	sort.Strings(out)
	return out
}

// buildRootCmd assembles the root command, its global flags, the shared deps,
// and every subcommand. It is used by Execute and by tests.
func buildRootCmd() *cobra.Command {
	root, _ := buildRootCmdWithDeps()
	return root
}

// buildRootCmdWithDeps is buildRootCmd but also returns the *service.Deps
// instance the command populates in PersistentPreRunE, so tests can observe
// the result of real flag parsing (e.g. OutputFormat/OutputExplicit).
func buildRootCmdWithDeps() (*cobra.Command, *service.Deps) {
	var (
		profileFlag string
		outputFlag  string
		verboseFlag bool
	)
	deps := &service.Deps{}

	root := &cobra.Command{
		Use:   "gsvc",
		Short: "A CLI for interacting with Google services",
		Long: `gsvc is a command line tool for interacting with
various Google services (Drive, Sheets, Gmail, ...).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			explicit := cmd.Root().PersistentFlags().Changed("output")
			return populateDeps(deps, profileFlag, outputFlag, explicit, cmd.OutOrStdout())
		},
	}

	root.PersistentFlags().StringVar(&profileFlag, "profile", "", "profile to use (default: active profile)")
	root.PersistentFlags().StringVarP(&outputFlag, "output", "o", "table", "output format: table | json | text")
	root.PersistentFlags().BoolVar(&verboseFlag, "verbose", false, "verbose error output")

	// Shared commands.
	root.AddCommand(newVersionCmd())
	root.AddCommand(newAuthCmd(deps))
	root.AddCommand(newConfigCmd(deps))

	// Registry: each service contributes its own subtree. Add new services here.
	for _, s := range services {
		root.AddCommand(s.Command(deps))
	}
	return root, deps
}

// populateDeps fills the shared deps from flags and on-disk config.
// Missing/unauthenticated config is tolerated: NewClient returns a helpful
// error only when a command actually needs API access.
func populateDeps(deps *service.Deps, profileFlag, outputFlag string, outputExplicit bool, out io.Writer) error {
	dir, err := config.DefaultDir()
	if err != nil {
		return err
	}
	store, err := config.NewStore(dir)
	if err != nil {
		return err
	}
	tokens := auth.NewFileTokenStore(filepath.Join(dir, "tokens"))

	deps.Config = store
	deps.Tokens = tokens
	deps.Scopes = serviceScopes(services)
	deps.OutputFormat = outputFlag
	deps.OutputExplicit = outputExplicit
	deps.NewOut = func(format string) output.Writer { return output.NewWriter(format, out) }
	deps.Out = deps.NewOut(outputFlag)

	prof, perr := selectedProfile(store, profileFlag)
	if perr == nil {
		deps.Profile = prof
		deps.NewClient = gclient.NewClientFunc(prof, tokens, auth.NewProvider)
	} else {
		deps.NewClient = func(context.Context, ...string) (*http.Client, error) {
			return nil, fmt.Errorf("%w: run 'gsvc config add' then 'gsvc auth login'", auth.ErrNotAuthenticated)
		}
	}
	return nil
}

func selectedProfile(store config.Store, profileFlag string) (config.Profile, error) {
	if profileFlag != "" {
		return store.Get(profileFlag)
	}
	return store.Active()
}

// Execute runs the root command and is the entry point for the CLI.
func Execute() {
	root := buildRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
