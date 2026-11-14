package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

func TestClientSatisfiesAPI(t *testing.T) {
	var _ API = (*Client)(nil)
}

func TestRunFansOutAndOrdersSpacesByRecency(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha", SpaceType: "SPACE", SpaceThreadingState: "THREADED_MESSAGES"},
			{Name: "spaces/B", DisplayName: "Beta", SpaceType: "SPACE", SpaceThreadingState: "THREADED_MESSAGES"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			switch parent {
			case "spaces/A":
				return []*chatapi.Message{
					rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "old", "2026-07-20T01:00:00Z"),
				}, nil
			case "spaces/B":
				return []*chatapi.Message{
					rawMsg("spaces/B/messages/t9.t9", "spaces/B/threads/t9", "users/2", "Huy", "new", "2026-07-25T09:00:00Z"),
				}, nil
			}
			return nil, nil
		},
	}

	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spaces) != 2 {
		t.Fatalf("got %d spaces", len(res.Spaces))
	}
	if res.Spaces[0].Space.Name != "Beta" {
		t.Fatalf("most recently active space must come first, got %q", res.Spaces[0].Space.Name)
	}
	if res.Summary.Spaces != 2 || res.Summary.Messages != 2 || res.Summary.Threads != 2 {
		t.Fatalf("summary = %+v", res.Summary)
	}
}

func TestRunAppliesDefaultWindowWhenScanningEverySpace(t *testing.T) {
	now := fixedTime(t, "2026-07-26T00:00:00Z")
	var gotFilter string
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(_ string, o ListOpts) ([]*chatapi.Message, error) {
			gotFilter = o.Filter
			return nil, nil
		},
	}
	if _, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: now}); err != nil {
		t.Fatal(err)
	}
	if want := `create_time > "2026-07-19T00:00:00Z"`; gotFilter != want {
		t.Fatalf("filter = %q, want %q", gotFilter, want)
	}
}

func TestRunSkipsDefaultWindowForSingleSpaceAndForUnread(t *testing.T) {
	now := fixedTime(t, "2026-07-26T00:00:00Z")

	var singleFilter string
	single := &fakeAPI{
		t:        t,
		getSpace: func(name string) (*chatapi.Space, error) { return &chatapi.Space{Name: name}, nil },
		messages: func(_ string, o ListOpts) ([]*chatapi.Message, error) {
			singleFilter = o.Filter
			return nil, nil
		},
	}
	if _, err := NewEngine(single, nullDirectory{}).Run(context.Background(), Query{Space: "spaces/A", Now: now}); err != nil {
		t.Fatal(err)
	}
	if singleFilter != "" {
		t.Fatalf("an explicit --space must not get a default window, got %q", singleFilter)
	}

	var unreadFilter string
	unread := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A"}},
		readState: readStateFor(map[string]string{"spaces/A": "2026-07-01T00:00:00Z"}),
		messages: func(_ string, o ListOpts) ([]*chatapi.Message, error) {
			unreadFilter = o.Filter
			return nil, nil
		},
	}
	if _, err := NewEngine(unread, nullDirectory{}).Run(context.Background(), Query{UnreadOnly: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	if want := `create_time > "2026-07-01T00:00:00Z"`; unreadFilter != want {
		t.Fatalf("--unread must use lastReadTime, not the 7-day default: got %q, want %q", unreadFilter, want)
	}
}

func TestRunUsesPerSpaceReadMarkers(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		readState: readStateFor(map[string]string{
			"spaces/A": "2026-07-20T00:00:00Z",
			"spaces/B": "2026-07-24T00:00:00Z",
		}),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) { return nil, nil },
	}
	if _, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{UnreadOnly: true, Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	fa, _ := api.msgFilters.Load("spaces/A")
	fb, _ := api.msgFilters.Load("spaces/B")
	if fa != `create_time > "2026-07-20T00:00:00Z"` || fb != `create_time > "2026-07-24T00:00:00Z"` {
		t.Fatalf("each space needs its own marker; got A=%v B=%v", fa, fb)
	}
}

func TestUnreadExcludesYourOwnMessages(t *testing.T) {
	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		readState: readStateFor(map[string]string{"spaces/A": "2026-07-25T00:00:00Z"}),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "theirs", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:05:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{UnreadOnly: true, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("your own message must not count as unread: %+v", res.Summary)
	}
	got := res.Spaces[0].Threads[0].Messages[0]
	if got.Text != "theirs" || !got.Unread {
		t.Fatalf("message = %+v", got)
	}
	if res.Spaces[0].Space.UnreadCount != 1 {
		t.Fatalf("UnreadCount = %d", res.Spaces[0].Space.UnreadCount)
	}
}

func TestMentionsMeFiltersClientSide(t *testing.T) {
	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		readState: readStateFor(nil),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "hi all", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "hi ben", "2026-07-25T09:01:00Z", meUser),
				rawMsg("spaces/A/messages/t1.t3", "spaces/A/threads/t1", "users/1", "Linh", "hi huy", "2026-07-25T09:02:00Z", "users/999"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{MentionsMe: true, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("expected 1 message, got %+v", res.Summary)
	}
	m := res.Spaces[0].Threads[0].Messages[0]
	if m.Text != "hi ben" || !m.MentionsMe || len(m.Mentions) != 1 {
		t.Fatalf("message = %+v", m)
	}
}

