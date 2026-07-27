package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixedTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func sampleResult(t *testing.T) Result {
	t.Helper()
	lastRead := fixedTime(t, "2026-07-25T15:45:00Z")
	return Result{
		Spaces: []SpaceGroup{{
			Space: SpaceInfo{
				ID:           "spaces/AAQAbc123XyZ",
				Name:         "Backend Team",
				Type:         "SPACE",
				Threading:    "THREADED_MESSAGES",
				LastReadTime: &lastRead,
				UnreadCount:  1,
				Link:         "https://chat.google.com/u/0/app/chat/AAQAbc123XyZ",
			},
			Threads: []ThreadGroup{{
				Thread: ThreadInfo{
					ID:           "spaces/AAQAbc123XyZ/threads/kR3nP",
					MessageCount: 1,
					LastActivity: fixedTime(t, "2026-07-25T09:12:04Z"),
					Link:         "https://chat.google.com/room/AAQAbc123XyZ/kR3nP/kR3nP",
				},
				Messages: []MessageInfo{{
					ID:           "spaces/AAQAbc123XyZ/messages/kR3nP.kR3nP",
					ThreadID:     "spaces/AAQAbc123XyZ/threads/kR3nP",
					IsThreadHead: true,
					CreateTime:   fixedTime(t, "2026-07-25T09:12:04Z"),
					Sender:       Sender{ID: "users/108812449", Name: "Linh Tran", Type: "HUMAN"},
					Text:         "review PR #221 please,\nit blocks the release",
					Mentions:     []string{"users/114427003"},
					MentionsMe:   true,
					Link:         "https://chat.google.com/room/AAQAbc123XyZ/kR3nP/kR3nP",
				}},
			}},
		}},
		Summary: Summary{Spaces: 12, Messages: 8, Threads: 4, Unread: 4, Window: "2 days"},
	}
}

func TestResultMarshalsAsSpaceArray(t *testing.T) {
	b, err := json.Marshal(sampleResult(t))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Result must marshal as a JSON array of spaces: %v (%s)", err, b)
	}
	if len(got) != 1 {
		t.Fatalf("got %d spaces", len(got))
	}
	sp := got[0]["space"].(map[string]any)
	if sp["id"] != "spaces/AAQAbc123XyZ" || sp["unreadCount"].(float64) != 1 {
		t.Fatalf("space = %v", sp)
	}
	msgs := got[0]["threads"].([]any)[0].(map[string]any)["messages"].([]any)
	m := msgs[0].(map[string]any)
	if m["isThreadHead"] != true || m["mentionsMe"] != true {
		t.Fatalf("message = %v", m)
	}
	if _, ok := m["threadId"]; ok {
		t.Fatal("threadId must not appear in JSON; the thread level already carries it")
	}
	// Summary and errors are stderr concerns, so `-o json > out.json` must stay
	// valid JSON: the top level must be an array, not an object exposing those
	// fields as keys (already enforced above by unmarshaling into []map[string]any),
	// and neither field name may appear as a key at all, case-insensitively, in
	// case a fallback to default struct marshaling emits them capitalized.
	lb := strings.ToLower(string(b))
	if strings.Contains(lb, `"summary"`) || strings.Contains(lb, `"errors"`) {
		t.Fatalf("JSON leaked stderr-only fields: %s", b)
	}
}

func TestEmptyResultMarshalsAsEmptyArray(t *testing.T) {
	b, err := json.Marshal(Result{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Fatalf("empty Result = %s, want []", b)
	}
}

func TestResultTableIsFlatAndSingleLine(t *testing.T) {
	r := sampleResult(t)
	if h := r.Headers(); len(h) != 5 || h[0] != "TIME" || h[4] != "TEXT" {
		t.Fatalf("Headers() = %v", h)
	}
	rows := r.Rows()
	if len(rows) != 1 {
		t.Fatalf("Rows() = %v", rows)
	}
	if rows[0][1] != "Backend Team" || rows[0][2] != "kR3nP" || rows[0][3] != "Linh Tran" {
		t.Fatalf("Rows()[0] = %v", rows[0])
	}
	if strings.Contains(rows[0][4], "\n") {
		t.Fatalf("table cell must be single-line: %q", rows[0][4])
	}
}

func TestSpaceListView(t *testing.T) {
	sl := SpaceList{Spaces: []SpaceInfo{{
		ID: "spaces/A", Name: "Backend Team", Type: "SPACE",
		Threading: "THREADED_MESSAGES", UnreadCount: 3,
	}}}
	if h := sl.Headers(); len(h) != 6 || h[1] != "NAME" || h[4] != "UNREAD" {
		t.Fatalf("Headers() = %v", h)
	}
	rows := sl.Rows()
	if len(rows) != 1 || rows[0][0] != "spaces/A" || rows[0][4] != "3" {
		t.Fatalf("Rows() = %v", rows)
	}
	b, err := json.Marshal(sl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "[{") {
		t.Fatalf("SpaceList must marshal as an array: %s", b)
	}
}

func TestSummaryString(t *testing.T) {
	cases := []struct {
		in   Summary
		want string
	}{
		{Summary{Spaces: 12, Messages: 8, Threads: 4, Unread: 4, Window: "2 days"},
			"scanned 12 spaces · 2 days · 8 messages · 4 threads · 4 unread"},
		{Summary{Spaces: 1, Messages: 1, Window: "7 days"},
			"scanned 1 space · 7 days · 1 message"},
		{Summary{Spaces: 3},
			"scanned 3 spaces · 0 messages"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Summary.String() = %q, want %q", got, c.want)
		}
	}
}

func TestExcerptAndOneLine(t *testing.T) {
	if got := oneLine("a\nb   c\t d"); got != "a b c d" {
		t.Errorf("oneLine = %q", got)
	}
	if got := excerpt("hello world", 20); got != "hello world" {
		t.Errorf("excerpt = %q", got)
	}
	if got := excerpt("hello world", 5); got != "hello…" {
		t.Errorf("excerpt = %q", got)
	}
	// Truncation counts runes, not bytes.
	if got := excerpt("nghiêm túc lắm", 6); got != "nghiêm…" {
		t.Errorf("excerpt = %q", got)
	}
}
