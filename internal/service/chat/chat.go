package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
	peopleapi "google.golang.org/api/people/v1"
)

// OAuthScopes lists the scopes requested for Google Chat and People directory
// resolution. ChatMessagesScope enables sending, editing, deleting messages,
// uploading attachments, and managing reactions.
var OAuthScopes = []string{
	chatapi.ChatSpacesReadonlyScope,
	chatapi.ChatMessagesReadonlyScope,
	chatapi.ChatMessagesScope,
	chatapi.ChatUsersReadstateReadonlyScope,
	peopleapi.DirectoryReadonlyScope,
}

// Field masks keep responses small and make the wire format explicit.
const (
	spaceFields   = "name,displayName,spaceType,spaceThreadingState,spaceUri"
	messageFields = "name,createTime,text,sender(name,displayName,type)," +
		"thread(name),annotations(type,userMention(type,user(name,displayName)))," +
		"attachment(name,contentName,contentType,source,attachmentDataRef(resourceName),driveDataRef(driveFileId),downloadUri)"
	pageSize         = 1000
	reactionPageSize = 100

	// probeFields is all the newest-message probe needs: who sent it.
	probeFields = "sender(name)"
)

// maxPages is a runaway guard on the pagination loops below, not a Chat API
// limit: the API imposes no page-count cap of its own. It exists so that a
// server bug (or a hostile server) returning an endless stream of distinct
// page tokens can't grow out's backing array without bound.
const maxPages = 100

// Client wraps the Chat v1 API.
type Client struct{ svc *chatapi.Service }

// NewClient builds a Chat client. Extra options are used by tests
// (e.g. option.WithEndpoint, option.WithoutAuthentication).
func NewClient(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	all := append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := chatapi.NewService(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

// ListSpaces returns every space the caller is a member of, following all pages.
func (c *Client) ListSpaces(ctx context.Context) ([]*chatapi.Space, error) {
	var out []*chatapi.Space
	token := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("chat: list spaces exceeded %d pages; refusing to keep paging", maxPages)
		}
		call := c.svc.Spaces.List().Context(ctx).
			PageSize(pageSize).
			Fields("spaces(" + spaceFields + "),nextPageToken")
		if token != "" {
			call = call.PageToken(token)
		}
		res, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, res.Spaces...)
		if res.NextPageToken == "" {
			return out, nil
		}
		if res.NextPageToken == token {
			return nil, fmt.Errorf("chat: list spaces got a repeating page token %q; refusing to keep paging", token)
		}
		token = res.NextPageToken
	}
}

// GetSpace fetches one space by resource name ("spaces/{id}").
func (c *Client) GetSpace(ctx context.Context, name string) (*chatapi.Space, error) {
	return c.svc.Spaces.Get(name).Context(ctx).Fields(spaceFields).Do()
}

// FindDirectMessage returns the 1:1 DM space with a user. userRef is
// "users/{email}" or "users/{id}".
func (c *Client) FindDirectMessage(ctx context.Context, userRef string) (*chatapi.Space, error) {
	return c.svc.Spaces.FindDirectMessage().Context(ctx).Name(userRef).Fields(spaceFields).Do()
}

// ListOpts configures ListMessages.
type ListOpts struct {
	// Filter is the server-side messages.list filter expression.
	Filter string
	// Keep, when non-nil, decides which messages the caller actually wants. It
	// runs inside the pagination loop so that Limit counts messages that
	// survived it: a filter the API cannot express server-side (mentions,
	// unread) must not be starved by a limit spent on discarded messages.
	Keep func(*chatapi.Message) bool
	// Limit caps the number of kept messages; <= 0 means unlimited.
	Limit int
}

// ListMessages returns messages in a space matching o.Filter, oldest first,
// stopping once o.Limit kept messages have been collected.
//
// Pages are requested newest-first. The API's own default is `create_time ASC`,
// which would make a limited request return the OLDEST messages in the window —
// the opposite of what every command here wants.
func (c *Client) ListMessages(ctx context.Context, parent string, o ListOpts) ([]*chatapi.Message, error) {
	var out []*chatapi.Message
	token := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("chat: list messages exceeded %d pages; refusing to keep paging", maxPages)
		}
		size := int64(pageSize)
		// Shrinking the last page only works when every message is kept: with a
		// Keep predicate, how many messages are still wanted says nothing about
		// how many must be fetched to find them.
		if o.Keep == nil && o.Limit > 0 && int64(o.Limit-len(out)) < size {
			size = int64(o.Limit - len(out))
		}
		call := c.svc.Spaces.Messages.List(parent).Context(ctx).
			PageSize(size).
			OrderBy("createTime DESC").
			Fields("messages(" + messageFields + "),nextPageToken")
		if o.Filter != "" {
			call = call.Filter(o.Filter)
		}
		if token != "" {
			call = call.PageToken(token)
		}
		res, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, m := range res.Messages {
			if o.Keep != nil && !o.Keep(m) {
				continue
			}
			out = append(out, m)
			if o.Limit > 0 && len(out) >= o.Limit {
				return oldestFirst(out), nil
			}
		}
		if res.NextPageToken == "" {
			return oldestFirst(out), nil
		}
		if res.NextPageToken == token {
			return nil, fmt.Errorf("chat: list messages got a repeating page token %q; refusing to keep paging", token)
		}
		token = res.NextPageToken
	}
}

// oldestFirst reverses newest-first pages back into the chronological order the
// rest of the package works in.
func oldestFirst(ms []*chatapi.Message) []*chatapi.Message {
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
	return ms
}

