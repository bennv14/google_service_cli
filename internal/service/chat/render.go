package chat

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bennv14/google_service_cli/internal/output"
)

const (
	// lineWidth is the column the right-hand metadata is aligned to, measured
	// on undecorated text so colour and hyperlinks never shift the layout.
	lineWidth = 76
	// textWidth wraps message bodies.
	textWidth = 60
	// excerptWidth truncates the quoted snippet in a thread label.
	excerptWidth = 44
)

// ANSI SGR codes, applied only when RenderOpts.Color is set.
const (
	cSpace  = "1;36"
	cThread = "35"
	cDim    = "2"
	cSender = "1"
	cUnread = "33"
	cWarn   = "33"
)

// seg is decorated text that remembers its undecorated width, so alignment can
// be computed on what the user sees rather than on escape sequences.
type seg struct{ plain, out string }

func joinSegs(ss ...seg) seg {
	var out seg
	for _, s := range ss {
		out.plain += s.plain
		out.out += s.out
	}
	return out
}

func (o RenderOpts) seg(code, text string) seg {
	return seg{plain: text, out: o.paint(code, text)}
}

// linkSeg renders text as an OSC 8 terminal hyperlink when enabled, so the URL
// takes up no space on screen.
func (o RenderOpts) linkSeg(code, url, text string) seg {
	return seg{plain: text, out: o.hyper(url, o.paint(code, text))}
}

func (o RenderOpts) paint(code, s string) string {
	if !o.Color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (o RenderOpts) hyper(url, text string) string {
	if !o.Hyperlinks || url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// writeRow writes prefix + left, then pads so right ends at lineWidth. An empty
// right side emits no padding, so lines never carry trailing spaces.
func writeRow(w io.Writer, prefix string, left, right seg) {
	fmt.Fprint(w, prefix, left.out)
	if right.plain != "" {
		n := lineWidth -
			utf8.RuneCountInString(prefix) -
			utf8.RuneCountInString(left.plain) -
			utf8.RuneCountInString(right.plain)
		if n < 2 {
			n = 2
		}
		fmt.Fprint(w, strings.Repeat(" ", n), right.out)
	}
	fmt.Fprintln(w)
}

// These pin Result and SpaceList to output.TextView at compile time, so a
// signature drift on either Text method fails the build instead of surfacing
// as a runtime "cannot render as text" error.
var (
	_ output.TextView = Result{}
	_ output.TextView = SpaceList{}
)

// Text renders the result as a tree: space → thread → message → body.
func (r Result) Text(w io.Writer) error {
	bw := bufio.NewWriter(w)
	if r.Opts.Group == "flat" {
		renderFlat(bw, r)
		return bw.Flush()
	}
	for i, sg := range r.Spaces {
		if i > 0 {
			fmt.Fprintln(bw)
		}
		renderSpace(bw, sg, r.Opts)
	}
	return bw.Flush()
}

// showThreads decides whether a space gets a thread level. Spaces that support
// threading do; DMs and group DMs do not, because there every message is its
// own thread and the extra level is pure noise. --group overrides.
func showThreads(sg SpaceGroup, o RenderOpts) bool {
	switch o.Group {
	case "space", "thread":
		return true
	}
	return sg.Space.Threading != "UNTHREADED_MESSAGES"
}

func renderSpace(w io.Writer, sg SpaceGroup, o RenderOpts) {
	threaded := showThreads(sg, o)
	msgs := allMessages(sg)
	// hasChildren tells the header whether a thread or message actually
	// follows, so the "│" connector never dangles over an empty level.
	hasChildren := threaded && len(sg.Threads) > 0 || !threaded && len(msgs) > 0

	if o.Group != "thread" {
		label := "◆ " + sg.Space.Name
		writeRow(w, "", o.linkSeg(cSpace, sg.Space.Link, label), spaceMeta(sg, threaded, o))
		if o.ShowLinks && sg.Space.Link != "" {
			fmt.Fprintf(w, "  %s\n", sg.Space.Link)
		}
		if hasChildren {
			fmt.Fprintln(w, "│")
		}
	}

	if !threaded {
		renderMessages(w, msgs, "", o)
		return
	}
	for i, tg := range sg.Threads {
		last := i == len(sg.Threads)-1
		renderThread(w, tg, last, o)
		if !last {
			fmt.Fprintln(w, "│")
		}
	}
}

func renderThread(w io.Writer, tg ThreadGroup, last bool, o RenderOpts) {
	branch, cont := "├─▸ ", "│  "
	if last {
		branch, cont = "└─▸ ", "   "
	}
	writeRow(w, branch, o.linkSeg(cThread, tg.Thread.Link, threadLabel(tg)), threadMeta(tg, o))
	if o.ShowLinks && tg.Thread.Link != "" {
		fmt.Fprintf(w, "%s%s\n", cont, tg.Thread.Link)
	}
	// Only draw the connector down to the messages when there are any;
	// an empty thread must not leave a dangling "│".
	if len(tg.Messages) > 0 {
		fmt.Fprintf(w, "%s│\n", cont)
	}
	renderMessages(w, tg.Messages, cont, o)
}

// renderMessages prints messages under cont, which is the continuation column
// of whatever level encloses them ("" directly under a space).
func renderMessages(w io.Writer, msgs []MessageInfo, cont string, o RenderOpts) {
	for i, m := range msgs {
		last := i == len(msgs)-1
		branch, body := "├ ", cont+"│        "
		if last {
			branch, body = "└ ", cont+"         "
		}
		left := joinSegs(
			o.seg(cDim, m.CreateTime.Format("15:04")),
			seg{plain: "  ", out: "  "},
			o.seg(cSender, m.Sender.Name),
		)
		var right seg
		if m.Unread {
			right = o.seg(cUnread, "● unread")
		}
		writeRow(w, cont+branch, left, right)

		for _, line := range wrapText(m.Text, textWidth) {
			fmt.Fprintf(w, "%s%s\n", body, line)
		}
		if o.ShowLinks && m.Link != "" {
			fmt.Fprintf(w, "%s%s\n", body, m.Link)
		}
	}
}

// renderFlat prints one line per message on a single timeline, each carrying
// its space and thread, for grepping.
func renderFlat(w io.Writer, r Result) {
	type row struct {
		space  string
		thread string
		msg    MessageInfo
	}
	var rows []row
	for _, sg := range r.Spaces {
		for _, tg := range sg.Threads {
			for _, m := range tg.Messages {
				rows = append(rows, row{sg.Space.Name, shortID(tg.Thread.ID), m})
			}
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		return rows[a].msg.CreateTime.Before(rows[b].msg.CreateTime)
	})
	for _, rw := range rows {
		mark := "  "
		if rw.msg.Unread {
			mark = r.Opts.paint(cUnread, "●") + " "
		}
		fmt.Fprintf(w, "%s%s  %s/%s  %s: %s\n",
			mark,
			r.Opts.paint(cDim, rw.msg.CreateTime.Format("2006-01-02 15:04")),
			rw.space, rw.thread,
			r.Opts.paint(cSender, rw.msg.Sender.Name),
			oneLine(rw.msg.Text),
		)
	}
}

// allMessages flattens a space's threads onto one timeline, oldest first.
func allMessages(sg SpaceGroup) []MessageInfo {
	var out []MessageInfo
	for _, tg := range sg.Threads {
		out = append(out, tg.Messages...)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].CreateTime.Before(out[b].CreateTime) })
	return out
}

