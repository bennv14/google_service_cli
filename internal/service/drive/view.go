package drive

import (
	"fmt"

	driveapi "google.golang.org/api/drive/v3"
)

// File is the trimmed Drive file shape used for output.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Modified string `json:"modifiedTime"`
}

func toFile(f *driveapi.File) File {
	return File{ID: f.Id, Name: f.Name, MimeType: f.MimeType, Size: f.Size, Modified: f.ModifiedTime}
}

func toFiles(in []*driveapi.File) []File {
	out := make([]File, 0, len(in))
	for _, f := range in {
		out = append(out, toFile(f))
	}
	return out
}

// FileList renders as a table and marshals as a JSON array.
type FileList []File

func (fl FileList) Headers() []string { return []string{"ID", "NAME", "TYPE", "SIZE", "MODIFIED"} }

func (fl FileList) Rows() [][]string {
	rows := make([][]string, 0, len(fl))
	for _, f := range fl {
		rows = append(rows, []string{f.ID, f.Name, f.MimeType, fmt.Sprintf("%d", f.Size), f.Modified})
	}
	return rows
}

// About renders account + quota info.
type About struct {
	User       string `json:"user"`
	Email      string `json:"email"`
	UsedBytes  int64  `json:"usedBytes"`
	LimitBytes int64  `json:"limitBytes"`
}

func (a About) Headers() []string { return []string{"USER", "EMAIL", "USED", "LIMIT"} }

func (a About) Rows() [][]string {
	return [][]string{{a.User, a.Email, fmt.Sprintf("%d", a.UsedBytes), fmt.Sprintf("%d", a.LimitBytes)}}
}
