package chat

import (
	"context"
	"fmt"
	"net/http"
	"time"

	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
	peopleapi "google.golang.org/api/people/v1"
)

// The four read-only scopes this service ever requests. The three Chat scopes
// are restricted: in a Testing-mode OAuth consent screen the account must be
// listed as a test user, and the Chat API must be enabled in the client's GCP
// project. directory.readonly is what turns users/123456789 into a person's
// name — the Chat API itself only ever returns the ID.
var OAuthScopes = []string{
	chatapi.ChatSpacesReadonlyScope,
	chatapi.ChatMessagesReadonlyScope,
	chatapi.ChatUsersReadstateReadonlyScope,
	peopleapi.DirectoryReadonlyScope,
}

// Field masks keep responses small and make the wire format explicit.
const (
	spaceFields   = "name,displayName,spaceType,spaceThreadingState,spaceUri"
	messageFields = "name,createTime,text,sender(name,displayName,type)," +
		"thread(name),annotations(type,userMention(type,user(name,displayName)))"
	pageSize = 1000
)

// maxPages is a runaway guard on the pagination loops below, not a Chat API
// limit: the API imposes no page-count cap of its own. It exists so that a
// server bug (or a hostile server) returning an endless stream of distinct
// page tokens can't grow out's backing array without bound.
const maxPages = 100

// Client wraps the Chat v1 API. It is read-only by construction: no method here
// calls a mutating endpoint.
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
