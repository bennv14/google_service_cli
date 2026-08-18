package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DownloadResult records the outcome of downloading one attachment.
type DownloadResult struct {
	Attachment AttachmentInfo
	LocalPath  string
	Err        error
}

// Downloader centralizes attachment downloading by dispatching to the first matching DownloadStrategy.
type Downloader struct {
	strategies []DownloadStrategy
}

// NewDownloader creates a Downloader with the given download strategies.
func NewDownloader(strategies ...DownloadStrategy) *Downloader {
	var valid []DownloadStrategy
	for _, s := range strategies {
		if s != nil {
			valid = append(valid, s)
		}
	}
	return &Downloader{
		strategies: valid,
	}
}

// RegisterStrategy adds a new DownloadStrategy to the Downloader.
func (d *Downloader) RegisterStrategy(s DownloadStrategy) {
	if s != nil {
		d.strategies = append(d.strategies, s)
	}
}

// DownloadAttachment finds the first strategy that can handle att and executes the download.
func (d *Downloader) DownloadAttachment(ctx context.Context, att AttachmentInfo, targetPath string) error {
	for _, s := range d.strategies {
		if s != nil && s.CanHandle(att) {
			return s.Download(ctx, att, targetPath)
		}
	}
	return fmt.Errorf("no download strategy available for attachment %q (source: %q)", att.ContentName, att.Source)
}

// DownloadAll downloads all attachments to outDir (or customOut if a single attachment).
func (d *Downloader) DownloadAll(ctx context.Context, attachments []AttachmentInfo, outDir, customOut string) ([]DownloadResult, error) {
	if customOut != "" && len(attachments) > 1 {
		return nil, fmt.Errorf("cannot use --out when message has multiple attachments; use --output-dir instead")
	}

	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %q: %w", outDir, err)
	}

	results := make([]DownloadResult, 0, len(attachments))
	for i, att := range attachments {
		targetPath := customOut
		if targetPath == "" {
			name := filepath.Base(filepath.Clean(att.ContentName))
			if name == "" || name == "." || name == ".." || name == "/" || name == "\\" {
				name = shortID(att.Name)
			}
			if name == "" || name == "." || name == ".." || name == "/" || name == "\\" {
				name = fmt.Sprintf("attachment-%d", i+1)
			}
			targetPath = filepath.Join(outDir, name)
		}

		res := DownloadResult{
			Attachment: att,
			LocalPath:  targetPath,
			Err:        d.DownloadAttachment(ctx, att, targetPath),
		}

		results = append(results, res)
	}

	return results, nil
}
