package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockChatMediaClient struct {
	downloadFunc func(ctx context.Context, resourceName, targetPath string) error
}

func (m *mockChatMediaClient) DownloadMedia(ctx context.Context, resourceName, targetPath string) error {
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, resourceName, targetPath)
	}
	return errors.New("mockChatMediaClient not implemented")
}

type mockDriveClient struct {
	downloadFunc func(ctx context.Context, id, outPath string) (string, error)
}

func (m *mockDriveClient) Download(ctx context.Context, id, outPath string) (string, error) {
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, id, outPath)
	}
	return "", errors.New("mockDriveClient not implemented")
}

func TestChatMediaStrategy_CanHandle(t *testing.T) {
	strat := NewChatMediaStrategy(nil)

	if !strat.CanHandle(AttachmentInfo{Source: "UPLOADED_CONTENT"}) {
		t.Error("expected CanHandle to be true for UPLOADED_CONTENT")
	}
	if !strat.CanHandle(AttachmentInfo{ResourceName: "spaces/s/messages/m/attachments/a"}) {
		t.Error("expected CanHandle to be true when ResourceName is set")
	}
	if strat.CanHandle(AttachmentInfo{Source: "DRIVE_FILE"}) {
		t.Error("expected CanHandle to be false for DRIVE_FILE without ResourceName")
	}
	if strat.CanHandle(AttachmentInfo{Source: "OTHER"}) {
		t.Error("expected CanHandle to be false for OTHER")
	}
}

func TestChatMediaStrategy_Download(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "image.png")

	var calledRes, calledTarget string
	clientMock := &mockChatMediaClient{
		downloadFunc: func(ctx context.Context, resourceName, targetPath string) error {
			calledRes = resourceName
			calledTarget = targetPath
			return os.WriteFile(targetPath, []byte("chat bytes"), 0644)
		},
	}

	strat := NewChatMediaStrategy(clientMock)
	att := AttachmentInfo{
		Source:       "UPLOADED_CONTENT",
		ResourceName: "spaces/s1/messages/m1/attachments/att1",
	}

	if err := strat.Download(context.Background(), att, targetPath); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if calledRes != "spaces/s1/messages/m1/attachments/att1" || calledTarget != targetPath {
		t.Fatalf("unexpected call args: res=%q, target=%q", calledRes, calledTarget)
	}

	// Test nil client error
	nilStrat := NewChatMediaStrategy(nil)
	if err := nilStrat.Download(context.Background(), att, targetPath); err == nil || !strings.Contains(err.Error(), "chat media client not configured") {
		t.Fatalf("expected client not configured error, got: %v", err)
	}
}

func TestDriveFileStrategy_CanHandle(t *testing.T) {
	strat := NewDriveFileStrategy(nil)

	if !strat.CanHandle(AttachmentInfo{Source: "DRIVE_FILE"}) {
		t.Error("expected CanHandle to be true for DRIVE_FILE")
	}
	if !strat.CanHandle(AttachmentInfo{DriveFileID: "file-xyz"}) {
		t.Error("expected CanHandle to be true when DriveFileID is set")
	}
	if strat.CanHandle(AttachmentInfo{Source: "UPLOADED_CONTENT"}) {
		t.Error("expected CanHandle to be false for UPLOADED_CONTENT without DriveFileID")
	}
	if strat.CanHandle(AttachmentInfo{Source: "OTHER"}) {
		t.Error("expected CanHandle to be false for OTHER")
	}
}

func TestDriveFileStrategy_Download(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "report.pdf")

	var calledID, calledTarget string
	clientMock := &mockDriveClient{
		downloadFunc: func(ctx context.Context, id, outPath string) (string, error) {
			calledID = id
			calledTarget = outPath
			return outPath, os.WriteFile(outPath, []byte("drive bytes"), 0644)
		},
	}

	strat := NewDriveFileStrategy(clientMock)
	att := AttachmentInfo{
		Source:      "DRIVE_FILE",
		DriveFileID: "drive-12345",
	}

	if err := strat.Download(context.Background(), att, targetPath); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if calledID != "drive-12345" || calledTarget != targetPath {
		t.Fatalf("unexpected call args: id=%q, target=%q", calledID, calledTarget)
	}

	// Test nil client error
	nilStrat := NewDriveFileStrategy(nil)
	if err := nilStrat.Download(context.Background(), att, targetPath); err == nil || !strings.Contains(err.Error(), "drive client not configured") {
		t.Fatalf("expected client not configured error, got: %v", err)
	}
}
