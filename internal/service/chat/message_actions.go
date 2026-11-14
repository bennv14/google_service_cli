package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

// WriteAPI is the slice of the Chat API required for message write operations.
// *Client implements it; unit tests supply a fake so write logic runs without HTTP.
type WriteAPI interface {
	GetSpace(ctx context.Context, name string) (*chatapi.Space, error)
	FindDirectMessage(ctx context.Context, userRef string) (*chatapi.Space, error)
	ListSpaces(ctx context.Context) ([]*chatapi.Space, error)
	GetMessage(ctx context.Context, name string) (*chatapi.Message, error)
	SpaceReadState(ctx context.Context, spaceName string) (string, time.Time, error)

	CreateMessage(ctx context.Context, parent string, msg *chatapi.Message, replyOption string) (*chatapi.Message, error)
	PatchMessage(ctx context.Context, name string, text string) (*chatapi.Message, error)
	DeleteMessage(ctx context.Context, name string) error
	UploadAttachment(ctx context.Context, parent, filePath string) (*chatapi.AttachmentDataRef, error)
	CreateReaction(ctx context.Context, parent, unicode string) (*chatapi.Reaction, error)
	ListReactions(ctx context.Context, parent, filter string) ([]*chatapi.Reaction, error)
	DeleteReaction(ctx context.Context, name string) error
}

var _ WriteAPI = (*Client)(nil)

// Writer executes Chat message write operations.
type Writer struct {
	api WriteAPI
	dir Directory
}

// NewWriter constructs a Writer backed by api and optional Directory dir.
func NewWriter(api WriteAPI, dir Directory) *Writer {
	return &Writer{api: api, dir: dir}
}

// SendOpts configures Writer.Send.
type SendOpts struct {
	Space       string
	Thread      string
	ReplyTo     string
	Text        string
	Attachments []string
	Progress    io.Writer
}

// DeleteOpts configures Writer.Delete.
type DeleteOpts struct {
	Space       string
	Yes         bool
	Interactive bool
	In          io.Reader
	ErrOut      io.Writer
}

// ReactOpts configures Writer.React.
type ReactOpts struct {
	Space  string
	Remove bool
}

// Send resolves the target space/thread, validates and uploads any attachments,
// sends the message verbatim, and returns it as a SingleMessage view.
func (w *Writer) Send(ctx context.Context, opt SendOpts) (SingleMessage, error) {
	if err := validateAttachments(opt.Attachments); err != nil {
		return SingleMessage{}, err
	}
	if strings.TrimSpace(opt.Text) == "" && len(opt.Attachments) == 0 {
		return SingleMessage{}, errors.New("message text cannot be empty (pass text, pipe stdin, or attach a file with --attach)")
	}

	spaceName, threadName, replyOption, err := w.resolveSendTarget(ctx, opt)
	if err != nil {
		return SingleMessage{}, err
	}

	var atts []*chatapi.Attachment
	for _, path := range opt.Attachments {
		if opt.Progress != nil {
			fmt.Fprintf(opt.Progress, "Uploading %s...\n", filepath.Base(filepath.Clean(path)))
		}
		ref, err := w.api.UploadAttachment(ctx, spaceName, path)
		if err != nil {
			return SingleMessage{}, err
		}
		atts = append(atts, &chatapi.Attachment{AttachmentDataRef: ref})
	}

	msg := &chatapi.Message{
		Text:       opt.Text,
		Attachment: atts,
	}
	if threadName != "" {
		msg.Thread = &chatapi.Thread{Name: threadName}
	}

	created, err := w.api.CreateMessage(ctx, spaceName, msg, replyOption)
	if err != nil {
		return SingleMessage{}, err
	}
	return w.toSingleMessage(ctx, created)
}

// Edit updates an existing message's text body using updateMask=text.
func (w *Writer) Edit(ctx context.Context, msgRef, spaceRef, text string) (SingleMessage, error) {
	if strings.TrimSpace(text) == "" {
		return SingleMessage{}, errors.New("message text cannot be empty")
	}
	msgName, _, err := w.resolveMessageName(ctx, msgRef, spaceRef)
	if err != nil {
		return SingleMessage{}, err
	}
	updated, err := w.api.PatchMessage(ctx, msgName, text)
	if err != nil {
		return SingleMessage{}, err
	}
	return w.toSingleMessage(ctx, updated)
}

