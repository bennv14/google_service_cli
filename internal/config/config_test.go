package config

import (
	"errors"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// No profiles yet: Active is a typed error.
	if _, err := s.Active(); !errors.Is(err, ErrNoActiveProfile) {
		t.Fatalf("Active() err = %v, want ErrNoActiveProfile", err)
	}

	p := Profile{Name: "default", AuthType: "oauth", ClientPath: "/tmp/c.json"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	// First saved profile becomes active automatically.
	got, err := s.Active()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "default" || got.AuthType != "oauth" || got.ClientPath != "/tmp/c.json" {
		t.Fatalf("Active() = %+v", got)
	}

	// Reload from disk in a fresh store to prove persistence.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if list := s2.List(); len(list) != 1 || list[0].Name != "default" {
		t.Fatalf("List() = %+v", list)
	}
	if err := s2.SetActive("missing"); err == nil {
		t.Fatal("SetActive(missing) should error")
	}
}

func TestDefaultDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/here")
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/xdg/here/google_service_cli" {
		t.Fatalf("DefaultDir() = %q", got)
	}
}