// SpaceReadState returns the canonical resource name of the caller's read state
// for a space, plus the time they last read it. A space that has never been read
// yields a zero time and no error.
func (c *Client) SpaceReadState(ctx context.Context, spaceName string) (string, time.Time, error) {
	rs, err := c.svc.Users.Spaces.GetSpaceReadState("users/me/" + spaceName + "/spaceReadState").
		Context(ctx).Do()
	if err != nil {
		return "", time.Time{}, err
	}
	if rs.LastReadTime == "" {
		return rs.Name, time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, rs.LastReadTime)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("chat: space %s has a malformed lastReadTime %q: %w", spaceName, rs.LastReadTime, err)
	}
	return rs.Name, ts, nil
}

// LatestMessages returns up to n of a space's newest messages, newest first.
// messages.list defaults to create_time ASC, so without the explicit ordering
// this would hand back the oldest messages in the space instead.
func (c *Client) LatestMessages(ctx context.Context, parent string, n int) ([]*chatapi.Message, error) {
	res, err := c.svc.Spaces.Messages.List(parent).Context(ctx).
		PageSize(int64(n)).
		OrderBy("create_time DESC").
		Fields("messages(" + probeFields + ")").
		Do()
	if err != nil {
		return nil, err
	}
	return res.Messages, nil
}

// GetMessage fetches one message by resource name
// ("spaces/{sid}/messages/{tid}.{mid}"). It reuses messageFields so the result
// converts through messageInfo() exactly like a listed message would. The Chat
// API has no batch-get, so N messages cost N requests.
func (c *Client) GetMessage(ctx context.Context, name string) (*chatapi.Message, error) {
	return c.svc.Spaces.Messages.Get(name).Context(ctx).Fields(messageFields).Do()
}

// DownloadMedia fetches an uploaded attachment's raw byte stream by its resource name
// and writes it to targetPath. If an error occurs during transfer, any partially written
// file at targetPath is removed.
func (c *Client) DownloadMedia(ctx context.Context, resourceName, targetPath string) (err error) {
	resp, err := c.svc.Media.Download(resourceName).Context(ctx).Download()
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := out.Close()
		if err != nil {
			_ = os.Remove(targetPath)
		} else if closeErr != nil {
			err = closeErr
		}
	}()

	if _, err = io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

// CreateMessage sends a new message in parent ("spaces/{id}"). When replyOption
// is non-empty (e.g. "REPLY_MESSAGE_OR_FAIL"), it is passed as messageReplyOption.
func (c *Client) CreateMessage(ctx context.Context, parent string, msg *chatapi.Message, replyOption string) (*chatapi.Message, error) {
	call := c.svc.Spaces.Messages.Create(parent, msg).Context(ctx).Fields(messageFields)
	if replyOption != "" {
		call = call.MessageReplyOption(replyOption)
	}
	return call.Do()
}

// PatchMessage updates the text body of an existing message using updateMask=text
// so existing attachments and metadata are preserved.
func (c *Client) PatchMessage(ctx context.Context, name string, text string) (*chatapi.Message, error) {
	return c.svc.Spaces.Messages.Patch(name, &chatapi.Message{Text: text}).
		UpdateMask("text").
		Context(ctx).
		Fields(messageFields).
		Do()
}

// DeleteMessage deletes a message by resource name ("spaces/{sid}/messages/{mid}").
func (c *Client) DeleteMessage(ctx context.Context, name string) error {
	_, err := c.svc.Spaces.Messages.Delete(name).Context(ctx).Do()
	return err
}

// UploadAttachment uploads a local file into parent ("spaces/{id}") and returns
// its AttachmentDataRef for attaching to a message. Only the sanitized base
// filename is sent to the Chat API.
func (c *Client) UploadAttachment(ctx context.Context, parent, filePath string) (*chatapi.AttachmentDataRef, error) {
	cleaned := filepath.Clean(filePath)
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	baseName := filepath.Base(cleaned)
	res, err := c.svc.Media.Upload(parent, &chatapi.UploadAttachmentRequest{
		Filename: baseName,
	}).Media(f).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	if res == nil || res.AttachmentDataRef == nil {
		return nil, fmt.Errorf("chat: upload %s returned no attachmentDataRef", baseName)
	}
	return res.AttachmentDataRef, nil
}

// CreateReaction adds a Unicode emoji reaction to parent ("spaces/{sid}/messages/{mid}").
func (c *Client) CreateReaction(ctx context.Context, parent, unicode string) (*chatapi.Reaction, error) {
	return c.svc.Spaces.Messages.Reactions.Create(parent, &chatapi.Reaction{
		Emoji: &chatapi.Emoji{Unicode: unicode},
	}).Context(ctx).Do()
}

// ListReactions returns all reactions on parent matching filter, following all pages.
func (c *Client) ListReactions(ctx context.Context, parent, filter string) ([]*chatapi.Reaction, error) {
	var out []*chatapi.Reaction
	token := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("chat: list reactions exceeded %d pages; refusing to keep paging", maxPages)
		}
		call := c.svc.Spaces.Messages.Reactions.List(parent).Context(ctx).
			PageSize(reactionPageSize)
		if filter != "" {
			call = call.Filter(filter)
		}
		if token != "" {
			call = call.PageToken(token)
		}
		res, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, res.Reactions...)
		if res.NextPageToken == "" {
			return out, nil
		}
		if res.NextPageToken == token {
			return nil, fmt.Errorf("chat: list reactions got a repeating page token %q; refusing to keep paging", token)
		}
		token = res.NextPageToken
	}
}

// DeleteReaction deletes a reaction by resource name
// ("spaces/{sid}/messages/{mid}/reactions/{rid}").
func (c *Client) DeleteReaction(ctx context.Context, name string) error {
	_, err := c.svc.Spaces.Messages.Reactions.Delete(name).Context(ctx).Do()
	return err
}

