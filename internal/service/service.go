// Package service defines the contract every Google service command implements.
package service

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/bennv/google_service_cli/internal/auth"
	"github.com/bennv/google_service_cli/internal/config"
	"github.com/bennv/google_service_cli/internal/output"
)

// Deps is the shared runtime context, populated by cmd/root.go in
// PersistentPreRunE and shared by pointer with every command.
// Services should rely only on Config, Out, and NewClient.
type Deps struct {
	Config    config.Store
	Profile   config.Profile
	Tokens    auth.TokenStore
	Out       output.Writer
	NewClient func(ctx context.Context, scopes ...string) (*http.Client, error)

	// Scopes is the union of every registered service's OAuth scopes,
	// used by `gsvc auth login`.
	Scopes []string
}

// Service is one Google service's command subtree.
type Service interface {
	Name() string
	// Scopes are the OAuth scopes this service needs; `gsvc auth login`
	// requests the union across all registered services.
	Scopes() []string
	Command(d *Deps) *cobra.Command
}
