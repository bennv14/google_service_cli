package chat

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 7, 26, 14, 30, 0, 0, time.FixedZone("ICT", 7*3600))

	cases := []struct {
		in   string
		want time.Time
	}{
		{"", time.Time{}},
		{"30m", now.Add(-30 * time.Minute)},
		{"12h", now.Add(-12 * time.Hour)},
		{"3d", now.Add(-72 * time.Hour)},
		// A bare date is local midnight, not UTC midnight.
		{"2026-07-25", time.Date(2026, 7, 25, 0, 0, 0, 0, time.Local)},
		{"2026-07-25T09:12:04+07:00", time.Date(2026, 7, 25, 9, 12, 4, 0, time.FixedZone("", 7*3600))},
	}
	for _, c := range cases {
		got, err := parseWhen(c.in, now)
		if err != nil {
			t.Errorf("parseWhen(%q) error: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseWhen(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseWhenRejectsGarbage(t *testing.T) {
	now := time.Now()
	for _, in := range []string{"yesterday", "3", "d3", "3w", "-2d", "2026-13-99"} {
		if _, err := parseWhen(in, now); err == nil {
			t.Errorf("parseWhen(%q) should have failed", in)
		}
	}
}

func TestParseWhenErrorMentionsAcceptedForms(t *testing.T) {
	_, err := parseWhen("yesterday", time.Now())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"3d", "2026-07-25", "RFC3339"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	now := time.Date(2026, 7, 26, 14, 30, 0, 0, time.UTC)
	cases := []struct {
		since, until time.Time
		want         string
	}{
		{time.Time{}, time.Time{}, "all time"},
		{now.Add(-48 * time.Hour), time.Time{}, "2 days"},
		{now.Add(-24 * time.Hour), time.Time{}, "1 day"},
		{now.Add(-3 * time.Hour), time.Time{}, "3 hours"},
		{now.Add(-30 * time.Minute), time.Time{}, "30 minutes"},
		{
			time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
			"2026-07-01 00:00 → 2026-07-02 00:00",
		},
		{time.Time{}, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), "until 2026-07-02 00:00"},
		// Boundary tests: minutes/hours threshold
		{now.Add(-59*time.Minute - 29*time.Second), time.Time{}, "59 minutes"},
		{now.Add(-59*time.Minute - 30*time.Second), time.Time{}, "1 hour"},
		// Boundary tests: hours/days threshold
		{now.Add(-23*time.Hour - 29*time.Minute), time.Time{}, "23 hours"},
		{now.Add(-23*time.Hour - 30*time.Minute), time.Time{}, "1 day"},
		{now.Add(-23*time.Hour - 59*time.Minute), time.Time{}, "1 day"},
	}
	for _, c := range cases {
		if got := windowLabel(c.since, c.until, now); got != c.want {
			t.Errorf("windowLabel(%v,%v) = %q, want %q", c.since, c.until, got, c.want)
		}
	}
}

// fakeAPI implements API with per-method hooks. A nil hook means "not expected
// to be called" and fails the test loudly rather than returning zero values.
type fakeAPI struct {
	t          *testing.T
	spaces     []*chatapi.Space
	getSpace   func(name string) (*chatapi.Space, error)
	findDM     func(userRef string) (*chatapi.Space, error)
	messages   func(parent string, o ListOpts) ([]*chatapi.Message, error)
	latest     func(parent string, n int) ([]*chatapi.Message, error)
	readState  func(spaceName string) (string, time.Time, error)
	getMessage func(name string) (*chatapi.Message, error)
	msgMu      sync.Mutex
	gotHeads   []string
	listErr    error
	listCalls  int32
	msgFilters sync.Map // parent -> filter, for asserting per-space filters
}

func (f *fakeAPI) ListSpaces(context.Context) ([]*chatapi.Space, error) {
	atomic.AddInt32(&f.listCalls, 1)
	return f.spaces, f.listErr
}

func (f *fakeAPI) GetSpace(_ context.Context, name string) (*chatapi.Space, error) {
	if f.getSpace == nil {
		f.t.Fatalf("unexpected GetSpace(%q)", name)
	}
	return f.getSpace(name)
}

func (f *fakeAPI) FindDirectMessage(_ context.Context, userRef string) (*chatapi.Space, error) {
	if f.findDM == nil {
		f.t.Fatalf("unexpected FindDirectMessage(%q)", userRef)
	}
	return f.findDM(userRef)
}

// ListMessages enforces the same contract as *Client, so the engine is never
// tested against a fake that is more generous than the real API: hooks return
// a space's messages oldest-first, and this applies Keep and then Limit to the
// NEWEST end, exactly as a `createTime DESC` request truncated at Limit does.
func (f *fakeAPI) ListMessages(_ context.Context, parent string, o ListOpts) ([]*chatapi.Message, error) {
	f.msgFilters.Store(parent, o.Filter)
	if f.messages == nil {
		f.t.Fatalf("unexpected ListMessages(%q)", parent)
	}
	all, err := f.messages(parent, o)
	if err != nil {
		return nil, err
	}
	kept := make([]*chatapi.Message, 0, len(all))
	for _, m := range all {
		if o.Keep == nil || o.Keep(m) {
			kept = append(kept, m)
		}
	}
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].CreateTime < kept[b].CreateTime })
	if o.Limit > 0 && len(kept) > o.Limit {
		kept = kept[len(kept)-o.Limit:]
	}
	return kept, nil
}