// threadLabel names a thread by its opening message: who started it and a
// snippet. Thread.Title is that message's text even when the message itself sat
// outside the scanned window, so it wins over the earliest message in hand —
// which for a partial thread is a mid-thread reply, and would credit the wrong
// person with starting the thread and quote the wrong text.
func threadLabel(tg ThreadGroup) string {
	if tg.Thread.Title != "" {
		return withSender(tg.Thread.HeadSender, tg.Thread.Title)
	}
	if len(tg.Messages) == 0 {
		return shortID(tg.Thread.ID)
	}
	head := tg.Messages[0]
	return withSender(head.Sender.Name, head.Text)
}

// withSender formats `Sender · "excerpt"`, dropping the prefix entirely when
// there is nobody to name rather than opening the line with a bare separator.
func withSender(sender, text string) string {
	quoted := `"` + excerpt(oneLine(text), excerptWidth) + `"`
	if sender == "" {
		return quoted
	}
	return sender + " · " + quoted
}

func threadMeta(tg ThreadGroup, o RenderOpts) seg {
	s := o.seg(cDim, plural(tg.Thread.MessageCount, "msg", "msgs"))
	if tg.Thread.Partial {
		s = joinSegs(s, o.seg(cDim, " · "), o.seg(cWarn, "partial"))
	}
	return s
}

func spaceMeta(sg SpaceGroup, threaded bool, o RenderOpts) seg {
	count := len(sg.Threads)
	label := plural(count, "thread", "threads")
	if !threaded {
		n := 0
		for _, tg := range sg.Threads {
			n += len(tg.Messages)
		}
		label = plural(n, "msg", "msgs")
	}
	s := o.seg(cDim, label)
	if sg.Space.UnreadCount > 0 {
		s = joinSegs(s, o.seg(cDim, " · "),
			o.seg(cUnread, fmt.Sprintf("%d unread", sg.Space.UnreadCount)))
	}
	return s
}

// wrapText word-wraps to width runes, preserving the message's own line breaks.
func wrapText(s string, width int) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > width {
				out = append(out, line)
				line = word
				continue
			}
			line += " " + word
		}
		out = append(out, line)
	}
	return out
}

// Text renders `gsvc chat spaces` as one line per space.
func (sl SpaceList) Text(w io.Writer) error {
	bw := bufio.NewWriter(w)
	for _, s := range sl.Spaces {
		right := sl.Opts.seg(cDim, spaceTypeLabel(s.Type))
		if s.UnreadCount > 0 {
			right = joinSegs(
				sl.Opts.seg(cDim, spaceTypeLabel(s.Type)+" · "),
				sl.Opts.seg(cUnread, fmt.Sprintf("%d unread", s.UnreadCount)),
			)
		}
		writeRow(bw, "", sl.Opts.linkSeg(cSpace, s.Link, "◆ "+s.Name), right)
	}
	return bw.Flush()
}

// spaceTypeLabel maps the API's spaceType onto the words --type accepts.
func spaceTypeLabel(t string) string {
	switch t {
	case "SPACE":
		return "space"
	case "DIRECT_MESSAGE":
		return "dm"
	case "GROUP_CHAT":
		return "group"
	default:
		return strings.ToLower(t)
	}
}
