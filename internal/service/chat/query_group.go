package chat

import (
	"context"
	"sort"
	"strings"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

// keepFunc is the client-side filter handed down to the API layer so that a
// --limit is spent on messages the user asked for. It returns nil when there is
// nothing to filter, which lets the API layer page as cheaply as it can.
func (q Query) keepFunc(lastRead time.Time, readKnown bool, meID string) func(*chatapi.Message) bool {
	if !q.MentionsMe && !q.UnreadOnly {
		return nil
	}
	return func(m *chatapi.Message) bool {
		mi, ok := messageInfo(m, lastRead, readKnown, meID)
		return ok && q.wants(mi)
	}
}

// wants reports whether a message passes the filters the API cannot express
// server-side. It must agree with keepFunc, so both go through it.
func (q Query) wants(mi MessageInfo) bool {
	if q.MentionsMe && !mi.MentionsMe {
		return false
	}
	if q.UnreadOnly && !mi.Unread {
		return false
	}
	return true
}

// convert turns API messages into output shapes and applies the client-side
// filters the API cannot express.
func convert(raw []*chatapi.Message, lastRead time.Time, readKnown bool, meID string, q Query) []MessageInfo {
	out := make([]MessageInfo, 0, len(raw))
	for _, m := range raw {
		mi, ok := messageInfo(m, lastRead, readKnown, meID)
		if !ok || !q.wants(mi) {
			continue
		}
		out = append(out, mi)
	}
	return out
}

// attachmentInfos converts Chat API attachments into view AttachmentInfo structs.
func attachmentInfos(atts []*chatapi.Attachment) []AttachmentInfo {
	if len(atts) == 0 {
		return nil
	}
	out := make([]AttachmentInfo, 0, len(atts))
	for _, a := range atts {
		if a == nil {
			continue
		}
		info := AttachmentInfo{
			Name:        a.Name,
			ContentName: a.ContentName,
			ContentType: a.ContentType,
			Source:      a.Source,
			DownloadURI: a.DownloadUri,
		}
		if a.AttachmentDataRef != nil {
			info.ResourceName = a.AttachmentDataRef.ResourceName
		}
		if a.DriveDataRef != nil {
			info.DriveFileID = a.DriveDataRef.DriveFileId
		}
		out = append(out, info)
	}
	return out
}

// messageInfo converts one API message. ok is false for a message that cannot
// be placed in time, which makes it impossible to sort or filter.
func messageInfo(m *chatapi.Message, lastRead time.Time, readKnown bool, meID string) (MessageInfo, bool) {
	ts, err := time.Parse(time.RFC3339Nano, m.CreateTime)
	if err != nil {
		return MessageInfo{}, false
	}
	head, _ := isThreadHead(m.Name)
	mi := MessageInfo{
		ID:           formatMessageID(m.Name),
		RawName:      m.Name,
		ThreadID:     threadIDOf(m),
		IsThreadHead: head,
		CreateTime:   ts.Local(),
		Text:         m.Text,
		Mentions:     []string{},
		Link:         messageLink(m.Name),
		Attachments:  attachmentInfos(m.Attachment),
	}
	if m.Sender != nil {
		mi.Sender = Sender{
			ID:   m.Sender.Name,
			Name: m.Sender.DisplayName,
			Type: m.Sender.Type,
			IsMe: meID != "" && m.Sender.Name == meID,
		}
		if mi.Sender.Name == "" {
			mi.Sender.Name = m.Sender.Name // fall back to the raw users/… ID
		}
	}
	for _, a := range m.Annotations {
		if a == nil || a.Type != "USER_MENTION" || a.UserMention == nil || a.UserMention.User == nil {
			continue
		}
		mi.Mentions = append(mi.Mentions, a.UserMention.User.Name)
		if meID != "" && a.UserMention.User.Name == meID {
			mi.MentionsMe = true
		}
	}
	// A zero lastRead that was actually fetched means the space has never been
	// opened, so everything in it is unread. Unread deliberately excludes your
	// own messages: comparing times alone would mark everything you just sent
	// as unread.
	mi.Unread = readKnown && mi.CreateTime.After(lastRead) && !mi.Sender.IsMe
	return mi, true
}

// threadIDOf prefers the API's thread name, derives it from the message name
// when absent, and falls back to the message's own name so an unrecognised
// message forms its own single-message thread instead of merging with others.
func threadIDOf(m *chatapi.Message) string {
	if m.Thread != nil && m.Thread.Name != "" {
		return m.Thread.Name
	}
	if sid, tid, _, ok := splitMessageName(m.Name); ok {
		return "spaces/" + sid + "/threads/" + tid
	}
	return m.Name
}

// resolveNames replaces raw users/… IDs with real people. Its position in Run
// is deliberate: after applyLimit, so only people who will actually appear cost
// a lookup, and before group, so spaceName can name a DM after the person in it
// instead of after their ID. A directory failure is a warning and nothing more.
func (e *Engine) resolveNames(ctx context.Context, scans []spaceScan) {
	seen := map[string]bool{}
	var ids []string
	for _, sc := range scans {
		for _, m := range sc.msgs {
			if id := m.Sender.ID; id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	people, err := e.dir.Lookup(ctx, ids)
	if err != nil {
		e.warn(nameWarning(err))
	}
	for i := range scans {
		for j := range scans[i].msgs {
			p, ok := people[scans[i].msgs[j].Sender.ID]
			if !ok {
				continue // leave the raw ID; an unknown name is not a failure
			}
			scans[i].msgs[j].Sender.Name = p.Name
			scans[i].msgs[j].Sender.Email = p.Email
		}
	}
}

// applyLimit keeps the newest limit messages across every space. Cutting before
// grouping means counts and partial flags are computed on exactly what will be
// displayed. Thread labels are not: a limit that cuts away a thread's opening
// message makes the thread partial, and resolveThreadTitles then fetches back
// the message the cut discarded, so the label still names whoever started it.
func applyLimit(scans []spaceScan, limit int) []spaceScan {
	if limit <= 0 {
		return scans
	}
	type ref struct{ scan, idx int }
	var all []ref
	for i := range scans {
		for j := range scans[i].msgs {
			all = append(all, ref{i, j})
		}
	}
	if len(all) <= limit {
		return scans
	}
	sort.Slice(all, func(a, b int) bool {
		x := scans[all[a].scan].msgs[all[a].idx]
		y := scans[all[b].scan].msgs[all[b].idx]
		if !x.CreateTime.Equal(y.CreateTime) {
			return x.CreateTime.After(y.CreateTime)
		}
		return x.ID > y.ID
	})
	keep := make(map[string]bool, limit)
	for _, r := range all[:limit] {
		keep[scans[r.scan].msgs[r.idx].ID] = true
	}
	for i := range scans {
		kept := scans[i].msgs[:0]
		for _, m := range scans[i].msgs {
			if keep[m.ID] {
				kept = append(kept, m)
			}
		}
		scans[i].msgs = kept
	}
	return scans
}

// group builds the space → thread → message tree, newest activity first.
func group(scans []spaceScan, q Query, since time.Time, dir Directory) []SpaceGroup {
	var out []SpaceGroup
	for _, sc := range scans {
		if len(sc.msgs) == 0 {
			continue
		}
		out = append(out, SpaceGroup{
			Space:   spaceInfo(sc, q, dir),
			Threads: groupThreads(sc.msgs, since),
		})
	}
	sort.SliceStable(out, func(a, b int) bool {
		ta, tb := newestActivity(out[a]), newestActivity(out[b])
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		return out[a].Space.ID < out[b].Space.ID
	})
	return out
}

func newestActivity(sg SpaceGroup) time.Time {
	var t time.Time
	for _, tg := range sg.Threads {
		if tg.Thread.LastActivity.After(t) {
			t = tg.Thread.LastActivity
		}
	}
	return t
}

// groupThreads gathers messages by thread: oldest first inside a thread, most
// recently active thread first.
func groupThreads(msgs []MessageInfo, since time.Time) []ThreadGroup {
	var order []string
	byThread := map[string][]MessageInfo{}
	for _, m := range msgs {
		if _, seen := byThread[m.ThreadID]; !seen {
			order = append(order, m.ThreadID)
		}
		byThread[m.ThreadID] = append(byThread[m.ThreadID], m)
	}
	out := make([]ThreadGroup, 0, len(order))
	for _, id := range order {
		ms := byThread[id]
		sort.SliceStable(ms, func(a, b int) bool { return ms[a].CreateTime.Before(ms[b].CreateTime) })
		out = append(out, ThreadGroup{
			Thread: ThreadInfo{
				ID:           id,
				Partial:      isPartial(ms, since),
				MessageCount: len(ms),
				LastActivity: ms[len(ms)-1].CreateTime,
				Link:         threadLinkFor(id, ms),
			},
			Messages: ms,
		})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if !out[a].Thread.LastActivity.Equal(out[b].Thread.LastActivity) {
			return out[a].Thread.LastActivity.After(out[b].Thread.LastActivity)
		}
		return out[a].Thread.ID < out[b].Thread.ID
	})
	return out
}

// threadLinkFor uses the thread's own URL formula, falling back to the earliest
// message actually in hand when the thread name is unparseable.
func threadLinkFor(threadID string, ms []MessageInfo) string {
	if l := threadLink(threadID); l != "" {
		return l
	}
	if len(ms) > 0 {
		return ms[0].Link
	}
	return ""
}

// isPartial reports whether a thread began before the scanned window, so only
// its tail is visible. The reliable signal is the absence of the thread's head
// message. When no message name follows the observed {tid}.{mid} convention we
// cannot tell, and fall back to "the earliest message sits on the window edge".
func isPartial(ms []MessageInfo, since time.Time) bool {
	parseable := false
	for _, m := range ms {
		if m.IsThreadHead {
			return false
		}
		name := m.RawName
		if name == "" {
			name = m.ID
		}
		if _, _, _, ok := splitMessageName(name); ok {
			parseable = true
		}
	}
	if parseable {
		return true
	}
	if since.IsZero() || len(ms) == 0 {
		return false
	}
	return ms[0].CreateTime.Sub(since) < partialSlack
}

func spaceInfo(sc spaceScan, q Query, dir Directory) SpaceInfo {
	info := SpaceInfo{
		ID:        sc.space.Name,
		Name:      spaceName(sc, dir),
		Type:      sc.space.SpaceType,
		Threading: sc.space.SpaceThreadingState,
		Link:      spaceLink(sc.space.Name, sc.space.SpaceUri, q.AccountIndex),
	}
	if !sc.lastRead.IsZero() {
		t := sc.lastRead
		info.LastReadTime = &t
	}
	return info
}

// spaceName names a space. DMs and group chats have no displayName — the API
// never returns one — so the other party is inferred from the messages already
// in hand. There is no second source: spaces.members.list needs a scope gsvc
// does not request, and returns bare IDs even when it succeeds.
func spaceName(sc spaceScan, dir Directory) string {
	if sc.space.DisplayName != "" {
		return sc.space.DisplayName
	}
	for _, m := range sc.msgs {
		if !m.Sender.IsMe && m.Sender.Name != "" && !strings.HasPrefix(m.Sender.Name, "users/") {
			return m.Sender.Name
		}
	}
	if c, ok := dir.(nameCache); ok {
		if p, ok := c.Recall(sc.space.Name); ok && p.Name != "" {
			return p.Name
		}
	}
	return shortID(sc.space.Name)
}

// applyThreadLimit keeps the most recently active threads across the whole
// result. `chat threads` cuts threads, not messages — and it saves no API calls,
// because the thread list can only be derived from messages already fetched.
func applyThreadLimit(groups []SpaceGroup, limit int) []SpaceGroup {
	if limit <= 0 {
		return groups
	}
	type ref struct {
		g, t int
		at   time.Time
		id   string
	}
	var all []ref
	for i := range groups {
		for j := range groups[i].Threads {
			all = append(all, ref{i, j, groups[i].Threads[j].Thread.LastActivity, groups[i].Threads[j].Thread.ID})
		}
	}
	if len(all) <= limit {
		return groups
	}
	sort.Slice(all, func(a, b int) bool {
		if !all[a].at.Equal(all[b].at) {
			return all[a].at.After(all[b].at)
		}
		return all[a].id > all[b].id
	})
	keep := make(map[string]bool, limit)
	for _, r := range all[:limit] {
		keep[r.id] = true
	}
	out := groups[:0]
	for _, g := range groups {
		kept := make([]ThreadGroup, 0, len(g.Threads))
		for _, tg := range g.Threads {
			if keep[tg.Thread.ID] {
				kept = append(kept, tg)
			}
		}
		if len(kept) == 0 {
			continue
		}
		g.Threads = kept
		out = append(out, g)
	}
	return out
}

// recount refreshes per-space counters after the limits removed messages, so
// space.unreadCount always matches what is actually displayed.
func recount(groups []SpaceGroup) {
	for i := range groups {
		n := 0
		for _, tg := range groups[i].Threads {
			for _, m := range tg.Messages {
				if m.Unread {
					n++
				}
			}
		}
		groups[i].Space.UnreadCount = n
	}
}
