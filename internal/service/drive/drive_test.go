package drive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/api/option"
)

func TestBuildQuery(t *testing.T) {
	cases := []struct{ folder, query, want string }{
		{"", "", ""},
		{"FID", "", "'FID' in parents"},
		{"", "report", "(name contains 'report' or fullText contains 'report')"},
		{"FID", "report", "'FID' in parents and (name contains 'report' or fullText contains 'report')"},
		{"", "o'brien", `(name contains 'o\'brien' or fullText contains 'o\'brien')`},
	}
	for _, c := range cases {
		if got := buildQuery(c.folder, c.query); got != c.want {
			t.Errorf("buildQuery(%q,%q) = %q, want %q", c.folder, c.query, got, c.want)
		}
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		input int64
		want  int64
	}{
		{-10, 100},
		{0, 100},
		{50, 50},
		{1000, 1000},
		{5000, 1000},
	}
	for _, c := range cases {
		if got := clampLimit(c.input); got != c.want {
			t.Errorf("clampLimit(%d) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestClientList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v3/files" && r.URL.Path != "/files" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"id": "1", "name": "a.txt", "mimeType": "text/plain", "size": "12", "modifiedTime": "2026-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/drive/v3/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	files, err := cl.List(ctx, "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "a.txt" || files[0].Size != 12 {
		t.Fatalf("List() = %+v", files)
	}
}

func TestClientInfoAndSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/files/file-123", "/files/file-123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "file-123",
				"name":         "info.doc",
				"mimeType":     "application/msword",
				"size":         "500",
				"modifiedTime": "2026-02-01T00:00:00Z",
			})
		case "/drive/v3/files", "/files":
			q := r.URL.Query().Get("q")
			if q != "(name contains 'searchterm' or fullText contains 'searchterm')" {
				http.Error(w, "invalid q", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "file-123", "name": "info.doc", "mimeType": "application/msword", "size": "500", "modifiedTime": "2026-02-01T00:00:00Z"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/drive/v3/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	info, err := cl.Info(ctx, "file-123")
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.ID != "file-123" || info.Name != "info.doc" || info.Size != 500 {
		t.Fatalf("Info() = %+v", info)
	}

	results, err := cl.Search(ctx, "searchterm", 5)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 || results[0].ID != "file-123" {
		t.Fatalf("Search() = %+v", results)
	}
}

func TestClientAbout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v3/about" && r.URL.Path != "/about" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"displayName":  "Alice",
				"emailAddress": "alice@example.com",
			},
			"storageQuota": map[string]any{
				"usage": "1000",
				"limit": "10000",
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/drive/v3/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	ab, err := cl.About(ctx)
	if err != nil {
		t.Fatalf("About() error: %v", err)
	}
	if ab.User != "Alice" || ab.Email != "alice@example.com" || ab.UsedBytes != 1000 || ab.LimitBytes != 10000 {
		t.Fatalf("About() = %+v", ab)
	}
}

func TestClientUploadAndDownload(t *testing.T) {
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "upload.txt")
	err := os.WriteFile(localFile, []byte("hello drive"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && (r.URL.Path == "/upload/drive/v3/files" || r.URL.Path == "/upload/files") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "uploaded-id",
				"name":         "custom_name.txt",
				"mimeType":     "text/plain",
				"size":         "11",
				"modifiedTime": "2026-03-01T00:00:00Z",
			})
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/drive/v3/files/file-dl" || r.URL.Path == "/files/file-dl") {
			alt := r.URL.Query().Get("alt")
			if alt == "media" {
				_, _ = w.Write([]byte("downloaded content"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "file-dl",
				"name": "remote_dl.txt",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/drive/v3/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	upFile, err := cl.Upload(ctx, localFile, "parent-folder-id", "custom_name.txt")
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if upFile.ID != "uploaded-id" || upFile.Name != "custom_name.txt" {
		t.Fatalf("Upload() = %+v", upFile)
	}

	dlTarget := filepath.Join(tmpDir, "out_dl.txt")
	outPath, err := cl.Download(ctx, "file-dl", dlTarget)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if outPath != dlTarget {
		t.Errorf("Download() outPath = %q, want %q", outPath, dlTarget)
	}
	content, err := os.ReadFile(dlTarget)
	if err != nil || string(content) != "downloaded content" {
		t.Fatalf("Downloaded file content = %q, err = %v", string(content), err)
	}
}

func TestViews(t *testing.T) {
	fl := FileList{
		{ID: "1", Name: "file1.txt", MimeType: "text/plain", Size: 100, Modified: "2026-01-01"},
	}
	headers := fl.Headers()
	if len(headers) != 5 || headers[0] != "ID" || headers[1] != "NAME" {
		t.Errorf("FileList.Headers() = %v", headers)
	}
	rows := fl.Rows()
	if len(rows) != 1 || rows[0][0] != "1" || rows[0][1] != "file1.txt" || rows[0][3] != "100" {
		t.Errorf("FileList.Rows() = %v", rows)
	}

	ab := About{User: "Bob", Email: "bob@example.com", UsedBytes: 50, LimitBytes: 500}
	abHeaders := ab.Headers()
	if len(abHeaders) != 4 || abHeaders[0] != "USER" {
		t.Errorf("About.Headers() = %v", abHeaders)
	}
	abRows := ab.Rows()
	if len(abRows) != 1 || abRows[0][0] != "Bob" || abRows[0][2] != "50" {
		t.Errorf("About.Rows() = %v", abRows)
	}
}
