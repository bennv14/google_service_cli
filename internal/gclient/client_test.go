package gclient

import (
	"context"
	"testing"

	"golang.org/x/oauth2"

	"github.com/bennv14/google_service_cli/internal/auth"
	"github.com/bennv14/google_service_cli/internal/config"
)

type fakeProvider struct{}

func (fakeProvider) Kind() string { return "fake" }
func (fakeProvider) TokenSource(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "AT"}), nil
}

func TestNewClientFuncBuildsClient(t *testing.T) {
	factory := func(config.Profile, auth.TokenStore) (auth.Provider, error) {
		return fakeProvider{}, nil
	}
	nc := NewClientFunc(config.Profile{Name: "default"}, nil, factory)
	hc, err := nc(context.Background(), "scope")
	if err != nil {
		t.Fatal(err)
	}
	if hc == nil {
		t.Fatal("expected non-nil *http.Client")
	}
}