func (f *fakeAPI) LatestMessages(_ context.Context, parent string, n int) ([]*chatapi.Message, error) {
	if f.latest == nil {
		f.t.Fatalf("unexpected LatestMessages(%q)", parent)
	}
	return f.latest(parent, n)
}

// SpaceReadState answers with a canonical name by default: every Run resolves
// the caller's own ID through this call, so a nil hook is normal, not an error.
func (f *fakeAPI) SpaceReadState(_ context.Context, spaceName string) (string, time.Time, error) {
	if f.readState == nil {
		return readStateFor(nil)(spaceName)
	}
	return f.readState(spaceName)
}

// GetMessage records every head fetch. Unlike the other hooks, a nil hook is
// not a test failure: once thread titles exist, any Run whose scan missed a
// thread's opening message legitimately fetches it. The default answer is a
// message with no text, which yields no title and no warning — so tests
// written before titles existed keep the assertions they already had.
func (f *fakeAPI) GetMessage(_ context.Context, name string) (*chatapi.Message, error) {
	f.msgMu.Lock()
	f.gotHeads = append(f.gotHeads, name)
	hook := f.getMessage
	f.msgMu.Unlock()
	if hook == nil {
		return &chatapi.Message{Name: name, CreateTime: "2026-07-01T00:00:00Z"}, nil
	}
	return hook(name)
}

// getMessageNames returns the head messages fetched so far. The fetches run
// concurrently, so the order is not meaningful beyond a single call — sort
// before comparing more than one name.
func (f *fakeAPI) getMessageNames() []string {
	f.msgMu.Lock()
	defer f.msgMu.Unlock()
	return append([]string(nil), f.gotHeads...)
}

func (f *fakeAPI) getMessageCalls() int { return len(f.getMessageNames()) }

// meUser is the caller's own ID in every engine test.
const meUser = "users/114427003"

// readStateFor makes SpaceReadState answer with the canonical name that carries
// the caller's ID, plus an optional per-space lastReadTime.
func readStateFor(times map[string]string) func(string) (string, time.Time, error) {
	return func(spaceName string) (string, time.Time, error) {
		name := "users/114427003/" + spaceName + "/spaceReadState"
		s, ok := times[spaceName]
		if !ok {
			return name, time.Time{}, nil
		}
		ts, err := time.Parse(time.RFC3339, s)
		return name, ts, err
	}
}

func TestClientSatisfiesAPI(t *testing.T) {
	var _ API = (*Client)(nil)
}

func TestBuildMessageFilter(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		since, until time.Time
		thread, want string
	}{
		{time.Time{}, time.Time{}, "", ""},
		{since, time.Time{}, "", `create_time > "2026-07-19T00:00:00Z"`},
		{time.Time{}, until, "", `create_time < "2026-07-26T00:00:00Z"`},
		{since, until, "", `create_time > "2026-07-19T00:00:00Z" AND create_time < "2026-07-26T00:00:00Z"`},
		{time.Time{}, time.Time{}, "spaces/A/threads/T", `thread.name = "spaces/A/threads/T"`},
		{since, time.Time{}, "spaces/A/threads/T",
			`create_time > "2026-07-19T00:00:00Z" AND thread.name = "spaces/A/threads/T"`},
	}
	for _, c := range cases {
		if got := buildMessageFilter(c.since, c.until, c.thread); got != c.want {
			t.Errorf("buildMessageFilter(%v,%v,%q) = %q, want %q", c.since, c.until, c.thread, got, c.want)
		}
	}
}

