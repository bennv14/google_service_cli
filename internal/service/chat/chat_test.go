package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// newTestClient points a Client at an httptest server, mirroring the pattern in
// internal/service/drive/drive_test.go.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cl, err := NewClient(context.Background(), srv.Client(),
		option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

func TestOAuthScopesAreReadOnly(t *testing.T) {
	want := []string{
		"https://www.googleapis.com/auth/chat.spaces.readonly",
		"https://www.googleapis.com/auth/chat.messages.readonly",
		"https://www.googleapis.com/auth/chat.users.readstate.readonly",
	}
	if len(OAuthScopes) != len(want) {
		t.Fatalf("OAuthScopes = %v", OAuthScopes)
	}
	for i, w := range want {
		if OAuthScopes[i] != w {
			t.Fatalf("OAuthScopes[%d] = %q, want %q", i, OAuthScopes[i], w)
		}
	}
}

func TestListSpacesPaginates(t *testing.T) {
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spaces" {
			http.NotFound(w, r)
			return
		}
		calls++
		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spaces":        []map[string]any{{"name": "spaces/A", "displayName": "Alpha"}},
				"nextPageToken": "tok2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spaces": []map[string]any{{"name": "spaces/B", "displayName": "Beta"}},
		})
	})

	spaces, err := cl.ListSpaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests, got %d", calls)
	}
	if len(spaces) != 2 || spaces[0].Name != "spaces/A" || spaces[1].DisplayName != "Beta" {
		t.Fatalf("ListSpaces() = %+v", spaces)
	}
}

func TestListSpacesStopsOnRepeatingPageToken(t *testing.T) {
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spaces":        []map[string]any{{"name": "spaces/A"}},
			"nextPageToken": "same-tok",
		})
	})

	_, err := cl.ListSpaces(context.Background())
	if err == nil {
		t.Fatal("expected an error for a repeating page token, got nil")
	}
	if !strings.Contains(err.Error(), "repeating page token") {
		t.Fatalf("err = %q, want mention of a repeating page token", err.Error())
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 requests before detecting the repeat, got %d", calls)
	}
}

func TestListSpacesStopsAtMaxPages(t *testing.T) {
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spaces":        []map[string]any{{"name": "spaces/A"}},
			"nextPageToken": fmt.Sprintf("tok-%d", calls),
		})
	})

	_, err := cl.ListSpaces(context.Background())
	if err == nil {
		t.Fatal("expected an error once the page cap is exceeded, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d pages", maxPages)) {
		t.Fatalf("err = %q, want mention of the %d page cap", err.Error(), maxPages)
	}
	if calls != maxPages {
		t.Fatalf("expected exactly %d requests, got %d", maxPages, calls)
	}
}

func TestListMessagesStopsOnRepeatingPageToken(t *testing.T) {
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"name": "spaces/A/messages/t.1", "text": "one", "createTime": "2026-07-20T01:00:00Z"},
			},
			"nextPageToken": "same-tok",
		})
	})

	_, err := cl.ListMessages(context.Background(), "spaces/A", ListOpts{})
	if err == nil {
		t.Fatal("expected an error for a repeating page token, got nil")
	}
	if !strings.Contains(err.Error(), "repeating page token") {
		t.Fatalf("err = %q, want mention of a repeating page token", err.Error())
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 requests before detecting the repeat, got %d", calls)
	}
}

func TestListMessagesSendsFilterAndStopsAtLimit(t *testing.T) {
	const filter = `create_time > "2026-07-19T00:00:00Z"`
	var gotFilter string
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spaces/A/messages" {
			http.NotFound(w, r)
			return
		}
		calls++
		gotFilter = r.URL.Query().Get("filter")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"name": "spaces/A/messages/t.2", "text": "two", "createTime": "2026-07-20T02:00:00Z"},
				{"name": "spaces/A/messages/t.1", "text": "one", "createTime": "2026-07-20T01:00:00Z"},
			},
			"nextPageToken": "more",
		})
	})

	msgs, err := cl.ListMessages(context.Background(), "spaces/A", ListOpts{Filter: filter, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if gotFilter != filter {
		t.Fatalf("filter = %q, want %q", gotFilter, filter)
	}
	if calls != 1 {
		t.Fatalf("limit reached but client kept paginating: %d calls", calls)
	}
	if len(msgs) != 2 || msgs[1].Text != "two" {
		t.Fatalf("ListMessages() = %+v", msgs)
	}
}

// The Chat API orders messages `create_time ASC` by default, so a limited
// request must ask for DESC or it silently returns the OLDEST n messages.
func TestListMessagesLimitTakesTheNewestNotTheOldest(t *testing.T) {
	var gotOrder string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotOrder = r.URL.Query().Get("orderBy")
		// A DESC-ordered server hands back the newest page first.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"name": "spaces/A/messages/t.5", "text": "newest", "createTime": "2026-07-20T05:00:00Z"},
				{"name": "spaces/A/messages/t.4", "text": "newer", "createTime": "2026-07-20T04:00:00Z"},
			},
			"nextPageToken": "older",
		})
	})

	msgs, err := cl.ListMessages(context.Background(), "spaces/A", ListOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if gotOrder != "createTime DESC" {
		t.Fatalf("orderBy = %q, want %q so that --limit cuts the newest end", gotOrder, "createTime DESC")
	}
	// Callers downstream expect oldest-first, so the newest page is reversed.
	if len(msgs) != 2 || msgs[0].Text != "newer" || msgs[1].Text != "newest" {
		t.Fatalf("ListMessages() = %+v, want oldest-first [newer newest]", msgs)
	}
}

