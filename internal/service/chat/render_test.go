package chat

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func at(t *testing.T, hhmm string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", "2026-07-25 "+hhmm, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func threadedResult(t *testing.T) Result {
	t.Helper()
	return Result{
		Spaces: []SpaceGroup{{
			Space: SpaceInfo{
				ID: "spaces/A", Name: "Alpha", Type: "SPACE",
				Threading: "THREADED_MESSAGES", UnreadCount: 1,
				Link: "https://chat.google.com/u/0/app/chat/A",
			},
			Threads: []ThreadGroup{
				{
					Thread: ThreadInfo{
						ID: "spaces/A/threads/t1", MessageCount: 2,
						LastActivity: at(t, "09:20"),
						Link:         "https://chat.google.com/room/A/t1/t1",
					},
					Messages: []MessageInfo{
						{ID: "spaces/A/messages/t1.t1", IsThreadHead: true, CreateTime: at(t, "09:12"),
							Sender: Sender{Name: "Linh"}, Text: "hello"},
						{ID: "spaces/A/messages/t1.t2", CreateTime: at(t, "09:20"),
							Sender: Sender{Name: "Ben", IsMe: true}, Text: "ok"},
					},
				},
				{
					Thread: ThreadInfo{
						ID: "spaces/A/threads/t2", Partial: true, MessageCount: 1,
						LastActivity: at(t, "15:40"),
						Link:         "https://chat.google.com/room/A/t2/t2",
					},
					Messages: []MessageInfo{
						{ID: "spaces/A/messages/t2.zz", CreateTime: at(t, "15:40"),
							Sender: Sender{Name: "Huy"}, Text: "down", Unread: true},
					},
				},
			},
		}},
	}
}

