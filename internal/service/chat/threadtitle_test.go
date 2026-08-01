package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	chatapi "google.golang.org/api/chat/v1"
)

// titleAPI serves one threaded space whose messages the caller supplies.
func titleAPI(t *testing.T, msgs ...*chatapi.Message) *fakeAPI {
	t.Helper()
	return &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{{
			Name: "spaces/A", DisplayName: "Alpha",
			SpaceType: "SPACE", SpaceThreadingState: "THREADED_MESSAGES",
		}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) { return msgs, nil },
	}
}

// titleRun runs one query over titleAPI's fixed window.
func titleRun(t *testing.T, api *fakeAPI, dir Directory) Result {
	t.Helper()
	res, err := NewEngine(api, dir).Run(context.Background(),
		Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTitleComesFromTheHeadAlreadyInHand(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-25T09:00:00Z"),
		rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/2", "Huy", "ok", "2026-07-25T09:05:00Z"),
	)
	th := titleRun(t, api, &fakeDirectory{}).Spaces[0].Threads[0].Thread

	if th.Title != "Friday deploy plan" {
		t.Fatalf("title = %q, want the head message's text", th.Title)
	}
	if th.HeadSender != "Linh" {
		t.Fatalf("head sender = %q, want Linh", th.HeadSender)
	}
	if n := api.getMessageCalls(); n != 0 {
		t.Fatalf("GetMessage called %d times; a head already in hand must cost no request", n)
	}
}

func TestTitleIsInJSONAndTheHeadSenderIsNot(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-25T09:00:00Z"),
	)
	b, err := json.Marshal(titleRun(t, api, &fakeDirectory{}))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"title":"Friday deploy plan"`) {
		t.Fatalf("title is missing from the thread level:\n%s", got)
	}
	// HeadSender is carried for the text renderer only, like MessageInfo.ThreadID.
	if strings.Contains(strings.ToLower(got), "headsender") {
		t.Fatalf("HeadSender must never reach JSON:\n%s", got)
	}
}

func TestMissingHeadIsFetchedByItsDerivedName(t *testing.T) {
	// Only a reply is in hand: this is what `chat mentions` sees, because the
	// head almost never mentions you.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}

	res := titleRun(t, api, &fakeDirectory{})

	// spaces/A/threads/t1 → spaces/A/messages/t1.t1
	if got := api.getMessageNames(); len(got) != 1 || got[0] != "spaces/A/messages/t1.t1" {
		t.Fatalf("GetMessage names = %v, want exactly [spaces/A/messages/t1.t1]", got)
	}
	th := res.Spaces[0].Threads[0].Thread
	if th.Title != "Friday deploy plan" {
		t.Fatalf("title = %q, want the fetched head's text", th.Title)
	}
	if th.HeadSender != "Linh" {
		t.Fatalf("head sender = %q, want Linh", th.HeadSender)
	}
	if !th.Partial {
		t.Fatal("a thread whose head was outside the window is still partial")
	}
}

func TestUnparseableThreadIDIssuesNoRequest(t *testing.T) {
	// A message name that breaks the {tid}.{mid} convention forms its own
	// thread keyed by that raw name, from which no head name can be derived.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/weird", "", "users/2", "Huy", "orphan", "2026-07-25T09:05:00Z"),
	)
	res := titleRun(t, api, &fakeDirectory{})

	if n := api.getMessageCalls(); n != 0 {
		t.Fatalf("GetMessage called %d times for a thread ID that cannot be parsed", n)
	}
	if got := res.Spaces[0].Threads[0].Thread.Title; got != "" {
		t.Fatalf("title = %q, want none", got)
	}
}

func TestEachRunRefetchesTheHead(t *testing.T) {
	// Titles are never cached: a second run must pay for them again. One
	// Engine is reused deliberately — a fresh Engine would prove nothing about
	// state the first run might have kept.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}
	e := NewEngine(api, &fakeDirectory{})
	for i := 0; i < 2; i++ {
		if _, err := e.Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if got := api.getMessageCalls(); got != 2 {
		t.Fatalf("GetMessage called %d times across two runs, want 2", got)
	}
}

func TestHeadSenderIsResolvedThroughTheDirectory(t *testing.T) {
	// The Chat API leaves displayName empty for a human caller, so both the
	// reply's sender and the head's arrive as raw users/… IDs.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "spaces/A/threads/t1", "users/1", "", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}
	dir := &fakeDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran"},
		"users/2": {Name: "Huy Nguyen"},
	}}

	res := titleRun(t, api, dir)

	if got := res.Spaces[0].Threads[0].Thread.HeadSender; got != "Linh Tran" {
		t.Fatalf("head sender = %q, want the resolved name", got)
	}
	// One lookup for the scanned senders, one for the senders the heads
	// introduced. Two, never one per head.
	if dir.calls != 2 {
		t.Fatalf("directory calls = %d, want 2", dir.calls)
	}
}

func TestHeadSendersAreLookedUpOnceForAllThreads(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "", "one", "2026-07-25T09:05:00Z"),
		rawMsg("spaces/A/messages/t2.t9", "spaces/A/threads/t2", "users/2", "", "two", "2026-07-25T09:06:00Z"),
	)
	// Both threads were opened by the same person.
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "", "users/1", "", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}
	dir := &fakeDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}

	res := titleRun(t, api, dir)

	// users/2 from the scan, then users/1 once for both heads.
	if len(dir.asked) != 2 || dir.asked[0] != "users/2" || dir.asked[1] != "users/1" {
		t.Fatalf("directory was asked for %v, want [users/2 users/1]", dir.asked)
	}
	for _, tg := range res.Spaces[0].Threads {
		if tg.Thread.HeadSender != "Linh Tran" {
			t.Fatalf("thread %s head sender = %q", tg.Thread.ID, tg.Thread.HeadSender)
		}
	}
}

func TestUnresolvedHeadSenderKeepsItsRawID(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "one", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "", "users/1", "", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}

	res := titleRun(t, api, &fakeDirectory{}) // resolves nobody

	if got := res.Spaces[0].Threads[0].Thread.HeadSender; got != "users/1" {
		t.Fatalf("head sender = %q, want the raw ID; an unknown name is not a failure", got)
	}
	if got := res.Spaces[0].Threads[0].Thread.Title; got != "Friday deploy plan" {
		t.Fatalf("title = %q, want it regardless of the sender's name", got)
	}
}

func TestFailedHeadFetchesBecomeOneWarning(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "one", "2026-07-25T09:05:00Z"),
		rawMsg("spaces/A/messages/t2.t9", "spaces/A/threads/t2", "users/2", "Huy", "two", "2026-07-25T09:06:00Z"),
	)
	api.getMessage = func(string) (*chatapi.Message, error) {
		return nil, errors.New("googleapi: Error 404: message not found")
	}

	res := titleRun(t, api, &fakeDirectory{}) // must not return an error

	want := "2 threads: could not read the opening message"
	if len(res.Warnings) != 1 || res.Warnings[0] != want {
		t.Fatalf("warnings = %v, want exactly [%q]", res.Warnings, want)
	}
	if len(res.Spaces[0].Threads) != 2 {
		t.Fatalf("threads = %d, want both still present", len(res.Spaces[0].Threads))
	}
	for _, tg := range res.Spaces[0].Threads {
		if tg.Thread.Title != "" {
			t.Fatalf("thread %s got a title from a failed fetch", tg.Thread.ID)
		}
	}
}

func TestASingleFailedHeadFetchIsPhrasedInTheSingular(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "one", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(string) (*chatapi.Message, error) { return nil, errors.New("403") }

	res := titleRun(t, api, &fakeDirectory{})

	want := "1 thread: could not read the opening message"
	if len(res.Warnings) != 1 || res.Warnings[0] != want {
		t.Fatalf("warnings = %v, want exactly [%q]", res.Warnings, want)
	}
}

func TestHeadWithNoTextYieldsNoTitle(t *testing.T) {
	// An attachment-, image-, or card-only opening message has no text at all.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "one", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "", "users/1", "Linh", "", "2026-07-20T09:00:00Z"), nil
	}

	res := titleRun(t, api, &fakeDirectory{})

	if got := res.Spaces[0].Threads[0].Thread.Title; got != "" {
		t.Fatalf("title = %q, want none", got)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v; a head with no text is not a failure", res.Warnings)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"title"`) {
		t.Fatalf("an empty title must be omitted from JSON:\n%s", b)
	}
}