// Limit counts messages that survived Keep, so a client-side filter cannot be
// starved by a limit that was spent on messages the caller discards.
func TestListMessagesLimitCountsKeptMessages(t *testing.T) {
	var calls int
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{
					{"name": "spaces/A/messages/t.5", "text": "skip", "createTime": "2026-07-20T05:00:00Z"},
					{"name": "spaces/A/messages/t.4", "text": "keep me", "createTime": "2026-07-20T04:00:00Z"},
					{"name": "spaces/A/messages/t.3", "text": "skip", "createTime": "2026-07-20T03:00:00Z"},
				},
				"nextPageToken": "p2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"name": "spaces/A/messages/t.2", "text": "keep me too", "createTime": "2026-07-20T02:00:00Z"},
				{"name": "spaces/A/messages/t.1", "text": "skip", "createTime": "2026-07-20T01:00:00Z"},
			},
		})
	})

	msgs, err := cl.ListMessages(context.Background(), "spaces/A", ListOpts{
		Limit: 2,
		Keep:  func(m *chatapi.Message) bool { return strings.HasPrefix(m.Text, "keep") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected the client to page on until 2 kept messages, got %d calls", calls)
	}
	if len(msgs) != 2 || msgs[0].Text != "keep me too" || msgs[1].Text != "keep me" {
		t.Fatalf("ListMessages() = %+v, want the two kept messages oldest-first", msgs)
	}
}

func TestSpaceReadStateReturnsCanonicalNameAndTime(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users/me/spaces/A/spaceReadState" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":         "users/108812449/spaces/A/spaceReadState",
			"lastReadTime": "2026-07-25T15:45:00Z",
		})
	})

	name, ts, err := cl.SpaceReadState(context.Background(), "spaces/A")
	if err != nil {
		t.Fatal(err)
	}
	if name != "users/108812449/spaces/A/spaceReadState" {
		t.Fatalf("name = %q", name)
	}
	if ts.UTC().Format("2006-01-02T15:04:05Z") != "2026-07-25T15:45:00Z" {
		t.Fatalf("lastReadTime = %v", ts)
	}
}

func TestSpaceReadStateToleratesMissingTime(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "users/1/spaces/A/spaceReadState",
		})
	})
	_, ts, err := cl.SpaceReadState(context.Background(), "spaces/A")
	if err != nil {
		t.Fatalf("a never-read space must not be an error: %v", err)
	}
	if !ts.IsZero() {
		t.Fatalf("lastReadTime = %v, want zero", ts)
	}
}

func TestSpaceReadStateRejectsMalformedTime(t *testing.T) {
	const badTime = "not-a-timestamp"
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":         "users/1/spaces/A/spaceReadState",
			"lastReadTime": badTime,
		})
	})
	_, ts, err := cl.SpaceReadState(context.Background(), "spaces/A")
	if err == nil {
		t.Fatal("expected an error for a malformed lastReadTime, got nil")
	}
	if !strings.Contains(err.Error(), badTime) {
		t.Fatalf("err = %q, want it to quote %q", err.Error(), badTime)
	}
	if !strings.Contains(err.Error(), "spaces/A") {
		t.Fatalf("err = %q, want it to name the space", err.Error())
	}
	if !ts.IsZero() {
		t.Fatalf("lastReadTime = %v, want zero on error", ts)
	}
}

func TestFindDirectMessageAndGetSpaceAndMembers(t *testing.T) {
	var dmName string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/spaces:findDirectMessage":
			dmName = r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "spaces/DM1", "spaceType": "DIRECT_MESSAGE",
			})
		case "/v1/spaces/A":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "spaces/A", "displayName": "Alpha", "spaceType": "SPACE",
			})
		case "/v1/spaces/DM1/members":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memberships": []map[string]any{
					{"name": "spaces/DM1/members/1", "member": map[string]any{"name": "users/1", "displayName": "Linh Tran"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	dm, err := cl.FindDirectMessage(ctx, "users/linh@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if dmName != "users/linh@example.com" || dm.Name != "spaces/DM1" {
		t.Fatalf("name param = %q, space = %+v", dmName, dm)
	}

	sp, err := cl.GetSpace(ctx, "spaces/A")
	if err != nil {
		t.Fatal(err)
	}
	if sp.DisplayName != "Alpha" {
		t.Fatalf("GetSpace = %+v", sp)
	}

	mem, err := cl.ListMembers(ctx, "spaces/DM1")
	if err != nil {
		t.Fatal(err)
	}
	if len(mem) != 1 || mem[0].Member.DisplayName != "Linh Tran" {
		t.Fatalf("ListMembers = %+v", mem)
	}
}
