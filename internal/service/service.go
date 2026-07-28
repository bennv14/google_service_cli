// Package service defines the contract every Google service command implements.
package service

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/auth"
	"github.com/bennv14/google_service_cli/internal/config"
	"github.com/bennv14/google_service_cli/internal/output"
)

// Deps is the shared runtime context, populated by cmd/root.go in
// PersistentPreRunE and shared by pointer with every command.
// Most services should rely only on Config, Out, and NewClient; a command
// with a different natural default output format may use NewOut/OutputExplicit
// to build its own writer instead of using Out directly.
type Deps struct {
	Config    config.Store
	Profile   config.Profile
	Tokens    auth.TokenStore
	Out       output.Writer
	NewClient func(ctx context.Context, scopes ...string) (*http.Client, error)

	// Scopes is the union of every registered service's OAuth scopes,
	// used by `gsvc auth login`.
	Scopes []string

	// OutputFormat is the resolved --output value; OutputExplicit reports
	// whether the user set it. A command with a different natural default
	// builds its own writer: if !OutputExplicit { out = NewOut("text") }.
	OutputFormat   string
	OutputExplicit bool
	NewOut         func(format string) output.Writer
}

// Service is one Google service's command subtree.
type Service interface {
	Name() string
	// Scopes are the OAuth scopes this service needs; `gsvc auth login`
	// requests the union across all registered services.
	Scopes() []string
	Command(d *Deps) *cobra.Command
}
