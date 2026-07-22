// Package gclient builds authenticated *http.Client values from a profile.
package gclient

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/bennv/google_service_cli/internal/auth"
	"github.com/bennv/google_service_cli/internal/config"
)

// ProviderFactory constructs an auth.Provider; production passes auth.NewProvider.
type ProviderFactory func(config.Profile, auth.TokenStore) (auth.Provider, error)

// NewClientFunc returns a lazy factory that authenticates only when invoked.
func NewClientFunc(p config.Profile, tokens auth.TokenStore, newProvider ProviderFactory) func(context.Context, ...string) (*http.Client, error) {
	return func(ctx context.Context, scopes ...string) (*http.Client, error) {
		prov, err := newProvider(p, tokens)
		if err != nil {
			return nil, err
		}
		ts, err := prov.TokenSource(ctx, scopes...)
		if err != nil {
			return nil, err
		}
		return oauth2.NewClient(ctx, ts), nil
	}
}
