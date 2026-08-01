package chat

import (
	"context"
	"encoding/json"
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
