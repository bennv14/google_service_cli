package chat

import "context"

// resolveThreadTitles gives every thread a title: the text of its opening
// message. Google Chat has no thread-title field — a thread's subject lives in
// its first message, which link.go identifies by the {tid}.{mid} convention.
//
// Nothing here is cached. query.go never caches messages or read state because
// a stale answer there is wrong rather than merely old, and an opening message
// can be edited or deleted like any other. Only people are remembered between
// runs, by namecache.go, which this leaves alone.
func (e *Engine) resolveThreadTitles(_ context.Context, groups []SpaceGroup) {
	for i := range groups {
		for j := range groups[i].Threads {
			tg := &groups[i].Threads[j]
			m, ok := headInHand(tg.Messages)
			if !ok {
				continue
			}
			tg.Thread.Title = m.Text
			tg.Thread.HeadSender = m.Sender.Name
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