// Delete removes a message after checking confirmation requirements.
func (w *Writer) Delete(ctx context.Context, msgRef string, opt DeleteOpts) (DeleteResult, error) {
	msgName, _, err := w.resolveMessageName(ctx, msgRef, opt.Space)
	if err != nil {
		return DeleteResult{}, err
	}

	if !opt.Yes {
		if !opt.Interactive {
			return DeleteResult{}, errors.New("confirmation required: pass --yes to delete in non-interactive mode")
		}
		ok, err := confirmDelete(msgName, opt.In, opt.ErrOut)
		if err != nil {
			return DeleteResult{}, err
		}
		if !ok {
			return DeleteResult{}, errors.New("aborted")
		}
	}

	if err := w.api.DeleteMessage(ctx, msgName); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{
		ID:      formatMessageID(msgName),
		Name:    msgName,
		Deleted: true,
	}, nil
}

// React adds or removes a Unicode emoji reaction on a message.
func (w *Writer) React(ctx context.Context, msgRef, emoji string, opt ReactOpts) (ReactionResult, error) {
	if strings.TrimSpace(emoji) == "" {
		return ReactionResult{}, errors.New("emoji cannot be empty")
	}
	msgName, _, err := w.resolveMessageName(ctx, msgRef, opt.Space)
	if err != nil {
		return ReactionResult{}, err
	}
	msgID := formatMessageID(msgName)

	if !opt.Remove {
		created, err := w.api.CreateReaction(ctx, msgName, emoji)
		if err != nil {
			return ReactionResult{}, err
		}
		var name string
		if created != nil {
			name = created.Name
		}
		return ReactionResult{
			Action:    "added",
			Emoji:     emoji,
			MessageID: msgID,
			Name:      name,
		}, nil
	}

	spaceName, ok := spaceFromMessageName(msgName)
	if !ok {
		return ReactionResult{}, fmt.Errorf("cannot determine space for message %s", msgName)
	}
	rsName, _, err := w.api.SpaceReadState(ctx, spaceName)
	if err != nil {
		return ReactionResult{}, err
	}
	meID, ok := parseUserID(rsName)
	if !ok {
		return ReactionResult{}, fmt.Errorf("the Chat API did not resolve users/me to a numeric ID (got %q)", rsName)
	}

	filter := fmt.Sprintf("emoji.unicode = %q", emoji)
	reactions, err := w.api.ListReactions(ctx, msgName, filter)
	if err != nil {
		return ReactionResult{}, err
	}
	var match *chatapi.Reaction
	for _, r := range reactions {
		if r != nil && r.User != nil && r.User.Name == meID && r.Emoji != nil && r.Emoji.Unicode == emoji {
			match = r
			break
		}
	}
	if match == nil {
		return ReactionResult{}, fmt.Errorf("no %s reaction from you found on message %s", emoji, msgID)
	}

	if err := w.api.DeleteReaction(ctx, match.Name); err != nil {
		return ReactionResult{}, err
	}
	return ReactionResult{
		Action:    "removed",
		Emoji:     emoji,
		MessageID: msgID,
		Name:      match.Name,
	}, nil
}

