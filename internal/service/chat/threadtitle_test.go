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
