package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"

	"github.com/bennv/google_service_cli/internal/config"
)

// writeInstalledClient writes a minimal "installed app" client secret file and
// returns its path.
func writeInstalledClient(t *testing.T, dir string) string {
	t.Helper()
	const client = `{"installed":{"client_id":"cid","client_secret":"secret",` +
		`"auth_uri":"https://accounts.google.com/o/oauth2/auth",` +
		`"token_uri":"https://oauth2.googleapis.com/token",` +
		`"redirect_uris":["http://localhost"]}}`
	p := filepath.Join(dir, "client.json")
	if err := os.WriteFile(p, []byte(client), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewProviderSelectsServiceAccount(t *testing.T) {
	ts := NewFileTokenStore(t.TempDir())
	// service_account without key_path must error.
	if _, err := NewProvider(config.Profile{Name: "x", AuthType: "service_account"}, ts); err == nil {
		t.Fatal("service_account without key_path should error")
	}
}

func TestNewProviderUnknownType(t *testing.T) {
	ts := NewFileTokenStore(t.TempDir())
	if _, err := NewProvider(config.Profile{Name: "x", AuthType: "nope"}, ts); err == nil {
		t.Fatal("unknown auth_type should error")
	}
}

func TestOAuthTokenSourceWithoutTokenIsNotAuthenticated(t *testing.T) {
	dir := t.TempDir()
	clientPath := writeInstalledClient(t, dir)
	ts := NewFileTokenStore(dir)
	p, err := NewProvider(config.Profile{Name: "default", AuthType: "oauth", ClientPath: clientPath}, ts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.TokenSource(context.Background(), "scope"); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("TokenSource err = %v, want ErrNotAuthenticated", err)
	}
}

func TestOAuthLoginExchangesAndSavesToken(t *testing.T) {
	// Fake Google token endpoint.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	dir := t.TempDir()
	clientPath := writeInstalledClient(t, dir)
	store := NewFileTokenStore(dir)
	prov, err := NewProvider(config.Profile{Name: "default", AuthType: "oauth", ClientPath: clientPath}, store)
	if err != nil {
		t.Fatal(err)
	}
	op := prov.(*oauthProvider)
	// Bypass the browser: point token URL at our fake server and return a fixed code.
	op.codeFn = func(ctx context.Context, conf *oauth2.Config) (string, error) {
		conf.Endpoint.TokenURL = tokenSrv.URL
		conf.RedirectURL = "http://127.0.0.1/callback"
		return "the-code", nil
	}

	in, ok := prov.(Interactive)
	if !ok {
		t.Fatal("oauth provider must implement Interactive")
	}
	if err := in.Login(context.Background(), []string{"scope"}); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if saved.AccessToken != "AT" || saved.RefreshToken != "RT" {
		t.Fatalf("saved token = %+v", saved)
	}
}
