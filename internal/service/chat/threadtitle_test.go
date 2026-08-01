package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

func TestJSONCarriesTheTitleAndItsAuthor(t *testing.T) {
	// Only a reply is in hand, so the opening message is not in messages[] at
	// all — which is exactly why its sender has to travel with the title.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		return rawMsg(name, "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-20T09:00:00Z"), nil
	}

	b, err := json.Marshal(titleRun(t, api, &fakeDirectory{}))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"title":"Friday deploy plan"`) {
		t.Fatalf("title is missing from the thread level:\n%s", got)
	}
	if !strings.Contains(got, `"headSender":"Linh"`) {
		t.Fatalf("the title's author is missing, and is nowhere else in the payload:\n%s", got)
	}
	if strings.Contains(got, `"Friday deploy plan"`) && !strings.Contains(got, `"which server?"`) {
		t.Fatalf("the reply should still be the only message listed:\n%s", got)
	}
}

func TestAThreadWithNoKnownHeadOmitsBothFields(t *testing.T) {
	api := titleAPI(t,
		rawMsg("spaces/A/messages/weird", "", "users/2", "Huy", "orphan", "2026-07-25T09:05:00Z"),
	)
	b, err := json.Marshal(titleRun(t, api, &fakeDirectory{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"title"`, `"headSender"`} {
		if strings.Contains(string(b), field) {
			t.Fatalf("%s must be omitted when there is no head to describe:\n%s", field, b)
		}
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

	// The half that matters: no title does not mean no head. Linh opened this
	// thread with an attachment and Huy only replied to it, so labelling the
	// thread "Huy" is the same misattribution as labelling a partial thread by
	// its earliest reply — reached by a different route.
	tg := res.Spaces[0].Threads[0]
	if got := threadLabel(tg); got != "Linh" {
		t.Fatalf("threadLabel = %q, want %q: a text-less head still names who opened the thread", got, "Linh")
	}
}

func TestHeadWithNeitherTextNorSenderFallsBackToTheID(t *testing.T) {
	// Nothing usable came back, but the head was still read: the reply is not a
	// substitute for it, so the label degrades to the ID rather than to Huy.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "one", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		m := rawMsg(name, "", "", "", "", "2026-07-20T09:00:00Z")
		m.Sender = nil
		return m, nil
	}

	res := titleRun(t, api, &fakeDirectory{})

	if got := threadLabel(res.Spaces[0].Threads[0]); got != "t1" {
		t.Fatalf("threadLabel = %q, want the short thread ID", got)
	}
}

func TestAppAssignedMessageIDsCostNoRequestAndNoWarning(t *testing.T) {
	// Chat lets apps set their own message IDs ("client-…"), which break the
	// {tid}.{mid} convention. isPartial cannot see a head in such a thread, but
	// it does not call it partial either — and it is right not to: the head is
	// in hand. Deriving spaces/A/messages/t1.t1 would fetch a name that does
	// not exist, then warn about an opening message that was never missing.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/client-abc", "spaces/A/threads/t1", "users/1", "Linh", "Friday deploy plan", "2026-07-25T09:00:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		t.Fatalf("unexpected GetMessage(%q): the opening message is already in hand", name)
		return nil, nil
	}

	res := titleRun(t, api, &fakeDirectory{})

	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v; nothing here is unreadable", res.Warnings)
	}
	// The label still comes out right, from the message in hand.
	if got := threadLabel(res.Spaces[0].Threads[0]); got != `Linh · "Friday deploy plan"` {
		t.Fatalf("threadLabel = %q", got)
	}
}

