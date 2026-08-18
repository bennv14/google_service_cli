package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloader_DownloadAttachment(t *testing.T) {
	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			return os.WriteFile(targetPath, []byte("chat file"), 0644)
		},
	}
	driveMock := &mockDriveClient{
		downloadFunc: func(ctx context.Context, id, outPath string) (string, error) {
			return outPath, os.WriteFile(outPath, []byte("drive file"), 0644)
		},
	}

	chatStrat := NewChatMediaStrategy(chatMock)
	driveStrat := NewDriveFileStrategy(driveMock)
	dl := NewDownloader(chatStrat, driveStrat)

	tmpDir := t.TempDir()

	t.Run("ChatAttachment", func(t *testing.T) {
		p := filepath.Join(tmpDir, "chat.png")
		err := dl.DownloadAttachment(context.Background(), AttachmentInfo{
			Source:       "UPLOADED_CONTENT",
			ResourceName: "res1",
		}, p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(p)
		if string(data) != "chat file" {
			t.Errorf("content = %q, want chat file", string(data))
		}
	})

	t.Run("DriveAttachment", func(t *testing.T) {
		p := filepath.Join(tmpDir, "drive.pdf")
		err := dl.DownloadAttachment(context.Background(), AttachmentInfo{
			Source:      "DRIVE_FILE",
			DriveFileID: "drv1",
		}, p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(p)
		if string(data) != "drive file" {
			t.Errorf("content = %q, want drive file", string(data))
		}
	})

	t.Run("UnsupportedAttachment", func(t *testing.T) {
		err := dl.DownloadAttachment(context.Background(), AttachmentInfo{
			Source: "UNSUPPORTED",
		}, "/tmp/unused")
		if err == nil || !strings.Contains(err.Error(), "no download strategy available") {
			t.Fatalf("expected no download strategy available error, got: %v", err)
		}
	})
}

func TestDownloader_DownloadAll_Success(t *testing.T) {
	tmpDir := t.TempDir()

	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			return os.WriteFile(targetPath, []byte("chat file content"), 0644)
		},
	}
	driveMock := &mockDriveClient{
		downloadFunc: func(ctx context.Context, id, outPath string) (string, error) {
			return outPath, os.WriteFile(outPath, []byte("drive file content"), 0644)
		},
	}

	downloader := NewDownloader(NewChatMediaStrategy(chatMock), NewDriveFileStrategy(driveMock))
	atts := []AttachmentInfo{
		{
			Name:         "spaces/s/messages/m/attachments/a1",
			ContentName:  "image.png",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "spaces/s/messages/m/attachments/a1",
		},
		{
			Name:        "spaces/s/messages/m/attachments/a2",
			ContentName: "report.pdf",
			Source:      "DRIVE_FILE",
			DriveFileID: "drive-id-456",
		},
	}

	results, err := downloader.DownloadAll(context.Background(), atts, tmpDir, "")
	if err != nil {
		t.Fatalf("DownloadAll returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for i, res := range results {
		if res.Err != nil {
			t.Errorf("result[%d] had error: %v", i, res.Err)
		}
	}

	if results[0].LocalPath != filepath.Join(tmpDir, "image.png") {
		t.Errorf("result[0].LocalPath = %q, want %q", results[0].LocalPath, filepath.Join(tmpDir, "image.png"))
	}
	if results[1].LocalPath != filepath.Join(tmpDir, "report.pdf") {
		t.Errorf("result[1].LocalPath = %q, want %q", results[1].LocalPath, filepath.Join(tmpDir, "report.pdf"))
	}

	gotBytes1, _ := os.ReadFile(results[0].LocalPath)
	if string(gotBytes1) != "chat file content" {
		t.Errorf("result[0] content = %q, want %q", string(gotBytes1), "chat file content")
	}
	gotBytes2, _ := os.ReadFile(results[1].LocalPath)
	if string(gotBytes2) != "drive file content" {
		t.Errorf("result[1] content = %q, want %q", string(gotBytes2), "drive file content")
	}
}

