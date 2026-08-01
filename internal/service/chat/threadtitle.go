package chat

import (
	"context"
	"sort"
	"sync"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

// headRef locates a thread whose opening message has to be fetched: where the
// thread sits in the result, and the resource name of the message to get.
type headRef struct {
	group  int
	thread int
	name   string
}

// resolveThreadTitles gives every thread a title: the text of its opening
// message. Google Chat has no thread-title field — a thread's subject lives in
// its first message, which link.go identifies by the {tid}.{mid} convention.
//
// Nothing here is cached. query.go never caches messages or read state because
// a stale answer there is wrong rather than merely old, and an opening message
// can be edited or deleted like any other. Only people are remembered between
// runs, by namecache.go, which this leaves alone.
func (e *Engine) resolveThreadTitles(ctx context.Context, groups []SpaceGroup) {
	var refs []headRef
	for i := range groups {
		for j := range groups[i].Threads {
			tg := &groups[i].Threads[j]
			if m, ok := headInHand(tg.Messages); ok {
				tg.Thread.Title = m.Text
				tg.Thread.HeadSender = m.Sender.Name
				continue
			}
			name, ok := headMessageName(tg.Thread.ID)
			if !ok {
				continue // no derivable head; the thread simply goes untitled
			}
			refs = append(refs, headRef{group: i, thread: j, name: name})
		}
	}
	if len(refs) == 0 {
		return
	}

	// A head the scan never saw usually introduces a sender the scan never
	// saw either, so their names are collected and resolved together.
	pending := map[string][]*ThreadInfo{}
	heads, failures := e.fetchHeads(ctx, refs)
	// One line however many heads were unreadable. A deleted message, a space
	// we lost access to, or a plain 404 all land here, and none of them is
	// worth failing a read command over.
	if failures > 0 {
		e.warn(plural(failures, "thread", "threads") + ": could not read the opening message")
	}
	for i, m := range heads {
		if m == nil {
			continue
		}
		// A head that cannot be placed in time is as unusable as one that
		// could not be fetched, and convert() drops such messages too.
		mi, ok := messageInfo(m, time.Time{}, false, "")
		if !ok {
			continue
		}
		th := &groups[refs[i].group].Threads[refs[i].thread].Thread
		th.Title = mi.Text
		th.HeadSender = mi.Sender.Name
		if id := mi.Sender.ID; id != "" {
			pending[id] = append(pending[id], th)
		}
	}
	e.nameHeads(ctx, pending)
}

// nameHeads turns the raw users/… IDs the fetched heads carried into people, in
// one lookup however many threads there are. This reuses the people cache;
// only message caching is off the table. A name we fail to learn leaves the raw
// ID in place, exactly as resolveNames does — an unknown name is not a failure.
func (e *Engine) nameHeads(ctx context.Context, pending map[string][]*ThreadInfo) {
	if len(pending) == 0 {
		return
	}
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids) // map order is random; the request must not be

	people, err := e.dir.Lookup(ctx, ids)
	if err != nil {
		e.warn(nameWarning(err))
	}
	for id, threads := range pending {
		p, ok := people[id]
		if !ok || p.Name == "" {
			continue
		}
		for _, th := range threads {
			th.HeadSender = p.Name
		}
	}
}

// headInHand returns the thread's own opening message when the scan already
// caught it — the ordinary case for a thread that started inside the window.
// Taking the title from it costs no request at all.
func headInHand(ms []MessageInfo) (MessageInfo, bool) {
	for _, m := range ms {
		if m.IsThreadHead {
			return m, true
		}
	}
	return MessageInfo{}, false
}

// headMessageName derives the opening message's resource name from a thread's:
// spaces/{sid}/threads/{tid} → spaces/{sid}/messages/{tid}.{tid}, because the
// first message of a thread is the one whose messageID equals the threadID.
// See the package doc in link.go. A thread ID that does not parse yields no
// name, and no request is issued for it.
func headMessageName(threadID string) (string, bool) {
	sid, tid, ok := splitThreadName(threadID)
	if !ok {
		return "", false
	}
	return "spaces/" + sid + "/messages/" + tid + "." + tid, true
}

// fetchHeads fetches the listed opening messages, bounded to
// maxConcurrentSpaces like every other fan-out in this package. The Chat API
// has no batch-get, so N missing heads cost N requests.
//
// The messages come back one per ref, positionally; a nil entry means that
// fetch failed. The count is of fetches that were attempted and failed, so a
// cancelled run — where the remaining refs are never attempted — does not
// report a hundred deleted messages that were nothing of the kind.
func (e *Engine) fetchHeads(ctx context.Context, refs []headRef) ([]*chatapi.Message, int) {
	out := make([]*chatapi.Message, len(refs))
	failed := make([]bool, len(refs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSpaces)
launch:
	for i, r := range refs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(i int, r headRef) {
			defer wg.Done()
			defer func() { <-sem }()

			m, err := e.api.GetMessage(ctx, r.name)
			if err != nil {
				failed[i] = true
				return
			}
			out[i] = m
		}(i, r)
	}
	wg.Wait()

	n := 0
	for _, f := range failed {
		if f {
			n++
		}
	}
	return out, n
}
