package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/api/option"

	"github.com/bennv/google_service_cli/internal/output"
	"github.com/bennv/google_service_cli/internal/service"
)

func TestDriveServiceName(t *testing.T) {
	svc := New()
	if svc.Name() != "drive" {
		t.Fatalf("expected drive, got %s", svc.Name())
	}
}

func TestDriveListCommandRendersJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"id": "1", "name": "a.txt", "mimeType": "text/plain", "size": "12", "modifiedTime": "2026-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := &service.Deps{
		Out: output.NewWriter("json", &buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"list"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"name": "a.txt"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestDriveInfoCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "file-123",
			"name":         "info_doc.txt",
			"mimeType":     "text/plain",
			"size":         "50",
			"modifiedTime": "2026-01-02T10:00:00Z",
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := &service.Deps{
		Out: output.NewWriter("json", &buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"info", "file-123"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"file-123"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestDriveSearchCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"id": "s-1", "name": "search_result.pdf", "mimeType": "application/pdf", "size": "100", "modifiedTime": "2026-01-03T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := &service.Deps{
		Out: output.NewWriter("json", &buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"search", "report"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"search_result.pdf"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestDriveAboutCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"displayName":  "Alice",
				"emailAddress": "alice@example.com",
			},
			"storageQuota": map[string]any{
				"usage": "1048576",
				"limit": "15000000000",
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := &service.Deps{
		Out: output.NewWriter("json", &buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"about"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"alice@example.com"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestDriveUploadCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "up-123",
			"name":         "test.txt",
			"mimeType":     "text/plain",
			"size":         "11",
			"modifiedTime": "2026-01-04T00:00:00Z",
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(localFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	deps := &service.Deps{
		Out: output.NewWriter("json", &buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"upload", localFile})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"up-123"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestDriveDownloadCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") == "media" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("file content"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "dl.txt",
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "downloaded.txt")

	deps := &service.Deps{
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL), option.WithoutAuthentication()}
	defer func() { testClientOpts = nil }()

	var outBuf bytes.Buffer
	cmd := New().Command(deps)
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"download", "dl-123", "--out", outPath})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "file content" {
		t.Fatalf("got file content = %q, expected %q", string(data), "file content")
	}
}
