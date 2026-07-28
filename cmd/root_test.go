package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/service"
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var buf bytes.Buffer
	deps := &service.Deps{}
	if err := populateDeps(deps, "", "table", false, &buf); err != nil {
		t.Fatal(err)
	}

	// Assert deps.Scopes is non-empty: registry must contribute scopes
	if len(deps.Scopes) == 0 {
		t.Fatal("deps.Scopes is empty")
	}

	// Assert deps.Scopes equals serviceScopes(services): populateDeps must wire the scope union
	want := serviceScopes(services)
	if len(deps.Scopes) != len(want) {
		t.Fatalf("deps.Scopes = %v, want %v", deps.Scopes, want)
	}
	for i := range want {
		if deps.Scopes[i] != want[i] {
			t.Fatalf("deps.Scopes = %v, want %v", deps.Scopes, want)
		}
	}
}

func TestPopulateDepsRecordsOutputFormat(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var buf bytes.Buffer
	deps := &service.Deps{}
	if err := populateDeps(deps, "", "json", true, &buf); err != nil {
		t.Fatal(err)
	}
	if deps.OutputFormat != "json" || !deps.OutputExplicit {
		t.Fatalf("OutputFormat=%q OutputExplicit=%v", deps.OutputFormat, deps.OutputExplicit)
	}
	if deps.NewOut == nil {
		t.Fatal("NewOut not populated")
	}
	if err := deps.NewOut("text").Render(textOnly{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hi" {
		t.Fatalf("NewOut wrote to the wrong writer: %q", buf.String())
	}
}

type textOnly struct{}

func (textOnly) Text(w io.Writer) error {
	_, err := io.WriteString(w, "hi")
	return err
}

func TestOutputExplicitFlagWiring(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root, deps := buildRootCmdWithDeps()
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetArgs([]string{"version"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if deps.OutputExplicit {
			t.Fatal("OutputExplicit = true, want false when --output not passed")
		}
		if deps.OutputFormat != "table" {
			t.Fatalf("OutputFormat = %q, want %q", deps.OutputFormat, "table")
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root, deps := buildRootCmdWithDeps()
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetArgs([]string{"-o", "json", "version"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !deps.OutputExplicit {
			t.Fatal("OutputExplicit = false, want true when --output passed")
		}
		if deps.OutputFormat != "json" {
			t.Fatalf("OutputFormat = %q, want %q", deps.OutputFormat, "json")
		}
	})
}

func TestRootRegistersChat(t *testing.T) {
	root := buildRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "chat") {
		t.Fatalf("root help missing chat:\n%s", buf.String())
	}
}
