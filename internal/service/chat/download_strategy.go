package chat

import (
	"context"
	"errors"
)

// DownloadStrategy defines the Strategy interface for downloading message attachments.
// Each strategy is self-selecting: CanHandle inspects attachment metadata to determine
// if it can process and download the asset.
type DownloadStrategy interface {
	// CanHandle returns true if this strategy knows how to download the given attachment.
	CanHandle(att AttachmentInfo) bool
	// Download executes the download and saves the content to targetPath.
	Download(ctx context.Context, att AttachmentInfo, targetPath string) error
}

var _ DownloadStrategy = (*ChatMediaStrategy)(nil)

// ChatMediaClient is implemented by *Client to download Chat uploaded media bytes.
type ChatMediaClient interface {
	DownloadMedia(ctx context.Context, resourceName, targetPath string) error
}

// ChatMediaStrategy implements DownloadStrategy for Google Chat uploaded content.
type ChatMediaStrategy struct {
	client ChatMediaClient
}

// NewChatMediaStrategy builds a new ChatMediaStrategy.
func NewChatMediaStrategy(client ChatMediaClient) *ChatMediaStrategy {
	return &ChatMediaStrategy{client: client}
}

// CanHandle returns true if the attachment originates from Chat uploaded media.
func (s *ChatMediaStrategy) CanHandle(att AttachmentInfo) bool {
	return att.Source == "UPLOADED_CONTENT" || att.ResourceName != ""
}

// Download downloads an uploaded Chat attachment via the Chat Media API.
func (s *ChatMediaStrategy) Download(ctx context.Context, att AttachmentInfo, targetPath string) error {
	if s.client == nil {
		return errors.New("chat media client not configured")
	}
	return s.client.DownloadMedia(ctx, att.ResourceName, targetPath)
}
