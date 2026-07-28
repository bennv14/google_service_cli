package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bennv14/google_service_cli/internal/config"
	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
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

func TestConfigListJSON(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	deps := &service.Deps{
		Config: store,
		Out:    output.NewWriter("json", &buf),
	}

	// Add profile using flag aliases --auth and --client
	cmd := newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "dev", "--auth", "oauth", "--client", "/tmp/client.json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add dev with aliases failed: %v", err)
	}

	// Add profile using flag aliases --auth and --key
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "prod", "--auth", "service_account", "--key", "/tmp/key.json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add prod with aliases failed: %v", err)
	}

	// List profiles with json output
	buf.Reset()
	cmd = newConfigCmd(deps)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list json failed: %v", err)
	}

	var res struct {
		Profiles []config.Profile `json:"profiles"`
		Active   string           `json:"active"`
	}
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal json list output failed: %v, raw: %q", err, buf.String())
	}

	if res.Active != "dev" {
		t.Errorf("expected active profile 'dev', got %q", res.Active)
	}
	if len(res.Profiles) != 2 {
		t.Fatalf("expected 2 profiles in json output, got %d", len(res.Profiles))
	}
	if res.Profiles[0].Name != "dev" || res.Profiles[0].AuthType != "oauth" || res.Profiles[0].ClientPath != "/tmp/client.json" {
		t.Errorf("unexpected profile[0]: %+v", res.Profiles[0])
	}
	if res.Profiles[1].Name != "prod" || res.Profiles[1].AuthType != "service_account" || res.Profiles[1].KeyPath != "/tmp/key.json" {
		t.Errorf("unexpected profile[1]: %+v", res.Profiles[1])
	}
}