func TestDownloader_DownloadAll_FallbackNames(t *testing.T) {
	tmpDir := t.TempDir()

	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			return os.WriteFile(targetPath, []byte("content"), 0644)
		},
	}

	downloader := NewDownloader(NewChatMediaStrategy(chatMock))

	atts := []AttachmentInfo{
		{
			Name:         "spaces/s/messages/m/attachments/att99",
			ContentName:  "", // empty ContentName -> should use shortID(Name) = "att99"
			Source:       "UPLOADED_CONTENT",
			ResourceName: "spaces/s/messages/m/attachments/att99",
		},
		{
			Name:         "", // empty Name and empty ContentName -> should use "attachment-2"
			ContentName:  "..",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "res2",
		},
	}

	results, err := downloader.DownloadAll(context.Background(), atts, tmpDir, "")
	if err != nil {
		t.Fatalf("DownloadAll returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].LocalPath != filepath.Join(tmpDir, "att99") {
		t.Errorf("result[0].LocalPath = %q, want %q", results[0].LocalPath, filepath.Join(tmpDir, "att99"))
	}
	if results[1].LocalPath != filepath.Join(tmpDir, "attachment-2") {
		t.Errorf("result[1].LocalPath = %q, want %q", results[1].LocalPath, filepath.Join(tmpDir, "attachment-2"))
	}
}

func TestDownloader_DownloadAll_PathSanitization(t *testing.T) {
	tmpDir := t.TempDir()

	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			return os.WriteFile(targetPath, []byte("safe"), 0644)
		},
	}

	downloader := NewDownloader(NewChatMediaStrategy(chatMock))

	atts := []AttachmentInfo{
		{
			Name:         "spaces/s/messages/m/attachments/a1",
			ContentName:  "../../../../etc/passwd",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "spaces/s/messages/m/attachments/a1",
		},
	}

	results, err := downloader.DownloadAll(context.Background(), atts, tmpDir, "")
	if err != nil {
		t.Fatalf("DownloadAll returned error: %v", err)
	}

	wantPath := filepath.Join(tmpDir, "passwd")
	if results[0].LocalPath != wantPath {
		t.Fatalf("expected path to be sanitized to %q, got %q", wantPath, results[0].LocalPath)
	}

	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("file not found at sanitized path %q: %v", wantPath, err)
	}
}

func TestDownloader_DownloadAll_CustomOut_Single(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "my-custom-output.dat")

	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			return os.WriteFile(targetPath, []byte("custom data"), 0644)
		},
	}

	downloader := NewDownloader(NewChatMediaStrategy(chatMock))

	atts := []AttachmentInfo{
		{
			Name:         "spaces/s/messages/m/attachments/a1",
			ContentName:  "original.dat",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "spaces/s/messages/m/attachments/a1",
		},
	}

	results, err := downloader.DownloadAll(context.Background(), atts, tmpDir, customFile)
	if err != nil {
		t.Fatalf("DownloadAll returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].LocalPath != customFile {
		t.Fatalf("LocalPath = %q, want %q", results[0].LocalPath, customFile)
	}

	content, err := os.ReadFile(customFile)
	if err != nil {
		t.Fatalf("failed to read custom output file: %v", err)
	}
	if string(content) != "custom data" {
		t.Fatalf("content = %q, want %q", string(content), "custom data")
	}
}

func TestDownloader_DownloadAll_CustomOut_MultipleError(t *testing.T) {
	downloader := NewDownloader()

	atts := []AttachmentInfo{
		{Name: "a1", ContentName: "f1.txt"},
		{Name: "a2", ContentName: "f2.txt"},
	}

	_, err := downloader.DownloadAll(context.Background(), atts, ".", "custom.txt")
	if err == nil || !strings.Contains(err.Error(), "cannot use --out when message has multiple attachments") {
		t.Fatalf("expected cannot use --out error, got: %v", err)
	}
}

func TestDownloader_DownloadAll_PartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	chatMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			if resourceName == "res-fail" {
				return errors.New("failed to fetch stream")
			}
			return os.WriteFile(targetPath, []byte("ok content"), 0644)
		},
	}

	downloader := NewDownloader(NewChatMediaStrategy(chatMock))

	atts := []AttachmentInfo{
		{
			Name:         "a1",
			ContentName:  "ok.txt",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "res-ok",
		},
		{
			Name:         "a2",
			ContentName:  "bad.txt",
			Source:       "UPLOADED_CONTENT",
			ResourceName: "res-fail",
		},
	}

	results, err := downloader.DownloadAll(context.Background(), atts, tmpDir, "")
	if err != nil {
		t.Fatalf("DownloadAll should return results list even if individual downloads fail, got err: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("expected result[0] to succeed, got error: %v", results[0].Err)
	}
	if results[1].Err == nil {
		t.Errorf("expected result[1] to fail, got nil error")
	}
}
