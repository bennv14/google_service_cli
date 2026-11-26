package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	vercheck "github.com/bennv14/google_service_cli/internal/version"
)

type stubChecker struct {
	rel    vercheck.Release
	err    error
	called bool
}

func (s *stubChecker) Latest(context.Context) (vercheck.Release, error) {
	s.called = true
	return s.rel, s.err
}

func withVersionTestHooks(t *testing.T, ver, com, dt, execPath string, checker vercheck.Checker) {
	t.Helper()
	origVer, origCommit, origDate := version, commit, date
	origChecker := newVersionChecker
	origExec := resolveExecutablePath
	t.Cleanup(func() {
		version, commit, date = origVer, origCommit, origDate
		newVersionChecker = origChecker
		resolveExecutablePath = origExec
	})
	version, commit, date = ver, com, dt
	if checker != nil {
		newVersionChecker = func(string) vercheck.Checker { return checker }
	}
	resolveExecutablePath = func() string { return execPath }
}

func TestVersionStringUsesBuildVars(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "gsvc ") {
		t.Fatalf("versionString() = %q, want prefix %q", got, "gsvc ")
	}
	if !strings.Contains(got, version) {
		t.Fatalf("versionString() = %q, want it to contain version %q", got, version)
	}
}

func TestRootVersionFlagsAreLocalOnly(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		t.Run(flag, func(t *testing.T) {
			stub := &stubChecker{rel: vercheck.Release{TagName: "v9.9.9"}}
			withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "", stub)

			root := buildRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{flag})

			if err := root.Execute(); err != nil {
				t.Fatalf("gsvc %s error: %v", flag, err)
			}
			if stub.called {
				t.Fatalf("gsvc %s should not perform remote update check", flag)
			}
			want := "gsvc v3.0.1 (commit 7e28430, built 2026-11-15T00:00:00Z)\n"
			if stdout.String() != want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
		})
	}
}

func TestVersionCmdChecksUpdateByDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSVC_NO_UPDATE_CHECK", "")

	stub := &stubChecker{
		rel: vercheck.Release{
			TagName: "v3.1.0",
			HTMLURL: "https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
		},
	}
	withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "/opt/homebrew/Caskroom/gsvc/3.0.1/gsvc", stub)

	root := buildRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("gsvc version error: %v", err)
	}
	if !stub.called {
		t.Fatal("expected update checker to be called by default")
	}
	out := stdout.String()
	for _, want := range []string{
		"gsvc v3.0.1 (commit 7e28430, built 2026-11-15T00:00:00Z)",
		"Update available: v3.0.1 -> v3.1.0",
		"Release: https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
		"Upgrade: brew upgrade gsvc",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestVersionCmdNoCheckFlagAndEnv(t *testing.T) {
	t.Run("flag --no-check", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("GSVC_NO_UPDATE_CHECK", "")
		stub := &stubChecker{rel: vercheck.Release{TagName: "v3.1.0"}}
		withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "", stub)

		root := buildRootCmd()
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetArgs([]string{"version", "--no-check"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if stub.called {
			t.Fatal("checker should not be called when --no-check is passed")
		}
		want := "gsvc v3.0.1 (commit 7e28430, built 2026-11-15T00:00:00Z)\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("env GSVC_NO_UPDATE_CHECK=1", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("GSVC_NO_UPDATE_CHECK", "1")
		stub := &stubChecker{rel: vercheck.Release{TagName: "v3.1.0"}}
		withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "", stub)

		root := buildRootCmd()
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetArgs([]string{"version"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if stub.called {
			t.Fatal("checker should not be called when GSVC_NO_UPDATE_CHECK=1")
		}
	})
}

func TestVersionCmdJSONAndOfflineFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSVC_NO_UPDATE_CHECK", "")

	stub := &stubChecker{err: errors.New("dial tcp: network unreachable")}
	withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "/usr/local/bin/gsvc", stub)

	root := buildRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version", "-o", "json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("offline update check should be non-fatal, got error: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: update check failed") {
		t.Fatalf("expected warning on stderr, got %q", stderr.String())
	}

	var decoded vercheck.Info
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout must remain valid JSON on offline error: %v\n%s", err, stdout.String())
	}
	if decoded.Version != "v3.0.1" || decoded.Status != vercheck.StatusCheckFailed || decoded.UpdateAvailable {
		t.Fatalf("unexpected decoded info: %+v", decoded)
	}
}

func TestVersionCmdTableOutput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSVC_NO_UPDATE_CHECK", "")

	stub := &stubChecker{rel: vercheck.Release{TagName: "v3.0.1"}}
	withVersionTestHooks(t, "v3.0.1", "7e28430", "2026-11-15T00:00:00Z", "", stub)

	root := buildRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"version", "-o", "table"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"VERSION", "LATEST", "STATUS", "v3.0.1", vercheck.StatusUpToDate} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}
