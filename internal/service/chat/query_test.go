package chat

import (
	"context"
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
	messages   func(parent, filter string, limit int) ([]*chatapi.Message, error)
	readState  func(spaceName string) (string, time.Time, error)
	members    func(spaceName string) ([]*chatapi.Membership, error)
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

func (f *fakeAPI) ListMessages(_ context.Context, parent, filter string, limit int) ([]*chatapi.Message, error) {
	f.msgFilters.Store(parent, filter)
	if f.messages == nil {
		f.t.Fatalf("unexpected ListMessages(%q)", parent)
	}
	return f.messages(parent, filter, limit)
}

// SpaceReadState answers with a canonical name by default: every Run resolves
// the caller's own ID through this call, so a nil hook is normal, not an error.
func (f *fakeAPI) SpaceReadState(_ context.Context, spaceName string) (string, time.Time, error) {
	if f.readState == nil {
		return readStateFor(nil)(spaceName)
	}
	return f.readState(spaceName)
}

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

func (f *fakeAPI) ListMembers(_ context.Context, spaceName string) ([]*chatapi.Membership, error) {
	if f.members == nil {
		f.t.Fatalf("unexpected ListMembers(%q)", spaceName)
	}
	return f.members(spaceName)
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
	sp, err := NewEngine(api).resolveSpace(context.Background(), "spaces/AAQA")
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
	sp, err := NewEngine(api).resolveSpace(context.Background(), "linh@example.com")
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
	e := NewEngine(api)

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
	_, err := NewEngine(api).resolveSpace(context.Background(), "Backend")
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
	_, err := NewEngine(api).resolveSpace(context.Background(), "Nope")
	if err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("err = %v", err)
	}
}