func (w *Writer) resolveSendTarget(ctx context.Context, opt SendOpts) (spaceName, threadName, replyOption string, err error) {
	if opt.ReplyTo != "" {
		msgName, spaceFromFlag, err := w.resolveMessageName(ctx, opt.ReplyTo, opt.Space)
		if err != nil {
			return "", "", "", err
		}
		msgSpace, msgThread, err := w.messageSpaceAndThread(ctx, msgName)
		if err != nil {
			return "", "", "", err
		}
		if spaceFromFlag != "" && spaceFromFlag != msgSpace {
			return "", "", "", fmt.Errorf("--reply-to target space %s conflicts with --space %s", msgSpace, spaceFromFlag)
		}
		if opt.Thread != "" {
			threadSpace, threadFromFlag, err := resolveThreadRef(opt.Thread, msgSpace)
			if err != nil {
				return "", "", "", err
			}
			if threadSpace != msgSpace || threadFromFlag != msgThread {
				return "", "", "", fmt.Errorf("--reply-to target thread %s conflicts with --thread %s", msgThread, threadFromFlag)
			}
		}
		return msgSpace, msgThread, "REPLY_MESSAGE_OR_FAIL", nil
	}

	var spaceFromFlag string
	if opt.Space != "" {
		sp, err := resolveSpace(ctx, w.api, opt.Space)
		if err != nil {
			return "", "", "", err
		}
		spaceFromFlag = sp.Name
	}

	if opt.Thread != "" {
		threadSpace, threadName, err := resolveThreadRef(opt.Thread, spaceFromFlag)
		if err != nil {
			return "", "", "", err
		}
		if spaceFromFlag != "" && threadSpace != spaceFromFlag {
			return "", "", "", fmt.Errorf("--thread space %s conflicts with --space %s", threadSpace, spaceFromFlag)
		}
		return threadSpace, threadName, "REPLY_MESSAGE_OR_FAIL", nil
	}

	if spaceFromFlag == "" {
		return "", "", "", errors.New("target required: pass --space, --thread, or --reply-to")
	}
	return spaceFromFlag, "", "", nil
}

func (w *Writer) resolveMessageName(ctx context.Context, msgRef, spaceRef string) (msgName, resolvedSpace string, err error) {
	if spaceRef != "" {
		if _, ok := splitSpaceName(spaceRef); ok {
			resolvedSpace = spaceRef
		} else {
			sp, err := resolveSpace(ctx, w.api, spaceRef)
			if err != nil {
				return "", "", err
			}
			resolvedSpace = sp.Name
		}
	}

	msgName, err = qualifyMessage(msgRef, resolvedSpace)
	if err != nil {
		// Allow canonical message resource names whose final segment does not
		// use {tid}.{mid} (e.g. client-assigned IDs) so --reply-to can fall
		// back to GetMessage.
		trimmed := strings.TrimSpace(msgRef)
		parts := strings.Split(trimmed, "/")
		if len(parts) == 4 && parts[0] == "spaces" && parts[1] != "" && parts[2] == "messages" && parts[3] != "" {
			return trimmed, resolvedSpace, nil
		}
		return "", "", err
	}
	return msgName, resolvedSpace, nil
}

func (w *Writer) messageSpaceAndThread(ctx context.Context, msgName string) (spaceName, threadName string, err error) {
	if sid, tid, _, ok := splitMessageName(msgName); ok {
		return "spaces/" + sid, "spaces/" + sid + "/threads/" + tid, nil
	}
	m, err := w.api.GetMessage(ctx, msgName)
	if err != nil {
		return "", "", err
	}
	if m.Space != nil {
		spaceName = m.Space.Name
	}
	if spaceName == "" {
		spaceName, _ = spaceFromMessageName(msgName)
	}
	if m.Thread != nil {
		threadName = m.Thread.Name
	}
	if spaceName == "" || threadName == "" {
		return "", "", fmt.Errorf("could not determine space and thread for message %s", msgName)
	}
	return spaceName, threadName, nil
}

