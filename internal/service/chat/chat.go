package chat

import (
	"context"
	"net/http"
	"time"

	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// The three read-only scopes this service ever requests. All are restricted
// scopes: in a Testing-mode OAuth consent screen the account must be listed as
// a test user, and the Chat API must be enabled in the client's GCP project.
var OAuthScopes = []string{
	chatapi.ChatSpacesReadonlyScope,
	chatapi.ChatMessagesReadonlyScope,
	chatapi.ChatUsersReadstateReadonlyScope,
}

// Field masks keep responses small and make the wire format explicit.
const (
	spaceFields   = "name,displayName,spaceType,spaceThreadingState,spaceUri"
	messageFields = "name,createTime,text,sender(name,displayName,type)," +
		"thread(name),annotations(type,userMention(type,user(name,displayName)))"
	pageSize = 1000
)

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
	for {
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

// ListMessages returns messages in a space matching filter, oldest-page-first,
// stopping once limit messages have been collected. limit <= 0 means unlimited.
func (c *Client) ListMessages(ctx context.Context, parent, filter string, limit int) ([]*chatapi.Message, error) {
	var out []*chatapi.Message
	token := ""
	for {
		size := int64(pageSize)
		if limit > 0 && int64(limit-len(out)) < size {
			size = int64(limit - len(out))
		}
		call := c.svc.Spaces.Messages.List(parent).Context(ctx).
			PageSize(size).
			Fields("messages(" + messageFields + "),nextPageToken")
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
		out = append(out, res.Messages...)
		if res.NextPageToken == "" || (limit > 0 && len(out) >= limit) {
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			return out, nil
		}
		token = res.NextPageToken
	}
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
		return rs.Name, time.Time{}, nil
	}
	return rs.Name, ts, nil
}

// ListMembers returns the memberships of a space, following all pages. It is
// only needed to name a DM whose window contains nothing but the caller's own
// messages.
func (c *Client) ListMembers(ctx context.Context, spaceName string) ([]*chatapi.Membership, error) {
	var out []*chatapi.Membership
	token := ""
	for {
		call := c.svc.Spaces.Members.List(spaceName).Context(ctx).
			PageSize(pageSize).
			Fields("memberships(name,member(name,displayName,type)),nextPageToken")
		if token != "" {
			call = call.PageToken(token)
		}
		res, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, res.Memberships...)
		if res.NextPageToken == "" {
			return out, nil
		}
		token = res.NextPageToken
	}
}
