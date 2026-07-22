package auth

import (
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/bennv/google_service_cli/internal/config"
)

func TestFileTokenStoreRoundTrip(t *testing.T) {
	ts := NewFileTokenStore(t.TempDir())
	want := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	if err := ts.Save("default", want); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "a" || got.RefreshToken != "r" {
		t.Fatalf("Load() = %+v", got)
	}
	if err := ts.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Load("default"); err == nil {
		t.Fatal("Load after Delete should error")
	}
}

func TestServiceAccountProviderRequiresKey(t *testing.T) {
	// A service_account profile without key_path must error at construction.
	if _, err := newServiceAccountProvider(config.Profile{Name: "x", AuthType: "service_account"}); err == nil {
		t.Fatal("service_account without key_path should error")
	}
}
