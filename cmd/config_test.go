package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bennv/google_service_cli/internal/config"
	"github.com/bennv/google_service_cli/internal/output"
	"github.com/bennv/google_service_cli/internal/service"
)

func TestConfigRoundTrip(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	deps := &service.Deps{
		Config: store,
		Out:    output.NewWriter("table", &buf),
	}

	// 1. Add oauth profile "work"
	cmd := newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "work", "--auth-type", "oauth", "--client-path", "/tmp/c.json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add work failed: %v", err)
	}
	if !strings.Contains(buf.String(), `Saved profile "work".`) {
		t.Fatalf("add work output = %q", buf.String())
	}

	// 2. Add service account profile "prod"
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "prod", "--auth-type", "service_account", "--key-path", "/tmp/k.json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add prod failed: %v", err)
	}

	// 3. List profiles -> "work" is active (first added)
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "work") || !strings.Contains(out, "prod") {
		t.Fatalf("list output missing profiles: %s", out)
	}

	// 4. Use "prod"
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"use", "prod"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("use prod failed: %v", err)
	}
	if !strings.Contains(buf.String(), `Active profile is now "prod".`) {
		t.Fatalf("use output = %q", buf.String())
	}

	// 5. Show active profile ("prod")
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show failed: %v", err)
	}
	showOut := buf.String()
	if !strings.Contains(showOut, "prod") || !strings.Contains(showOut, "service_account") {
		t.Fatalf("show output = %q", showOut)
	}
}

func TestConfigAddValidation(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &service.Deps{
		Config: store,
		Out:    output.NewWriter("table", &bytes.Buffer{}),
	}

	// OAuth without client path
	cmd := newConfigCmd(deps)
	cmd.SetArgs([]string{"add", "bad1", "--auth-type", "oauth"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for oauth without client-path")
	}

	// Service account without key path
	cmd = newConfigCmd(deps)
	cmd.SetArgs([]string{"add", "bad2", "--auth-type", "service_account"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for service_account without key-path")
	}

	// Invalid auth type
	cmd = newConfigCmd(deps)
	cmd.SetArgs([]string{"add", "bad3", "--auth-type", "invalid"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid auth-type")
	}
}