func TestTextRendersThreadedTree(t *testing.T) {
	var buf bytes.Buffer
	if err := threadedResult(t).Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"◆ Alpha                                                 2 threads · 1 unread\n" +
		"│\n" +
		"├─▸ Linh · \"hello\"                                                    2 msgs\n" +
		"│  │\n" +
		"│  ├ 09:12  Linh\n" +
		"│  │        hello\n" +
		"│  └ 09:20  Ben\n" +
		"│           ok\n" +
		"│\n" +
		"└─▸ Huy · \"down\"                                             1 msg · partial\n" +
		"   │\n" +
		"   └ 15:40  Huy                                                     ● unread\n" +
		"            down\n"
	if buf.String() != want {
		t.Fatalf("text output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

func TestTextHasNoEscapesWhenColorIsOff(t *testing.T) {
	var buf bytes.Buffer
	if err := threadedResult(t).Text(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Fatalf("colour must be off by default:\n%q", buf.String())
	}
}

func TestTextColorAndHyperlinksAreOptIn(t *testing.T) {
	r := threadedResult(t)
	r.Opts = RenderOpts{Color: true, Hyperlinks: true}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "\x1b[1;36m") {
		t.Error("space name should be bold+colour")
	}
	if !strings.Contains(out, "\x1b]8;;https://chat.google.com/u/0/app/chat/A\x1b\\") {
		t.Error("space name should be an OSC 8 hyperlink")
	}
	// The bare URL must not take up a line unless --links asked for it.
	if strings.Contains(out, "\n  https://") {
		t.Error("URLs should not be printed on their own line without --links")
	}
}

func TestTextShowLinksPrintsURLs(t *testing.T) {
	r := threadedResult(t)
	r.Opts = RenderOpts{ShowLinks: true}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://chat.google.com/u/0/app/chat/A",
		"https://chat.google.com/room/A/t1/t1",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("--links output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestTextFlattensUnthreadedSpaces(t *testing.T) {
	r := Result{Spaces: []SpaceGroup{{
		Space: SpaceInfo{
			ID: "spaces/DM", Name: "Linh Tran", Type: "DIRECT_MESSAGE",
			Threading: "UNTHREADED_MESSAGES", UnreadCount: 1,
		},
		Threads: []ThreadGroup{
			{
				Thread:   ThreadInfo{ID: "spaces/DM/threads/t1", MessageCount: 1, LastActivity: at(t, "09:12")},
				Messages: []MessageInfo{{CreateTime: at(t, "09:12"), Sender: Sender{Name: "Linh Tran"}, Text: "hi there", Unread: true}},
			},
			{
				Thread:   ThreadInfo{ID: "spaces/DM/threads/t2", MessageCount: 1, LastActivity: at(t, "09:20")},
				Messages: []MessageInfo{{CreateTime: at(t, "09:20"), Sender: Sender{Name: "Ben", IsMe: true}, Text: "ok"}},
			},
		},
	}}}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"◆ Linh Tran                                                2 msgs · 1 unread\n" +
		"│\n" +
		"├ 09:12  Linh Tran                                                  ● unread\n" +
		"│        hi there\n" +
		"└ 09:20  Ben\n" +
		"         ok\n"
	if buf.String() != want {
		t.Fatalf("DM output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
	if strings.Contains(buf.String(), "▸") {
		t.Error("an unthreaded space must not show a thread level")
	}
}

func TestTextGroupFlatIsOneLinePerMessage(t *testing.T) {
	r := threadedResult(t)
	r.Opts = RenderOpts{Group: "flat"}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"  2026-07-25 09:12  Alpha/t1  Linh: hello\n" +
		"  2026-07-25 09:20  Alpha/t1  Ben: ok\n" +
		"● 2026-07-25 15:40  Alpha/t2  Huy: down\n"
	if buf.String() != want {
		t.Fatalf("flat output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

func TestTextGroupThreadDropsTheSpaceHeader(t *testing.T) {
	r := threadedResult(t)
	r.Opts = RenderOpts{Group: "thread"}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "◆") {
		t.Fatalf("--group thread must not print space headers:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "├─▸ ") {
		t.Fatalf("--group thread must still print threads:\n%s", buf.String())
	}
}

func TestTextWrapsLongMessages(t *testing.T) {
	r := threadedResult(t)
	r.Spaces[0].Threads[0].Messages[0].Text = strings.Repeat("word ", 30)
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if len([]rune(line)) > lineWidth {
			t.Fatalf("line exceeds %d columns: %q", lineWidth, line)
		}
	}
}

func TestSpaceListText(t *testing.T) {
	sl := SpaceList{Spaces: []SpaceInfo{
		{ID: "spaces/A", Name: "Alpha", Type: "SPACE", UnreadCount: 3},
		{ID: "spaces/DM", Name: "Linh Tran", Type: "DIRECT_MESSAGE"},
	}}
	var buf bytes.Buffer
	if err := sl.Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"◆ Alpha                                                     space · 3 unread\n" +
		"◆ Linh Tran                                                               dm\n"
	if buf.String() != want {
		t.Fatalf("spaces output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

func TestEmptyResultRendersNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := (Result{}).Text(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("empty result should print nothing, got %q", buf.String())
	}
}

// TestTextThreadedSpaceWithNoThreadsHasNoDanglingConnector covers a threaded
// space whose Threads list is empty: the header must not leave a "│"
// connector pointing at nothing.
func TestTextThreadedSpaceWithNoThreadsHasNoDanglingConnector(t *testing.T) {
	r := Result{Spaces: []SpaceGroup{{
		Space: SpaceInfo{
			ID: "spaces/A", Name: "Alpha", Type: "SPACE",
			Threading: "THREADED_MESSAGES",
			Link:      "https://chat.google.com/u/0/app/chat/A",
		},
		Threads: nil,
	}}}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "◆ Alpha                                                            0 threads\n"
	if buf.String() != want {
		t.Fatalf("empty-threads output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
	if strings.Contains(buf.String(), "│") {
		t.Fatalf("a space with no threads must not print a dangling connector:\n%q", buf.String())
	}
}

// TestTextThreadWithNoMessagesHasNoDanglingConnector covers a thread whose
// Messages list is empty: the thread line must not leave a "│" connector
// pointing at nothing beneath it.
func TestTextThreadWithNoMessagesHasNoDanglingConnector(t *testing.T) {
	r := Result{Spaces: []SpaceGroup{{
		Space: SpaceInfo{
			ID: "spaces/A", Name: "Alpha", Type: "SPACE",
			Threading: "THREADED_MESSAGES",
			Link:      "https://chat.google.com/u/0/app/chat/A",
		},
		Threads: []ThreadGroup{
			{
				Thread: ThreadInfo{
					ID:   "spaces/A/threads/t1",
					Link: "https://chat.google.com/room/A/t1/t1",
				},
				Messages: nil,
			},
		},
	}}}
	var buf bytes.Buffer
	if err := r.Text(&buf); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"◆ Alpha                                                             1 thread\n" +
		"│\n" +
		"└─▸ t1                                                                0 msgs\n"
	if buf.String() != want {
		t.Fatalf("empty-messages output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
	if strings.Contains(buf.String(), "   │") {
		t.Fatalf("a thread with no messages must not print a dangling connector:\n%q", buf.String())
	}
}
