// Package auth abstracts Google authentication behind a Provider interface.
package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

// ErrNotAuthenticated indicates the active profile has no usable token.
var ErrNotAuthenticated = errors.New("not authenticated")

// Provider mints token sources for a profile.
type Provider interface {
	TokenSource(ctx context.Context, scopes ...string) (oauth2.TokenSource, error)
	Kind() string // "oauth" | "service_account"
}

// Interactive is implemented by providers that run a login flow (OAuth).
type Interactive interface {
	Login(ctx context.Context, scopes []string) error
}

// NewProvider (the factory) lives in factory.go (Task 5), added once both
// providers exist — this keeps provider.go free of any forward reference.

// expandPath expands a leading ~/ to the user's home directory.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
