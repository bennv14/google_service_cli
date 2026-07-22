package cmd

import (
	"strings"
	"testing"
)

func TestVersionStringUsesBuildVars(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "gsvc ") {
		t.Fatalf("versionString() = %q, want prefix %q", got, "gsvc ")
	}
	if !strings.Contains(got, version) {
		t.Fatalf("versionString() = %q, want it to contain version %q", got, version)
	}
}
