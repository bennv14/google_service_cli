package cmd

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/bennv14/google_service_cli/internal/auth"
	"github.com/bennv14/google_service_cli/internal/config"
	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
)

func TestAuthStatusReportsProfile(t *testing.T) {
	var buf bytes.Buffer
	deps := &service.Deps{
		Profile: config.Profile{Name: "default", AuthType: "oauth"},
		Tokens:  auth.NewFileTokenStore(t.TempDir()),
		Out:     output.NewWriter("table", &buf),
	}
	cmd := newAuthCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("default")) || !bytes.Contains([]byte(out), []byte("oauth")) {
		t.Fatalf("status output = %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("not logged in")) {
		t.Fatalf("expected 'not logged in' when no token present, got %q", out)
	}
}

func TestAuthStatusWithValidToken(t *testing.T) {
	var buf bytes.Buffer
	ts := auth.NewFileTokenStore(t.TempDir())
	tok := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(time.Hour),
	}
	if err := ts.Save("default", tok); err != nil {
		t.Fatal(err)
	}

	deps := &service.Deps{
		Profile: config.Profile{Name: "default", AuthType: "oauth"},
		Tokens:  ts,
		Out:     output.NewWriter("table", &buf),
	}
	cmd := newAuthCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("logged in (token valid until")) {
		t.Fatalf("expected valid token status, got %q", out)
	}
}

func TestAuthStatusWithExpiredToken(t *testing.T) {
	var buf bytes.Buffer
	ts := auth.NewFileTokenStore(t.TempDir())
	tok := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(-time.Hour),
	}
	if err := ts.Save("default", tok); err != nil {
		t.Fatal(err)
	}

	deps := &service.Deps{
		Profile: config.Profile{Name: "default", AuthType: "oauth"},
		Tokens:  ts,
		Out:     output.NewWriter("table", &buf),
	}
	cmd := newAuthCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("logged in (token expired; will refresh on next use)")) {
		t.Fatalf("expected expired token status, got %q", out)
	}
}

func TestAuthStatusServiceAccount(t *testing.T) {
	var buf bytes.Buffer
	keyPath := filepath.Join(t.TempDir(), "sa.json")
	deps := &service.Deps{
		Profile: config.Profile{Name: "sa-prof", AuthType: "service_account", KeyPath: keyPath},
		Tokens:  auth.NewFileTokenStore(t.TempDir()),
		Out:     output.NewWriter("table", &buf),
	}
	cmd := newAuthCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("service account ("+keyPath+")")) {
		t.Fatalf("expected service account status, got %q", out)
	}
}

func TestAuthLogout(t *testing.T) {
	var buf bytes.Buffer
	ts := auth.NewFileTokenStore(t.TempDir())
	tok := &oauth2.Token{
		AccessToken: "test-token",
		Expiry:      time.Now().Add(time.Hour),
	}
	if err := ts.Save("default", tok); err != nil {
		t.Fatal(err)
	}

	deps := &service.Deps{
		Profile: config.Profile{Name: "default", AuthType: "oauth"},
		Tokens:  ts,
		Out:     output.NewWriter("table", &buf),
	}

	cmd := newAuthCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Logged out profile \"default\".")) {
		t.Fatalf("unexpected logout output: %q", buf.String())
	}

	if _, err := ts.Load("default"); err == nil {
		t.Fatal("expected token to be deleted")
	}
}

func TestAuthRequireProfile(t *testing.T) {
	var buf bytes.Buffer
	deps := &service.Deps{
		Profile: config.Profile{}, // empty profile name
		Tokens:  auth.NewFileTokenStore(t.TempDir()),
		Out:     output.NewWriter("table", &buf),
	}

	subcommands := []string{"status", "login", "logout"}
	for _, sub := range subcommands {
		cmd := newAuthCmd(deps)
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{sub})
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for 'auth %s' with no profile selected", sub)
		}
	}
}

func TestLoginRequestsEveryServicesScopes(t *testing.T) {
	scopes := serviceScopes(services)
	for _, want := range []string{
		"https://www.googleapis.com/auth/drive.readonly",
		"https://www.googleapis.com/auth/drive.file",
		"https://www.googleapis.com/auth/chat.spaces.readonly",
		"https://www.googleapis.com/auth/chat.messages.readonly",
		"https://www.googleapis.com/auth/chat.users.readstate.readonly",
	} {
		found := false
		for _, s := range scopes {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("auth login would not request %q; got %v", want, scopes)
		}
	}
}
