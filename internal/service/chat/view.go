package chat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SpaceInfo is one Chat space as it appears in output. UnreadCount counts only
// messages inside the scanned window, so it always matches what is displayed —
// it is not the space's total unread count.
type SpaceInfo struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Threading    string     `json:"threading"`
	LastReadTime *time.Time `json:"lastReadTime,omitempty"`
	UnreadCount  int        `json:"unreadCount"`
	Link         string     `json:"link,omitempty"`
}

// Sender is the author of a message. ID is always the raw users/… resource
// name; Name is a real person's name when the directory resolved one and the
// raw ID otherwise.
type Sender struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Type  string `json:"type"`
	IsMe  bool   `json:"isMe"`
}

// MessageInfo is one message. ThreadID is carried for flat rendering and
// grouping but stays out of JSON, where the thread level already holds it.
type MessageInfo struct {
	ID           string    `json:"id"`
	ThreadID     string    `json:"-"`
	IsThreadHead bool      `json:"isThreadHead"`
	CreateTime   time.Time `json:"createTime"`
	Sender       Sender    `json:"sender"`
	Text         string    `json:"text"`
	Mentions     []string  `json:"mentions"`
	MentionsMe   bool      `json:"mentionsMe"`
	Unread       bool      `json:"unread"`
	Link         string    `json:"link,omitempty"`
}

// ThreadInfo describes a thread. Partial means the thread's first message is
// outside the scanned window, so only its tail is shown. Title is that first
// message's text, verbatim and untruncated — Chat has no thread-title field, so
// the subject of a thread is whatever its opening message says. HeadSender is
// the resolved name of whoever started it. It is in JSON despite being a name
// the renderer also prints, because a partial thread's opening message is by
// definition not in Messages: without it a consumer could read the title and
// have no way to learn who wrote it.
//
// headKnown records that the opening message was read, which is not the same as
// Title being set: a thread opened by an attachment, image, or card has a head
// with no text at all. The renderer needs the difference, because "the head says
// nothing" and "we never found the head" call for different labels.
type ThreadInfo struct {
	ID           string    `json:"id"`
	Title        string    `json:"title,omitempty"`
	HeadSender   string    `json:"headSender,omitempty"`
	headKnown    bool      // see above; unexported, like every other render-only hint
	Partial      bool      `json:"partial"`
	MessageCount int       `json:"messageCount"`
	LastActivity time.Time `json:"lastActivity"`
	Link         string    `json:"link,omitempty"`
}

// ThreadGroup is a thread and its messages, oldest first.
type ThreadGroup struct {
	Thread   ThreadInfo    `json:"thread"`
	Messages []MessageInfo `json:"messages"`
}

// SpaceGroup is a space and its threads, most recently active first.
type SpaceGroup struct {
	Space   SpaceInfo     `json:"space"`
	Threads []ThreadGroup `json:"threads"`
}

// SpaceError records that one space failed while others succeeded.
type SpaceError struct {
	SpaceID   string
	SpaceName string
	Err       error
}

// Summary is the scan report. It is written to stderr, never to stdout, so that
// `-o json > out.json` always yields valid JSON.
type Summary struct {
	Spaces   int
	Messages int
	Threads  int
	Unread   int
	Window   string // human description of the time window, e.g. "2 days"
}

func (s Summary) String() string {
	parts := []string{"scanned " + plural(s.Spaces, "space", "spaces")}
	if s.Window != "" {
		parts = append(parts, s.Window)
	}
	parts = append(parts, plural(s.Messages, "message", "messages"))
	if s.Threads > 0 {
		parts = append(parts, plural(s.Threads, "thread", "threads"))
	}
	if s.Unread > 0 {
		parts = append(parts, strconv.Itoa(s.Unread)+" unread")
	}
	return strings.Join(parts, " · ")
}

// RenderOpts controls presentation only; the engine never reads it.
type RenderOpts struct {
	Group      string // "space" | "thread" | "flat"; "" means adapt per space
	Color      bool
	Hyperlinks bool // OSC 8 terminal hyperlinks
	ShowLinks  bool // --links: print URLs on their own lines
}

// Result is the full three-level output of a message query.
type Result struct {
	Spaces   []SpaceGroup
	Summary  Summary
	Errors   []SpaceError
	Warnings []string // stderr-only, like Summary; see Summary's comment
	Opts     RenderOpts
}

// MarshalJSON emits just the space array; Summary and Errors are stderr-only.
func (r Result) MarshalJSON() ([]byte, error) {
	if r.Spaces == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(r.Spaces)
}

func (r Result) Headers() []string { return []string{"TIME", "SPACE", "THREAD", "SENDER", "TEXT"} }

// Rows flattens the tree: a table cannot express the nesting, so it shows one
// truncated line per message for piping into other tools.
func (r Result) Rows() [][]string {
	var rows [][]string
	for _, sg := range r.Spaces {
		for _, tg := range sg.Threads {
			for _, m := range tg.Messages {
				rows = append(rows, []string{
					m.CreateTime.Format("2006-01-02 15:04"),
					sg.Space.Name,
					shortID(tg.Thread.ID),
					m.Sender.Name,
					excerpt(oneLine(m.Text), 80),
				})
			}
		}
	}
	return rows
}

// SpaceList is the output of `gsvc chat spaces`.
type SpaceList struct {
	Spaces   []SpaceInfo
	Warnings []string
	Opts     RenderOpts
}

func (sl SpaceList) MarshalJSON() ([]byte, error) {
	if sl.Spaces == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(sl.Spaces)
}

func (sl SpaceList) Headers() []string {
	return []string{"ID", "NAME", "TYPE", "THREADING", "UNREAD", "LINK"}
}

func (sl SpaceList) Rows() [][]string {
	rows := make([][]string, 0, len(sl.Spaces))
	for _, s := range sl.Spaces {
		rows = append(rows, []string{
			s.ID, s.Name, s.Type, s.Threading, strconv.Itoa(s.UnreadCount), s.Link,
		})
	}
	return rows
}

// plural renders "1 space" / "2 spaces".
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// oneLine collapses all whitespace runs into single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// excerpt truncates to max runes, appending an ellipsis when it cuts.
func excerpt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