func TestThreadIsPartialWhenItsHeadIsOutsideTheWindow(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				// t1 includes its head (mid == tid); t2 does not.
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "start", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/A/messages/t2.zz", "spaces/A/threads/t2", "users/2", "Huy", "tail", "2026-07-25T10:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ThreadInfo{}
	for _, tg := range res.Spaces[0].Threads {
		byID[tg.Thread.ID] = tg.Thread
	}
	if byID["spaces/A/threads/t1"].Partial {
		t.Error("t1 has its head and must not be partial")
	}
	if !byID["spaces/A/threads/t2"].Partial {
		t.Error("t2 is missing its head and must be partial")
	}
}

func TestOneFailingSpaceDoesNotKillTheCommand(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/BAD", DisplayName: "Broken"},
			{Name: "spaces/OK", DisplayName: "Fine"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			if parent == "spaces/BAD" {
				return nil, errors.New("403 forbidden")
			}
			return []*chatapi.Message{
				rawMsg("spaces/OK/messages/t1.t1", "spaces/OK/threads/t1", "users/1", "Linh", "still here", "2026-07-25T00:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatalf("one bad space must not fail the whole run: %v", err)
	}
	if len(res.Spaces) != 1 || res.Spaces[0].Space.Name != "Fine" {
		t.Fatalf("spaces = %+v", res.Spaces)
	}
	if len(res.Errors) != 1 || res.Errors[0].SpaceID != "spaces/BAD" {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

// A space with a read marker of zero has never been opened, so everything in it
// is unread. The scan still needs a lower bound, or it drags in all history.
func TestUnreadTreatsNeverReadSpaceAsFullyUnreadWithinDefaultWindow(t *testing.T) {
	var gotFilter string
	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		readState: readStateFor(nil), // never read: zero time, no error
		messages: func(_ string, o ListOpts) ([]*chatapi.Message, error) {
			gotFilter = o.Filter
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		UnreadOnly: true, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `create_time > "2026-07-19T00:00:00Z"`; gotFilter != want {
		t.Fatalf("a space with no read marker must still be bounded: filter = %q, want %q", gotFilter, want)
	}
	if res.Summary.Unread != 1 {
		t.Fatalf("a never-read space has no read messages in it: %+v", res.Summary)
	}
}

// Without a read marker there is no way to tell read from unread, so a failed
// read-state lookup has to be reported rather than silently scanned for nothing.
func TestUnreadReportsSpacesWhoseReadStateFailed(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		readState: func(spaceName string) (string, time.Time, error) {
			if spaceName == "spaces/B" {
				return "", time.Time{}, errors.New("readstate boom")
			}
			return readStateFor(map[string]string{"spaces/A": "2026-07-20T00:00:00Z"})(spaceName)
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			if parent == "spaces/B" {
				t.Error("a space with no usable read marker must not be scanned")
			}
			return nil, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		UnreadOnly: true, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 1 || res.Errors[0].SpaceID != "spaces/B" {
		t.Fatalf("expected one reported failure for spaces/B, got %+v", res.Errors)
	}
	if !strings.Contains(res.Errors[0].Err.Error(), "readstate boom") {
		t.Fatalf("the underlying error must survive: %v", res.Errors[0].Err)
	}
}

func TestEngineMessageFetchesAndResolvesSingleMessage(t *testing.T) {
	api := &fakeAPI{
		getMessage: func(name string) (*chatapi.Message, error) {
			if name != "spaces/AAAA/messages/t1.m1" {
				return nil, fmt.Errorf("not found: %s", name)
			}
			return &chatapi.Message{
				Name:       "spaces/AAAA/messages/t1.m1",
				CreateTime: "2026-08-19T12:00:00Z",
				Text:       "Single msg test",
				Sender:     &chatapi.User{Name: "users/101", DisplayName: "Short Name", Type: "HUMAN"},
			}, nil
		},
	}
	dir := &fakeDirectory{
		people: map[string]Person{
			"users/101": {Name: "Full Resolved Name", Email: "person@example.com"},
		},
	}

	eng := NewEngine(api, dir)
	sm, err := eng.Message(context.Background(), "spaces/AAAA/messages/t1.m1", "0")
	if err != nil {
		t.Fatalf("Engine.Message error: %v", err)
	}
	if sm.Message.ID != "AAAA/t1/m1" {
		t.Errorf("sm.Message.ID = %q, want AAAA/t1/m1", sm.Message.ID)
	}
	if sm.Message.Sender.Name != "Full Resolved Name" {
		t.Errorf("sender name = %q, want Full Resolved Name", sm.Message.Sender.Name)
	}
	if sm.Message.Sender.Email != "person@example.com" {
		t.Errorf("sender email = %q, want person@example.com", sm.Message.Sender.Email)
	}
}

func TestParseUserID(t *testing.T) {
	cases := []struct {
		in     string
		wantID string
		wantOK bool
	}{
		{"users/123456789/spaces/A/spaceReadState", "users/123456789", true},
		{"users/me/spaces/A/spaceReadState", "", false},
		{"users/", "", false},
		{"spaces/A", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		id, ok := parseUserID(c.in)
		if id != c.wantID || ok != c.wantOK {
			t.Errorf("parseUserID(%q) = (%q, %v), want (%q, %v)", c.in, id, ok, c.wantID, c.wantOK)
		}
	}
}
