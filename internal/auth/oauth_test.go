package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/bennv14/google_service_cli/internal/config"
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

type mockTokenSource struct {
	tok *oauth2.Token
	err error
}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	return m.tok, m.err
}

func TestSavingTokenSourceConcurrent(t *testing.T) {
	tok := &oauth2.Token{AccessToken: "token1"}
	var mu sync.Mutex
	savedCount := 0
	src := &savingTokenSource{
		src: &mockTokenSource{tok: tok},
		save: func(t *oauth2.Token) error {
			mu.Lock()
			savedCount++
			mu.Unlock()
			return nil
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := src.Token()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if savedCount == 0 {
		t.Fatal("expected save to be called at least once")
	}
}

func TestSavingTokenSourceSaveError(t *testing.T) {
	tok := &oauth2.Token{AccessToken: "token1"}
	saveCalls := 0
	saveErr := errors.New("save failed")
	src := &savingTokenSource{
		src: &mockTokenSource{tok: tok},
		save: func(t *oauth2.Token) error {
			saveCalls++
			return saveErr
		},
	}

	// First call: save returns error, so s.last must NOT be updated.
	got, err := src.Token()
	if err != nil {
		t.Fatalf("Token() error = %v, want nil", err)
	}
	if got.AccessToken != "token1" {
		t.Fatalf("got token %v, want token1", got.AccessToken)
	}
	if saveCalls != 1 {
		t.Fatalf("saveCalls = %d, want 1", saveCalls)
	}

	// Second call: s.last is still nil, so save will be attempted again.
	_, _ = src.Token()
	if saveCalls != 2 {
		t.Fatalf("saveCalls = %d, want 2", saveCalls)
	}
}

func TestLoopbackCodePathCheckAndNonBlocking(t *testing.T) {
	oldOpen := openBrowser
	defer func() { openBrowser = oldOpen }()

	openBrowser = func(authURLStr string) error {
		go func() {
			parsed, err := url.Parse(authURLStr)
			if err != nil {
				t.Errorf("failed to parse authURL: %v", err)
				return
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")

			parsedRedirect, err := url.Parse(redirectURI)
			if err != nil {
				t.Errorf("failed to parse redirectURI: %v", err)
				return
			}

			// 1. GET /favicon.ico -> expect 404 Not Found
			favURL := *parsedRedirect
			favURL.Path = "/favicon.ico"
			resp, err := http.Get(favURL.String())
			if err != nil {
				t.Errorf("GET favicon error: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET favicon status = %d, want 404", resp.StatusCode)
			}

			// 2. GET /callback -> expect 200 OK
			cbURL := *parsedRedirect
			q := cbURL.Query()
			q.Set("state", state)
			q.Set("code", "my-secret-code")
			cbURL.RawQuery = q.Encode()

			resp, err = http.Get(cbURL.String())
			if err != nil {
				t.Errorf("GET callback error: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET callback status = %d, want 200", resp.StatusCode)
			}

			// 3. Duplicate GET /callback -> non-blocking channel send handles it
			resp, err = http.Get(cbURL.String())
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conf := &oauth2.Config{
		Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/auth"},
	}
	code, err := loopbackCode(ctx, conf)
	if err != nil {
		t.Fatalf("loopbackCode error = %v", err)
	}
	if code != "my-secret-code" {
		t.Fatalf("loopbackCode code = %q, want 'my-secret-code'", code)
	}
}

