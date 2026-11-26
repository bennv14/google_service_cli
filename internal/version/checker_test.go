package version

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bennv14/google_service_cli/internal/output"
)

func TestGitHubCheckerLatestSuccess(t *testing.T) {
	var gotPath, gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v3.1.0","html_url":"https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0"}`))
	}))
	defer srv.Close()

	c := &GitHubChecker{
		BaseURL:   srv.URL,
		Owner:     "bennv14",
		Repo:      "google_service_cli",
		UserAgent: "gsvc/v3.0.1",
		Client:    srv.Client(),
	}

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest() unexpected error: %v", err)
	}
	if gotPath != "/repos/bennv14/google_service_cli/releases/latest" {
		t.Fatalf("request path = %q, want /repos/bennv14/google_service_cli/releases/latest", gotPath)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("Accept header = %q, want application/vnd.github+json", gotAccept)
	}
	if gotUA != "gsvc/v3.0.1" {
		t.Fatalf("User-Agent = %q, want gsvc/v3.0.1", gotUA)
	}
	if rel.TagName != "v3.1.0" {
		t.Fatalf("TagName = %q, want v3.1.0", rel.TagName)
	}
	if rel.HTMLURL != "https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0" {
		t.Fatalf("HTMLURL = %q", rel.HTMLURL)
	}
}

func TestGitHubCheckerErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    map[string]string
		body       string
		wantSubstr string
	}{
		{
			name:       "rate limited 403",
			status:     http.StatusForbidden,
			headers:    map[string]string{"X-RateLimit-Remaining": "0"},
			body:       `{"message":"API rate limit exceeded"}`,
			wantSubstr: "rate limit",
		},
		{
			name:       "server error 500",
			status:     http.StatusInternalServerError,
			body:       `internal error`,
			wantSubstr: "500",
		},
		{
			name:       "invalid json",
			status:     http.StatusOK,
			body:       `not json`,
			wantSubstr: "decode",
		},
		{
			name:       "missing tag_name",
			status:     http.StatusOK,
			body:       `{"tag_name":"","html_url":""}`,
			wantSubstr: "tag_name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := &GitHubChecker{
				BaseURL: srv.URL,
				Owner:   "bennv14",
				Repo:    "google_service_cli",
				Client:  srv.Client(),
			}
			_, err := c.Latest(context.Background())
			if err == nil {
				t.Fatalf("Latest() error = nil, want error containing %q", tc.wantSubstr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.wantSubstr) {
				t.Fatalf("Latest() error = %q, want substring %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestGitHubCheckerTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := &GitHubChecker{
		BaseURL: srv.URL,
		Owner:   "bennv14",
		Repo:    "google_service_cli",
		Client:  srv.Client(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := c.Latest(ctx); err == nil {
		t.Fatal("Latest() expected timeout error, got nil")
	}
}

func TestIsHomebrewInstall(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Caskroom/gsvc/3.0.1/gsvc", true},
		{"/usr/local/Caskroom/gsvc/3.0.1/gsvc", true},
		{"/home/linuxbrew/.linuxbrew/Caskroom/gsvc/3.0.1/gsvc", true},
		{"/opt/homebrew/Cellar/gsvc/3.0.1/bin/gsvc", true},
		{"/Users/bennv/go/bin/gsvc", false},
		{"/usr/local/bin/gsvc", false},
		{"C:\\Program Files\\gsvc\\gsvc.exe", false},
		{"", false},
	}

	for _, tc := range tests {
		if got := IsHomebrewInstall(tc.path); got != tc.want {
			t.Errorf("IsHomebrewInstall(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestInfoApplyReleaseAndRenderText(t *testing.T) {
	t.Run("update available via homebrew", func(t *testing.T) {
		info := NewInfo("v3.0.1", "7e28430", "2026-11-15T00:00:00Z", true)
		info.ApplyRelease(Release{
			TagName: "v3.1.0",
			HTMLURL: "https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
		})
		if !info.UpdateAvailable || info.Status != StatusUpdateAvailable {
			t.Fatalf("got UpdateAvailable=%v Status=%q", info.UpdateAvailable, info.Status)
		}
		var buf bytes.Buffer
		if err := output.NewWriter("text", &buf).Render(info); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		for _, want := range []string{
			"gsvc v3.0.1 (commit 7e28430, built 2026-11-15T00:00:00Z)",
			"Update available: v3.0.1 -> v3.1.0",
			"Release: https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
			"Upgrade: brew upgrade gsvc",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("text output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("update available non-homebrew omits brew hint", func(t *testing.T) {
		info := NewInfo("v3.0.1", "7e28430", "2026-11-15T00:00:00Z", false)
		info.ApplyRelease(Release{
			TagName: "v3.1.0",
			HTMLURL: "https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
		})
		var buf bytes.Buffer
		if err := output.NewWriter("text", &buf).Render(info); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, "brew upgrade") {
			t.Fatalf("non-homebrew output should not suggest brew upgrade:\n%s", out)
		}
		if !strings.Contains(out, "Release: https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0") {
			t.Fatalf("missing release URL:\n%s", out)
		}
	})

	t.Run("up to date", func(t *testing.T) {
		info := NewInfo("v3.0.1", "7e28430", "2026-11-15T00:00:00Z", true)
		info.ApplyRelease(Release{TagName: "v3.0.1"})
		if info.UpdateAvailable || info.Status != StatusUpToDate {
			t.Fatalf("got UpdateAvailable=%v Status=%q", info.UpdateAvailable, info.Status)
		}
		var buf bytes.Buffer
		if err := output.NewWriter("text", &buf).Render(info); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "Up to date (latest: v3.0.1)") {
			t.Fatalf("unexpected text output:\n%s", buf.String())
		}
	})

	t.Run("dev build", func(t *testing.T) {
		info := NewInfo("dev", "none", "unknown", false)
		info.ApplyRelease(Release{TagName: "v3.0.1"})
		if info.UpdateAvailable || info.Status != StatusDev {
			t.Fatalf("got UpdateAvailable=%v Status=%q", info.UpdateAvailable, info.Status)
		}
		var buf bytes.Buffer
		if err := output.NewWriter("text", &buf).Render(info); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "Dev build (latest release: v3.0.1)") {
			t.Fatalf("unexpected text output:\n%s", buf.String())
		}
	})

	t.Run("table and json rendering", func(t *testing.T) {
		info := NewInfo("v3.0.1", "7e28430", "2026-11-15T00:00:00Z", false)
		info.ApplyRelease(Release{
			TagName: "v3.1.0",
			HTMLURL: "https://github.com/bennv14/google_service_cli/releases/tag/v3.1.0",
		})

		var tableBuf bytes.Buffer
		if err := output.NewWriter("table", &tableBuf).Render(info); err != nil {
			t.Fatal(err)
		}
		tableOut := tableBuf.String()
		for _, want := range []string{"VERSION", "LATEST", "STATUS", "v3.0.1", "v3.1.0", StatusUpdateAvailable} {
			if !strings.Contains(tableOut, want) {
				t.Errorf("table output missing %q:\n%s", want, tableOut)
			}
		}

		var jsonBuf bytes.Buffer
		if err := output.NewWriter("json", &jsonBuf).Render(info); err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
			t.Fatalf("invalid json output: %v\n%s", err, jsonBuf.String())
		}
		if decoded["version"] != "v3.0.1" || decoded["latest_version"] != "v3.1.0" || decoded["update_available"] != true {
			t.Fatalf("unexpected json payload: %v", decoded)
		}
	})
}
