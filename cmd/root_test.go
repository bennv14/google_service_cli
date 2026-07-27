package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bennv/google_service_cli/internal/service"
)

func TestRootWiresSubcommands(t *testing.T) {
	root := buildRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"drive", "auth", "config", "version"} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
}

func TestGlobalOutputFlagRegistered(t *testing.T) {
	root := buildRootCmd()
	if root.PersistentFlags().Lookup("output") == nil {
		t.Fatal("--output persistent flag not registered")
	}
	if root.PersistentFlags().Lookup("profile") == nil {
		t.Fatal("--profile persistent flag not registered")
	}
}

type fakeService struct {
	name   string
	scopes []string
}

func (f fakeService) Name() string     { return f.name }
func (f fakeService) Scopes() []string { return f.scopes }
func (f fakeService) Command(*service.Deps) *cobra.Command {
	return &cobra.Command{Use: f.name}
}

func TestServiceScopesUnionsAndDedupes(t *testing.T) {
	got := serviceScopes([]service.Service{
		fakeService{name: "b", scopes: []string{"scope/z", "scope/a"}},
		fakeService{name: "a", scopes: []string{"scope/a", "scope/m"}},
	})
	want := []string{"scope/a", "scope/m", "scope/z"}
	if len(got) != len(want) {
		t.Fatalf("serviceScopes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("serviceScopes() = %v, want %v", got, want)
		}
	}
}

func TestRootPopulatesDepsScopes(t *testing.T) {
	if len(serviceScopes(services)) == 0 {
		t.Fatal("registry contributes no scopes")
	}
}