func TestCancelledContextStopsHeadFetching(t *testing.T) {
	const threads = 100
	api := &fakeAPI{t: t, getMessage: func(string) (*chatapi.Message, error) {
		return nil, context.Canceled
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	groups := []SpaceGroup{{Space: SpaceInfo{ID: "spaces/A", Name: "Alpha"}}}
	for i := 0; i < threads; i++ {
		tid := fmt.Sprintf("t%d", i)
		groups[0].Threads = append(groups[0].Threads, ThreadGroup{
			Thread: ThreadInfo{ID: "spaces/A/threads/" + tid},
			Messages: []MessageInfo{{
				ID: "spaces/A/messages/" + tid + ".zz", Sender: Sender{Name: "Huy"}, Text: "reply",
			}},
		})
	}

	NewEngine(api, &fakeDirectory{}).resolveThreadTitles(ctx, groups)

	// The launch loop selects between the semaphore and ctx.Done(). With the
	// context already cancelled it cannot reach the hundredth thread; without
	// the ctx.Done() branch it would queue every one of them.
	if got := api.getMessageCalls(); got >= threads {
		t.Fatalf("GetMessage called %d times for %d threads; cancellation must stop the fan-out", got, threads)
	}
	for _, tg := range groups[0].Threads {
		if tg.Thread.Title != "" {
			t.Fatalf("thread %s got a title despite cancellation", tg.Thread.ID)
		}
	}
}

func TestSpacesUnreadDoesNotFetchThreadTitles(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.readState = readStateFor(map[string]string{"spaces/A": "2026-07-25T00:00:00Z"})

	list, err := NewEngine(api, &fakeDirectory{}).Spaces(context.Background(),
		Query{UnreadOnly: true, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Spaces) != 1 || list.Spaces[0].UnreadCount != 1 {
		t.Fatalf("spaces = %+v", list.Spaces)
	}
	if n := api.getMessageCalls(); n != 0 {
		t.Fatalf("GetMessage called %d times; `chat spaces` prints no thread level", n)
	}
}
