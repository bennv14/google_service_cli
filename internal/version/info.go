package version

import (
	"fmt"
	"io"
)

const (
	StatusUpdateAvailable = "update_available"
	StatusUpToDate        = "up_to_date"
	StatusDev             = "dev"
	StatusUnchecked       = "unchecked"
	StatusCheckFailed     = "check_failed"
)

// Info holds local build metadata plus optional remote update-check results.
// It implements output.TextView and output.TableView.
type Info struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	Date            string `json:"date"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url,omitempty"`
	Status          string `json:"status,omitempty"`
	homebrew        bool
}

// NewInfo builds an Info with local build metadata.
func NewInfo(ver, commit, date string, homebrew bool) Info {
	return Info{
		Version:  ver,
		Commit:   commit,
		Date:     date,
		Status:   StatusUnchecked,
		homebrew: homebrew,
	}
}

// SummaryLine returns the concise single-line version banner used by -v/--version
// and the first line of text output.
func (i Info) SummaryLine() string {
	return fmt.Sprintf("gsvc %s (commit %s, built %s)", i.Version, i.Commit, i.Date)
}

// ApplyRelease compares the local version against rel and updates LatestVersion,
// ReleaseURL, UpdateAvailable, and Status.
func (i *Info) ApplyRelease(rel Release) {
	i.LatestVersion = rel.TagName
	i.ReleaseURL = rel.HTMLURL

	if IsDevBuild(i.Version) {
		i.UpdateAvailable = false
		i.Status = StatusDev
		return
	}

	cmp, err := Compare(i.Version, rel.TagName)
	if err != nil {
		i.UpdateAvailable = false
		i.Status = StatusCheckFailed
		return
	}
	if cmp < 0 {
		i.UpdateAvailable = true
		i.Status = StatusUpdateAvailable
		return
	}
	i.UpdateAvailable = false
	i.Status = StatusUpToDate
}

// Text implements output.TextView.
func (i Info) Text(w io.Writer) error {
	if _, err := fmt.Fprintln(w, i.SummaryLine()); err != nil {
		return err
	}
	switch i.Status {
	case StatusUpdateAvailable:
		if _, err := fmt.Fprintf(w, "Update available: %s -> %s\n", i.Version, i.LatestVersion); err != nil {
			return err
		}
		if i.ReleaseURL != "" {
			if _, err := fmt.Fprintf(w, "Release: %s\n", i.ReleaseURL); err != nil {
				return err
			}
		}
		if i.homebrew {
			if _, err := fmt.Fprintln(w, "Upgrade: brew upgrade gsvc"); err != nil {
				return err
			}
		}
	case StatusUpToDate:
		if i.LatestVersion != "" {
			if _, err := fmt.Fprintf(w, "Up to date (latest: %s)\n", i.LatestVersion); err != nil {
				return err
			}
		}
	case StatusDev:
		if i.LatestVersion != "" {
			if _, err := fmt.Fprintf(w, "Dev build (latest release: %s)\n", i.LatestVersion); err != nil {
				return err
			}
		}
	}
	return nil
}

// Headers implements output.TableView.
func (i Info) Headers() []string {
	return []string{"VERSION", "LATEST", "STATUS", "COMMIT", "BUILT"}
}

// Rows implements output.TableView.
func (i Info) Rows() [][]string {
	latest := i.LatestVersion
	if latest == "" {
		latest = "-"
	}
	status := i.Status
	if status == "" {
		status = "-"
	}
	return [][]string{{i.Version, latest, status, i.Commit, i.Date}}
}