func TestBuildMessageFilterNormalisesToUTC(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	since := time.Date(2026, 7, 19, 7, 0, 0, 0, ict)
	if got, want := buildMessageFilter(since, time.Time{}, ""), `create_time > "2026-07-19T00:00:00Z"`; got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
}

func TestMatchesSpaceType(t *testing.T) {
	sp := &chatapi.Space{SpaceType: "DIRECT_MESSAGE"}
	if !matchesSpaceType(sp, nil) {
		t.Error("no --type filter must match everything")
	}
	if !matchesSpaceType(sp, []string{"dm"}) {
		t.Error("dm should match DIRECT_MESSAGE")
	}
	if matchesSpaceType(sp, []string{"space", "group"}) {
		t.Error("DIRECT_MESSAGE must not match space/group")
	}
	if !matchesSpaceType(&chatapi.Space{SpaceType: "GROUP_CHAT"}, []string{"group"}) {
		t.Error("group should match GROUP_CHAT")
	}
	if !matchesSpaceType(&chatapi.Space{SpaceType: "SPACE"}, []string{"space"}) {
		t.Error("space should match SPACE")
	}
}

func TestResolveSpaceByID(t *testing.T) {
	api := &fakeAPI{t: t, getSpace: func(name string) (*chatapi.Space, error) {
		return &chatapi.Space{Name: name, DisplayName: "Alpha"}, nil
	}}
	sp, err := NewEngine(api, nullDirectory{}).resolveSpace(context.Background(), "spaces/AAQA")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Name != "spaces/AAQA" {
		t.Fatalf("resolveSpace = %+v", sp)
	}
	if api.listCalls != 0 {
		t.Fatal("a raw space ID must not trigger spaces.list")
	}
}

func TestResolveSpaceByEmail(t *testing.T) {
	var gotRef string
	api := &fakeAPI{t: t, findDM: func(userRef string) (*chatapi.Space, error) {
		gotRef = userRef
		return &chatapi.Space{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}, nil
	}}
	sp, err := NewEngine(api, nullDirectory{}).resolveSpace(context.Background(), "linh@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if gotRef != "users/linh@example.com" {
		t.Fatalf("FindDirectMessage got %q", gotRef)
	}
	if sp.Name != "spaces/DM1" {
		t.Fatalf("resolveSpace = %+v", sp)
	}
}

func TestResolveSpaceByDisplayName(t *testing.T) {
	api := &fakeAPI{t: t, spaces: []*chatapi.Space{
		{Name: "spaces/A", DisplayName: "Backend Team"},
		{Name: "spaces/B", DisplayName: "Backend Team Social"},
		{Name: "spaces/C", DisplayName: "Frontend"},
	}}
	e := NewEngine(api, nullDirectory{})

	// An exact case-insensitive match wins over the substring match.
	sp, err := e.resolveSpace(context.Background(), "backend team")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Name != "spaces/A" {
		t.Fatalf("exact match should win, got %+v", sp)
	}

	// A unique substring match resolves.
	sp, err = e.resolveSpace(context.Background(), "Frontend")
	if err != nil || sp.Name != "spaces/C" {
		t.Fatalf("resolveSpace = %+v, err = %v", sp, err)
	}
}

func TestResolveSpaceAmbiguousListsCandidates(t *testing.T) {
	api := &fakeAPI{t: t, spaces: []*chatapi.Space{
		{Name: "spaces/A", DisplayName: "Backend Team"},
		{Name: "spaces/B", DisplayName: "Backend Platform"},
	}}
	_, err := NewEngine(api, nullDirectory{}).resolveSpace(context.Background(), "Backend")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"Backend Team", "spaces/A", "Backend Platform", "spaces/B"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list %q, got: %v", want, err)
		}
	}
}

func TestResolveSpaceNotFound(t *testing.T) {
	api := &fakeAPI{t: t, spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}}}
	_, err := NewEngine(api, nullDirectory{}).resolveSpace(context.Background(), "Nope")
	if err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("err = %v", err)
	}
}