func resolveThreadRef(ref, resolvedSpace string) (spaceName, threadName string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", errors.New("thread reference cannot be empty")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		u, err := url.Parse(ref)
		if err != nil {
			return "", "", fmt.Errorf("invalid thread URL %q: %w", ref, err)
		}
		parts := strings.FieldsFunc(strings.Trim(u.Path, "/"), func(r rune) bool { return r == '/' })
		for i, part := range parts {
			if part == "room" && len(parts) >= i+3 {
				sid, tid := parts[i+1], parts[i+2]
				return "spaces/" + sid, "spaces/" + sid + "/threads/" + tid, nil
			}
		}
		return "", "", fmt.Errorf("invalid chat thread URL %q: expected path containing /room/{spaceId}/{threadId}", ref)
	}
	if strings.HasPrefix(ref, "spaces/") {
		sid, _, ok := splitThreadName(ref)
		if !ok {
			return "", "", fmt.Errorf("invalid thread resource name %q: expected format spaces/{spaceId}/threads/{threadId}", ref)
		}
		return "spaces/" + sid, ref, nil
	}
	if strings.Contains(ref, "/") {
		return "", "", fmt.Errorf("invalid thread ID %q: expected spaces/{spaceId}/threads/{threadId}, URL, or short thread ID with --space", ref)
	}
	sid, ok := splitSpaceName(resolvedSpace)
	if !ok {
		return "", "", errors.New("--space is required when --thread is a short thread ID")
	}
	return resolvedSpace, "spaces/" + sid + "/threads/" + ref, nil
}

func spaceFromMessageName(msgName string) (string, bool) {
	parts := strings.Split(msgName, "/")
	if len(parts) == 4 && parts[0] == "spaces" && parts[1] != "" && parts[2] == "messages" && parts[3] != "" {
		return "spaces/" + parts[1], true
	}
	return "", false
}

func validateAttachments(paths []string) error {
	for _, p := range paths {
		cleaned := filepath.Clean(p)
		info, err := os.Stat(cleaned)
		if err != nil {
			return fmt.Errorf("attachment %q: %w", p, err)
		}
		if info.IsDir() {
			return fmt.Errorf("attachment %q is a directory, expected a file", p)
		}
		f, err := os.Open(cleaned)
		if err != nil {
			return fmt.Errorf("attachment %q: %w", p, err)
		}
		_ = f.Close()
	}
	return nil
}

func confirmDelete(msgName string, in io.Reader, errOut io.Writer) (bool, error) {
	id := formatMessageID(msgName)
	link := messageLink(msgName)
	if errOut != nil {
		if link != "" {
			fmt.Fprintf(errOut, "Delete message %s (%s)? [y/N]: ", id, link)
		} else {
			fmt.Fprintf(errOut, "Delete message %s? [y/N]: ", id)
		}
	}
	if in == nil {
		return false, nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

func (w *Writer) toSingleMessage(ctx context.Context, m *chatapi.Message) (SingleMessage, error) {
	if m == nil {
		return SingleMessage{}, errors.New("chat API returned an empty message")
	}
	var mi MessageInfo
	if m.CreateTime != "" {
		var ok bool
		mi, ok = messageInfo(m, time.Time{}, false, "")
		if !ok {
			return SingleMessage{}, fmt.Errorf("malformed message create time in message %s", m.Name)
		}
	} else {
		head, _ := isThreadHead(m.Name)
		mi = MessageInfo{
			ID:           formatMessageID(m.Name),
			RawName:      m.Name,
			ThreadID:     threadIDOf(m),
			IsThreadHead: head,
			Text:         m.Text,
			Mentions:     []string{},
			Link:         messageLink(m.Name),
			Attachments:  attachmentInfos(m.Attachment),
		}
		if m.Sender != nil {
			mi.Sender = Sender{
				ID:   m.Sender.Name,
				Name: m.Sender.DisplayName,
				Type: m.Sender.Type,
			}
			if mi.Sender.Name == "" {
				mi.Sender.Name = m.Sender.Name
			}
		}
	}

	var warnings []string
	if w.dir != nil && mi.Sender.ID != "" {
		resolved, err := w.dir.Lookup(ctx, []string{mi.Sender.ID})
		if err != nil {
			warnings = append(warnings, nameWarning(err))
		}
		if p, ok := resolved[mi.Sender.ID]; ok {
			if p.Name != "" {
				mi.Sender.Name = p.Name
			}
			if p.Email != "" {
				mi.Sender.Email = p.Email
			}
		}
		if c, ok := w.dir.(nameCache); ok {
			c.Flush()
		}
	}

	return SingleMessage{
		Message:  mi,
		Warnings: warnings,
	}, nil
}
