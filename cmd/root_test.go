package cmd

import (
	"bytes"
	"strings"
	"testing"
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
