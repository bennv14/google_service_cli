package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	chatapi "google.golang.org/api/chat/v1"
)

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