// rawMsg builds a chat/v1 message the way the API returns one.
func rawMsg(name, thread, senderID, senderName, text, createTime string, mentions ...string) *chatapi.Message {
	m := &chatapi.Message{
		Name:       name,
		Thread:     &chatapi.Thread{Name: thread},
		Sender:     &chatapi.User{Name: senderID, DisplayName: senderName, Type: "HUMAN"},
		Text:       text,
		CreateTime: createTime,
	}
	for _, u := range mentions {
		m.Annotations = append(m.Annotations, &chatapi.Annotation{
			Type:        "USER_MENTION",
			UserMention: &chatapi.UserMentionMetadata{Type: "MENTION", User: &chatapi.User{Name: u}},
		})
	}
	return m
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

func TestLimitCutsNewestFirstAcrossAllSpaces(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			switch parent {
			case "spaces/A":
				return []*chatapi.Message{
					rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "a-old", "2026-07-20T00:00:00Z"),
					rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "a-new", "2026-07-25T00:00:00Z"),
				}, nil
			default:
				return []*chatapi.Message{
					rawMsg("spaces/B/messages/t3.t3", "spaces/B/threads/t3", "users/2", "Huy", "b-newest", "2026-07-25T12:00:00Z"),
				}, nil
			}
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 2 {
		t.Fatalf("limit is a total across spaces, got %+v", res.Summary)
	}
	var texts []string
	for _, sg := range res.Spaces {
		for _, tg := range sg.Threads {
			for _, m := range tg.Messages {
				texts = append(texts, m.Text)
			}
		}
	}
	sort.Strings(texts)
	if len(texts) != 2 || texts[0] != "a-new" || texts[1] != "b-newest" {
		t.Fatalf("kept %v, want the two newest", texts)
	}
}

func TestThreadLimitCutsThreadsNotMessages(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "old-1", "2026-07-20T00:00:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "old-2", "2026-07-20T00:01:00Z"),
				rawMsg("spaces/A/messages/t2.t2", "spaces/A/threads/t2", "users/2", "Huy", "new-1", "2026-07-25T00:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{ThreadLimit: 1, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Threads != 1 || res.Summary.Messages != 1 {
		t.Fatalf("summary = %+v", res.Summary)
	}
	if res.Spaces[0].Threads[0].Thread.ID != "spaces/A/threads/t2" {
		t.Fatalf("kept the wrong thread: %+v", res.Spaces[0].Threads[0].Thread)
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

func TestDMNameComesFromTheOtherParticipant(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE", SpaceThreadingState: "UNTHREADED_MESSAGES"},
		},
		readState: readStateFor(nil),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/DM1/messages/t2.t2", "spaces/DM1/threads/t2", "users/1", "Linh Tran", "theirs", "2026-07-25T09:01:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{UnreadOnly: false, MentionsMe: false, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Spaces[0].Space.Name != "Linh Tran" {
		t.Fatalf("DM name = %q, want the other participant", res.Spaces[0].Space.Name)
	}
}

func TestSpaceTypeFilterLimitsTheScan(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha", SpaceType: "SPACE"},
			{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			if parent != "spaces/A" {
				t.Errorf("scanned %q despite --type space", parent)
			}
			return nil, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{SpaceTypes: []string{"space"}, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Spaces != 1 {
		t.Fatalf("summary.Spaces = %d, want 1", res.Summary.Spaces)
	}
}

// fakeDirectory answers from a fixed map and records what it was asked.
type fakeDirectory struct {
	people map[string]Person
	err    error
	mu     sync.Mutex
	asked  []string
	calls  int
}

func (f *fakeDirectory) Lookup(_ context.Context, ids []string) (map[string]Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.asked = append(f.asked, ids...)
	out := map[string]Person{}
	for _, id := range ids {
		if p, ok := f.people[id]; ok {
			out[id] = p
		}
	}
	return out, f.err
}

func TestSenderNamesAndEmailsComeFromTheDirectory(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran", Email: "linh@example.com"},
	}}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Spaces[0].Threads[0].Messages[0]
	if m.Sender.Name != "Linh Tran" || m.Sender.Email != "linh@example.com" {
		t.Fatalf("sender = %+v", m.Sender)
	}
	if m.Sender.ID != "users/1" {
		t.Fatalf("the raw ID must survive in Sender.ID, got %q", m.Sender.ID)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}
}

func TestUnresolvedSenderKeepsItsRawID(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/9", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, &fakeDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Sender.Name; got != "users/9" {
		t.Fatalf("sender name = %q, want the raw ID", got)
	}
}

// The lookup runs after --limit, so nobody who was cut is ever resolved.
func TestResolveNamesRunsAfterTheLimit(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "old", "2026-07-20T09:00:00Z"),
				rawMsg("spaces/A/messages/t2.t2", "spaces/A/threads/t2", "users/2", "", "new", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran"},
		"users/2": {Name: "Huy Nguyen"},
	}}
	if _, err := NewEngine(api, dir).Run(context.Background(), Query{Limit: 1, Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	if len(dir.asked) != 1 || dir.asked[0] != "users/2" {
		t.Fatalf("directory was asked for %v, want only the sender that survived --limit", dir.asked)
	}
}

func TestDirectoryIsAskedOnceForEachDistinctSender(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg(parent+"/messages/t1.t1", parent+"/threads/t1", "users/1", "", "one", "2026-07-25T09:00:00Z"),
				rawMsg(parent+"/messages/t1.t2", parent+"/threads/t1", "users/1", "", "two", "2026-07-25T09:01:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	if _, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	if dir.calls != 1 || len(dir.asked) != 1 {
		t.Fatalf("directory calls=%d asked=%v, want one call for one distinct sender", dir.calls, dir.asked)
	}
}

func TestDMIsNamedAfterTheResolvedOtherParticipant(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE", SpaceThreadingState: "UNTHREADED_MESSAGES"},
		},
		readState: readStateFor(nil),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", meUser, "", "mine", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/DM1/messages/t2.t2", "spaces/DM1/threads/t2", "users/1", "", "theirs", "2026-07-25T09:01:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		meUser:    {Name: "Ben"},
		"users/1": {Name: "Linh Tran"},
	}}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Spaces[0].Space.Name != "Linh Tran" {
		t.Fatalf("DM name = %q, want the resolved other participant", res.Spaces[0].Space.Name)
	}
}

