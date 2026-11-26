package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultGitHubBaseURL = "https://api.github.com"
	DefaultRepoOwner     = "bennv14"
	DefaultRepoName      = "google_service_cli"
	DefaultCheckTimeout  = 3 * time.Second

	maxResponseBytes = 1 << 20 // 1 MiB safety limit for API response body
)

// Release describes the latest published GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// Checker fetches the latest available release metadata.
type Checker interface {
	Latest(ctx context.Context) (Release, error)
}

// GitHubChecker queries the GitHub Releases API for the latest release.
type GitHubChecker struct {
	BaseURL   string
	Owner     string
	Repo      string
	UserAgent string
	Client    *http.Client
}

// NewGitHubChecker returns a GitHubChecker configured for google_service_cli.
func NewGitHubChecker(currentVersion string) *GitHubChecker {
	ua := "gsvc"
	if currentVersion != "" {
		ua = "gsvc/" + currentVersion
	}
	return &GitHubChecker{
		BaseURL:   DefaultGitHubBaseURL,
		Owner:     DefaultRepoOwner,
		Repo:      DefaultRepoName,
		UserAgent: ua,
		Client:    &http.Client{Timeout: DefaultCheckTimeout},
	}
}

// Latest retrieves the latest release from GitHub.
func (c *GitHubChecker) Latest(ctx context.Context) (Release, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultGitHubBaseURL
	}
	owner := c.Owner
	if owner == "" {
		owner = DefaultRepoOwner
	}
	repo := c.Repo
	if repo == "" {
		repo = DefaultRepoName
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultCheckTimeout}
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	} else {
		req.Header.Set("User-Agent", "gsvc")
	}

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return Release{}, fmt.Errorf("GitHub API rate limit exceeded (status 403)")
		}
		return Release{}, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel Release
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decode GitHub release response: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return Release{}, fmt.Errorf("GitHub release response missing tag_name")
	}
	if rel.HTMLURL == "" {
		rel.HTMLURL = fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", owner, repo, rel.TagName)
	}
	return rel, nil
}

// IsHomebrewInstall reports whether the resolved executable path belongs to a
// Homebrew Caskroom or Cellar installation of gsvc.
func IsHomebrewInstall(execPath string) bool {
	if execPath == "" {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(execPath))
	return strings.Contains(normalized, "/caskroom/gsvc/") || strings.Contains(normalized, "/cellar/gsvc/")
}
