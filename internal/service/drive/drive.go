// Package drive implements the Google Drive service commands and API client.
package drive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	driveapi "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	scopeReadonly = "https://www.googleapis.com/auth/drive.readonly"
	scopeFile     = "https://www.googleapis.com/auth/drive.file"
)

// OAuthScopes is the union of scopes requested at login for v1.
var OAuthScopes = []string{scopeReadonly, scopeFile}

const fileFields = "id,name,mimeType,size,modifiedTime"

// Client wraps the Drive v3 API.
type Client struct{ svc *driveapi.Service }

// NewClient builds a Drive client. Extra options are used by tests
// (e.g. option.WithEndpoint, option.WithoutAuthentication).
func NewClient(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	all := append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := driveapi.NewService(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

func (c *Client) List(ctx context.Context, folder, query string, limit int64) ([]File, error) {
	call := c.svc.Files.List().Context(ctx).
		Fields("files(" + fileFields + ")").
		PageSize(clampLimit(limit))
	if q := buildQuery(folder, query); q != "" {
		call = call.Q(q)
	}
	res, err := call.Do()
	if err != nil {
		return nil, err
	}
	return toFiles(res.Files), nil
}

func (c *Client) Info(ctx context.Context, id string) (File, error) {
	f, err := c.svc.Files.Get(id).Context(ctx).Fields(fileFields).Do()
	if err != nil {
		return File{}, err
	}
	return toFile(f), nil
}

func (c *Client) Search(ctx context.Context, query string, limit int64) ([]File, error) {
	return c.List(ctx, "", query, limit)
}

func (c *Client) About(ctx context.Context) (About, error) {
	a, err := c.svc.About.Get().Context(ctx).
		Fields("user(displayName,emailAddress),storageQuota(usage,limit)").Do()
	if err != nil {
		return About{}, err
	}
	out := About{}
	if a.User != nil {
		out.User = a.User.DisplayName
		out.Email = a.User.EmailAddress
	}
	if a.StorageQuota != nil {
		out.UsedBytes = a.StorageQuota.Usage
		out.LimitBytes = a.StorageQuota.Limit
	}
	return out, nil
}

func (c *Client) Upload(ctx context.Context, localPath, parent, name string) (File, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return File{}, err
	}
	defer f.Close()

	meta := &driveapi.File{Name: name}
	if meta.Name == "" {
		meta.Name = filepath.Base(localPath)
	}
	if parent != "" {
		meta.Parents = []string{parent}
	}
	created, err := c.svc.Files.Create(meta).Context(ctx).Media(f).Fields(fileFields).Do()
	if err != nil {
		return File{}, err
	}
	return toFile(created), nil
}

// Download writes the file's bytes to outPath (or its Drive name if outPath is
// empty) and returns the path written. Google-native docs (Docs/Sheets/Slides)
// are not downloadable this way and return an error from the API.
func (c *Client) Download(ctx context.Context, id, outPath string) (string, error) {
	if outPath == "" {
		meta, err := c.svc.Files.Get(id).Context(ctx).Fields("name").Do()
		if err != nil {
			return "", err
		}
		outPath = meta.Name
	}
	resp, err := c.svc.Files.Get(id).Context(ctx).Download()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return outPath, nil
}

// buildQuery composes a Drive `q` from an optional parent folder and text query.
func buildQuery(folder, query string) string {
	var parts []string
	if folder != "" {
		parts = append(parts, fmt.Sprintf("'%s' in parents", folder))
	}
	if query != "" {
		q := escapeQ(query)
		parts = append(parts, fmt.Sprintf("(name contains '%s' or fullText contains '%s')", q, q))
	}
	return strings.Join(parts, " and ")
}

func escapeQ(s string) string { return strings.ReplaceAll(s, "'", `\'`) }

func clampLimit(n int64) int64 {
	if n <= 0 {
		return 100
	}
	if n > 1000 {
		return 1000
	}
	return n
}