// A directory failure warns and leaves raw IDs; it never fails the read.
func TestDirectoryFailureWarnsAndKeepsTheResult(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{err: errors.New("403 ACCESS_TOKEN_SCOPE_INSUFFICIENT")}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatalf("a directory failure must not fail the run: %v", err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("summary = %+v, want the message to have survived", res.Summary)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Sender.Name; got != "users/1" {
		t.Fatalf("sender name = %q, want the raw ID", got)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "gsvc auth login") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

// The degenerate case has to stay safe: no directory, no change in output.
func TestNullDirectoryLeavesTheOutputUnchanged(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Spaces[0].Threads[0].Messages[0]
	if m.Sender.Name != "users/1" || m.Sender.Email != "" {
		t.Fatalf("sender = %+v, want the untouched raw ID", m.Sender)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}
}

func TestSpacesNamesDMsAfterTheOtherParticipant(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha", SpaceType: "SPACE"},
			{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"},
		},
		latest: func(parent string, _ int) ([]*chatapi.Message, error) {
			// Newest first, and the newest is our own — the common case in a DM
			// you spoke in last.
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t2.t2", "spaces/DM1/threads/t2", meUser, "", "mine", "2026-07-25T10:00:00Z"),
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", "users/1", "", "theirs", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	list, err := NewEngine(api, dir).Spaces(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, s := range list.Spaces {
		byID[s.ID] = s.Name
	}
	if byID["spaces/A"] != "Alpha" {
		t.Errorf("a named space must keep its own name, got %q", byID["spaces/A"])
	}
	if byID["spaces/DM1"] != "Linh Tran" {
		t.Errorf("DM name = %q, want the other participant", byID["spaces/DM1"])
	}
}

// A space with no messages has no other source for a name, so it keeps its
// short ID rather than inventing one.
func TestSpacesKeepsTheShortIDWhenThereIsNothingToProbe(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}},
		latest: func(string, int) ([]*chatapi.Message, error) { return nil, nil },
	}
	list, err := NewEngine(api, &fakeDirectory{}).Spaces(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if list.Spaces[0].Name != "DM1" {
		t.Fatalf("name = %q, want the short ID", list.Spaces[0].Name)
	}
}

// The probe is one request per nameless space, so a remembered name has to skip
// it entirely — that is the difference between ~3s and ~0.7s.
func TestSpacesSkipsTheProbeForARememberedName(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}},
		latest: func(parent string, _ int) ([]*chatapi.Message, error) {
			t.Errorf("probed %q despite a cached name", parent)
			return nil, nil
		},
	}
	cache := newCachedDirectory(&fakeDirectory{}, filepath.Join(t.TempDir(), "people-test.json"), false)
	cache.Remember("spaces/DM1", Person{Name: "Linh Tran"})

	list, err := NewEngine(api, cache).Spaces(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if list.Spaces[0].Name != "Linh Tran" {
		t.Fatalf("name = %q, want the remembered one", list.Spaces[0].Name)
	}
}

