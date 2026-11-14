package chat

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

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