func TestHeadsAreAppliedToTheirOwnSpaceAndThread(t *testing.T) {
	// titleAPI only ever serves spaces/A, so every other test in this file
	// runs headRef.group at a constant 0 — an implementation that dropped the
	// index entirely and wrote every fetched head into groups[0] would still
	// pass all of them. Two spaces, each holding one headed thread (its
	// opening message already in the scan window) and one headless thread
	// (opening message outside it), with four distinct titles, is what it
	// takes to prove a fetched head lands back on the thread it came from.
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
					// a1: headed — its own opening message is right here.
					rawMsg("spaces/A/messages/a1.a1", "spaces/A/threads/a1", "users/1", "Linh", "Alpha headed title", "2026-07-25T09:00:00Z"),
					// a2: headless — only a reply is in hand.
					rawMsg("spaces/A/messages/a2.reply", "spaces/A/threads/a2", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
				}, nil
			case "spaces/B":
				return []*chatapi.Message{
					rawMsg("spaces/B/messages/b1.b1", "spaces/B/threads/b1", "users/3", "Mai", "Beta headed title", "2026-07-25T08:00:00Z"),
					rawMsg("spaces/B/messages/b2.reply", "spaces/B/threads/b2", "users/4", "Nam", "one more thing", "2026-07-25T08:05:00Z"),
				}, nil
			}
			return nil, nil
		},
	}
	api.getMessage = func(name string) (*chatapi.Message, error) {
		switch name {
		case "spaces/A/messages/a2.a2":
			return rawMsg(name, "spaces/A/threads/a2", "users/5", "Vy", "Alpha headless title", "2026-07-20T09:00:00Z"), nil
		case "spaces/B/messages/b2.b2":
			return rawMsg(name, "spaces/B/threads/b2", "users/6", "Khoa", "Beta headless title", "2026-07-20T08:00:00Z"), nil
		}
		t.Fatalf("unexpected GetMessage(%q)", name)
		return nil, nil
	}

	res := titleRun(t, api, &fakeDirectory{})

	space := func(id string) SpaceGroup {
		for _, sg := range res.Spaces {
			if sg.Space.ID == id {
				return sg
			}
		}
		t.Fatalf("space %s missing from result", id)
		return SpaceGroup{}
	}
	thread := func(sg SpaceGroup, id string) ThreadInfo {
		for _, tg := range sg.Threads {
			if tg.Thread.ID == id {
				return tg.Thread
			}
		}
		t.Fatalf("thread %s missing from space %s", id, sg.Space.ID)
		return ThreadInfo{}
	}

	cases := []struct {
		threadID, wantTitle, wantSender string
	}{
		{"spaces/A/threads/a1", "Alpha headed title", "Linh"},
		{"spaces/A/threads/a2", "Alpha headless title", "Vy"},
		{"spaces/B/threads/b1", "Beta headed title", "Mai"},
		{"spaces/B/threads/b2", "Beta headless title", "Khoa"},
	}
	spaceOf := map[string]string{
		"spaces/A/threads/a1": "spaces/A", "spaces/A/threads/a2": "spaces/A",
		"spaces/B/threads/b1": "spaces/B", "spaces/B/threads/b2": "spaces/B",
	}
	for _, c := range cases {
		th := thread(space(spaceOf[c.threadID]), c.threadID)
		if th.Title != c.wantTitle {
			t.Errorf("%s: title = %q, want %q", c.threadID, th.Title, c.wantTitle)
		}
		if th.HeadSender != c.wantSender {
			t.Errorf("%s: head sender = %q, want %q", c.threadID, th.HeadSender, c.wantSender)
		}
	}

	// The headed threads' titles came from messages already in hand; only the
	// two headless threads should have cost a request.
	got := api.getMessageNames()
	sort.Strings(got)
	want := []string{"spaces/A/messages/a2.a2", "spaces/B/messages/b2.b2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("GetMessage names = %v, want exactly %v", got, want)
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
			// Partial is what makes a head worth fetching, so without it this
			// test would prove nothing: no thread would be a candidate and the
			// count would sit at zero however broken cancellation was.
			Thread: ThreadInfo{ID: "spaces/A/threads/" + tid, Partial: true},
			Messages: []MessageInfo{{
				ID: "spaces/A/messages/" + tid + ".zz", Sender: Sender{Name: "Huy"}, Text: "reply",
			}},
		})
	}

	e := NewEngine(api, &fakeDirectory{})
	e.resolveThreadTitles(ctx, groups)

	// Whatever did slip out failed with context.Canceled. Reporting those would
	// tell the user their opening messages are unreadable when all they did was
	// press Ctrl-C — and nondeterministically, since the count is a race.
	if w := e.warnings(); len(w) != 0 {
		t.Fatalf("warnings = %v; an interrupted run must not blame the opening messages", w)
	}

	// The launch loop selects between the semaphore and ctx.Done(). Both are
	// ready here, and select picks at random, so a couple of fetches can slip
	// out before one of the coin flips lands on Done — but the odds of more
	// than a handful are vanishing (2^-25), while without the ctx.Done() branch
	// all hundred would launch. Asserting merely "< 100" would pass even if 99
	// did, which is cancellation failing.
	const slack = 25
	if got := api.getMessageCalls(); got > slack {
		t.Fatalf("GetMessage called %d times for %d threads; cancellation must stop the fan-out after a few at most", got, threads)
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
