package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/bennv/google_service_cli/internal/config"
)

type oauthProvider struct {
	profile string
	cfgData []byte
	tokens  TokenStore
	// codeFn obtains an authorization code; overridable in tests.
	codeFn func(ctx context.Context, conf *oauth2.Config) (string, error)
}

func newOAuthProvider(p config.Profile, tokens TokenStore) (*oauthProvider, error) {
	if p.ClientPath == "" {
		return nil, errors.New("oauth profile requires client_path")
	}
	b, err := os.ReadFile(expandPath(p.ClientPath))
	if err != nil {
		return nil, fmt.Errorf("read oauth client: %w", err)
	}
	return &oauthProvider{profile: p.Name, cfgData: b, tokens: tokens, codeFn: loopbackCode}, nil
}

func (o *oauthProvider) Kind() string { return "oauth" }

func (o *oauthProvider) oauthConfig(scopes []string) (*oauth2.Config, error) {
	return google.ConfigFromJSON(o.cfgData, scopes...)
}

func (o *oauthProvider) TokenSource(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
	conf, err := o.oauthConfig(scopes)
	if err != nil {
		return nil, err
	}
	tok, err := o.tokens.Load(o.profile)
	if err != nil {
		return nil, fmt.Errorf("%w: run 'gsvc auth login'", ErrNotAuthenticated)
	}
	base := oauth2.ReuseTokenSource(tok, conf.TokenSource(ctx, tok))
	return &savingTokenSource{src: base, last: tok, save: func(t *oauth2.Token) error {
		return o.tokens.Save(o.profile, t)
	}}, nil
}

func (o *oauthProvider) Login(ctx context.Context, scopes []string) error {
	conf, err := o.oauthConfig(scopes)
	if err != nil {
		return err
	}
	code, err := o.codeFn(ctx, conf) // codeFn sets conf.RedirectURL
	if err != nil {
		return err
	}
	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}
	return o.tokens.Save(o.profile, tok)
}

// savingTokenSource persists the token whenever it changes (e.g. after refresh).
type savingTokenSource struct {
	mu   sync.Mutex
	src  oauth2.TokenSource
	save func(*oauth2.Token) error
	last *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.last == nil || t.AccessToken != s.last.AccessToken {
		if err := s.save(t); err == nil {
			s.last = t
		}
	}
	return t, nil
}

// loopbackCode runs the installed-app flow: it starts a localhost server,
// opens the browser to the consent screen, and waits for the redirect.
func loopbackCode(ctx context.Context, conf *oauth2.Config) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	conf.RedirectURL = fmt.Sprintf("http://%s/callback", ln.Addr().String())

	state := randomState()
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errCh <- errors.New("state mismatch"):
			default:
			}
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, e, http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("authorization error: %s", e):
			default:
			}
			return
		}
		fmt.Fprintln(w, "Login successful. You can close this tab and return to the terminal.")
		select {
		case codeCh <- q.Get("code"):
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Open this URL to authorize (browser should open automatically):\n%s\n", authURL)
	_ = openBrowser(authURL)

	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var openBrowser = defaultOpenBrowser

func defaultOpenBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}