func TestSpacesRemembersWhatItProbed(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}},
		latest: func(string, int) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", "users/1", "", "hi", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	path := filepath.Join(t.TempDir(), "people-test.json")
	cache := newCachedDirectory(&fakeDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}, path, false)

	if _, err := NewEngine(api, cache).Spaces(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	reload := newCachedDirectory(&fakeDirectory{}, path, false)
	p, ok := reload.Recall("spaces/DM1")
	if !ok || p.Name != "Linh Tran" {
		t.Fatalf("Recall = %+v, %v; the probed name must survive to the next run", p, ok)
	}
}

func TestSpacesWarnsWhenTheDirectoryFails(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}},
		latest: func(string, int) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", "users/1", "", "hi", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{err: errors.New("403 ACCESS_TOKEN_SCOPE_INSUFFICIENT")}
	list, err := NewEngine(api, dir).Spaces(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatalf("a directory failure must not fail chat spaces: %v", err)
	}
	if list.Spaces[0].Name != "DM1" {
		t.Fatalf("name = %q, want the short ID fallback", list.Spaces[0].Name)
	}
	if len(list.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one", list.Warnings)
	}
}

func TestSpaceNameRecallsCachedNameWhenOnlyMeMessagesInScan(t *testing.T) {
	now := fixedTime(t, "2026-07-26T00:00:00Z")
	path := filepath.Join(t.TempDir(), "people-test.json")
	cache := newCachedDirectory(nullDirectory{}, path, false)
	cache.Remember("spaces/DM1", Person{Name: "Alice Smith"})

	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE"}},
		readState: readStateFor(map[string]string{"spaces/DM1": "2026-07-25T00:00:00Z"}),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", meUser, "Ben", "I sent a message", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}

	res, err := NewEngine(api, cache).Run(context.Background(), Query{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spaces) != 1 {
		t.Fatalf("res.Spaces count = %d, want 1", len(res.Spaces))
	}
	if res.Spaces[0].Space.Name != "Alice Smith" {
		t.Fatalf("Space.Name = %q, want cached name %q", res.Spaces[0].Space.Name, "Alice Smith")
	}
}

// A limit is a limit on messages the user asked for. Spending it on messages
// that the client-side filter then discards makes `chat mentions --limit 50`
// return nothing whenever the 50 newest messages happen not to mention you.
func TestMentionsSurviveTheMessageLimit(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "hi ben", "2026-07-25T09:00:00Z", meUser),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:01:00Z"),
				rawMsg("spaces/A/messages/t1.t3", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:02:00Z"),
				rawMsg("spaces/A/messages/t1.t4", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:03:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		MentionsMe: true, Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("the limit must be spent on mentions, not on messages that are filtered away: %+v", res.Summary)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Text; got != "hi ben" {
		t.Fatalf("message = %q, want %q", got, "hi ben")
	}
}

func TestUnreadSurvivesTheMessageLimit(t *testing.T) {
	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		readState: readStateFor(map[string]string{"spaces/A": "2026-07-25T09:00:30Z"}),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "unread one", "2026-07-25T09:01:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:02:00Z"),
				rawMsg("spaces/A/messages/t1.t3", "spaces/A/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:03:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		UnreadOnly: true, Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("your own newer messages must not eat the --limit budget: %+v", res.Summary)
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

type fakeCacheDirectory struct {
	nullDirectory
	recalled map[string]Person
}

func (f fakeCacheDirectory) Recall(id string) (Person, bool) {
	p, ok := f.recalled[id]
	return p, ok
}

func (f fakeCacheDirectory) Remember(id string, p Person) {}
func (f fakeCacheDirectory) Flush()                      {}

func TestSpaceNameIgnoresUnresolvedUserIDs(t *testing.T) {
	sc := spaceScan{
		space: &chatapi.Space{Name: "spaces/DM123"},
		msgs: []MessageInfo{
			{
				Sender: Sender{
					Name: "users/123456789", // Unresolved raw user ID
					IsMe: false,
				},
			},
		},
	}
	dir := fakeCacheDirectory{
		recalled: map[string]Person{
			"spaces/DM123": {Name: "Alice Smith"},
		},
	}

	got := spaceName(sc, dir)
	if got != "Alice Smith" {
		t.Fatalf("spaceName() = %q, want %q", got, "Alice Smith")
	}
}

