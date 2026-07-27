package chat

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

// relativeRe matches the relative forms accepted by --since / --until.
var relativeRe = regexp.MustCompile(`^(\d+)([dhm])$`)

// parseWhen accepts a relative offset ("3d", "12h", "30m"), a bare date
// ("2026-07-25", read as local midnight), or a full RFC3339 timestamp. An empty
// string yields the zero time, meaning "unbounded".
func parseWhen(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if m := relativeRe.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, invalidWhen(s)
		}
		unit := map[string]time.Duration{
			"d": 24 * time.Hour,
			"h": time.Hour,
			"m": time.Minute,
		}[m[2]]
		return now.Add(-time.Duration(n) * unit), nil
	}
	// A bare date means midnight in the machine's zone, not UTC.
	if ts, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, nil
	}
	return time.Time{}, invalidWhen(s)
}

func invalidWhen(s string) error {
	return fmt.Errorf("invalid time %q: use a relative offset (3d, 12h, 30m), "+
		"a date (2026-07-25), or a full RFC3339 timestamp", s)
}

// windowLabel describes the scanned window for the summary line.
// The display unit is chosen by rounding the duration to the candidate unit,
// then checking if the rounded value fits within that unit's range.
// This ensures the rounded count never overflows into the next unit.
func windowLabel(since, until, now time.Time) string {
	const stamp = "2006-01-02 15:04"
	switch {
	case since.IsZero() && until.IsZero():
		return "all time"
	case since.IsZero():
		return "until " + until.Format(stamp)
	case !until.IsZero():
		return since.Format(stamp) + " → " + until.Format(stamp)
	}
	d := now.Sub(since)

	// Round to minutes and check if we should display in minutes.
	minutes := int(d.Round(time.Minute).Minutes())
	if minutes < 60 {
		return plural(minutes, "minute", "minutes")
	}

	// Round to hours and check if we should display in hours.
	hours := int(d.Round(time.Hour).Hours())
	if hours < 24 {
		return plural(hours, "hour", "hours")
	}

	// Display in days.
	return plural(int(d.Round(24*time.Hour).Hours()/24), "day", "days")
}

// API is the slice of the Chat API the query engine needs. *Client implements
// it; tests supply a fake so the engine can be exercised without a network.
type API interface {
	ListSpaces(ctx context.Context) ([]*chatapi.Space, error)
	GetSpace(ctx context.Context, name string) (*chatapi.Space, error)
	FindDirectMessage(ctx context.Context, userRef string) (*chatapi.Space, error)
	ListMessages(ctx context.Context, parent, filter string, limit int) ([]*chatapi.Message, error)
	SpaceReadState(ctx context.Context, spaceName string) (string, time.Time, error)
	ListMembers(ctx context.Context, spaceName string) ([]*chatapi.Membership, error)
}

var _ API = (*Client)(nil)

// Engine runs queries against the Chat API. One Engine serves one command
// invocation; nothing it caches outlives that.
type Engine struct {
	api API

	meMu   sync.Mutex
	meID   string // "users/123456789", resolved lazily
	meDone bool
}

// NewEngine returns an Engine backed by api.
func NewEngine(api API) *Engine { return &Engine{api: api} }

// spaceTypeAliases maps the --type flag's values onto Space.spaceType.
var spaceTypeAliases = map[string]string{
	"space": "SPACE",
	"dm":    "DIRECT_MESSAGE",
	"group": "GROUP_CHAT",
}

// matchesSpaceType reports whether sp passes the --type filter. No filter
// matches everything.
func matchesSpaceType(sp *chatapi.Space, types []string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if spaceTypeAliases[strings.ToLower(t)] == sp.SpaceType {
			return true
		}
	}
	return false
}

// buildMessageFilter composes the server-side filter for messages.list.
// Timestamps are normalised to UTC because the API compares them as instants.
func buildMessageFilter(since, until time.Time, thread string) string {
	var parts []string
	if !since.IsZero() {
		parts = append(parts, fmt.Sprintf("create_time > %q", since.UTC().Format(time.RFC3339)))
	}
	if !until.IsZero() {
		parts = append(parts, fmt.Sprintf("create_time < %q", until.UTC().Format(time.RFC3339)))
	}
	if thread != "" {
		parts = append(parts, fmt.Sprintf("thread.name = %q", thread))
	}
	return strings.Join(parts, " AND ")
}

// resolveSpace turns a user-supplied --space value into a Space. A raw
// "spaces/…" ID is used directly; a value containing "@" is treated as a user
// and resolved to the DM with them; anything else is matched against display
// names. Nothing is cached: every command re-resolves, which costs one extra
// call but never shows a stale answer.
func (e *Engine) resolveSpace(ctx context.Context, ref string) (*chatapi.Space, error) {
	switch {
	case strings.HasPrefix(ref, "spaces/"):
		return e.api.GetSpace(ctx, ref)
	case strings.Contains(ref, "@"):
		return e.api.FindDirectMessage(ctx, "users/"+ref)
	}

	spaces, err := e.api.ListSpaces(ctx)
	if err != nil {
		return nil, err
	}
	var exact, partial []*chatapi.Space
	lower := strings.ToLower(ref)
	for _, sp := range spaces {
		name := strings.ToLower(sp.DisplayName)
		switch {
		case name == lower:
			exact = append(exact, sp)
		case name != "" && strings.Contains(name, lower):
			partial = append(partial, sp)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return nil, fmt.Errorf("no space matches %q (try the space ID, or an email for a DM)", ref)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d spaces; use one of these IDs:", ref, len(candidates))
		for _, sp := range candidates {
			fmt.Fprintf(&b, "\n  %s  (%s)", sp.DisplayName, sp.Name)
		}
		return nil, fmt.Errorf("%s", b.String())
	}
}

const (
	// defaultScanWindow bounds a scan of every space when the user gave no
	// --since, so one stray command cannot pull down all history everywhere.
	defaultScanWindow = 7 * 24 * time.Hour
	// maxConcurrentSpaces caps the fan-out; more than this invites 429s.
	maxConcurrentSpaces = 8
	// partialSlack is how close to the window's edge a thread's earliest
	// message must sit before we call the thread partial, in the fallback path
	// where message names do not follow the {tid}.{mid} convention.
	partialSlack = time.Minute
)

// Query is one read request. Zero values mean "unbounded" throughout.
type Query struct {
	Space        string // "" scans every space the user belongs to
	Thread       string // "spaces/X/threads/Y"; the API allows only one
	Since, Until time.Time
	UnreadOnly   bool
	MentionsMe   bool
	SpaceTypes   []string  // --type values: space | dm | group
	Limit        int       // max messages in the merged result; 0 = unlimited
	ThreadLimit  int       // max threads in the merged result; 0 = unlimited
	AccountIndex string    // browser account index used in space links
	Now          time.Time // injected by tests; zero means time.Now()
	Progress     io.Writer // progress ticks for expensive scans; nil = quiet
}

// Run executes the query. A failure that affects a single space is collected
// into Result.Errors and does not fail the run; only a failure that makes the
// whole query impossible is returned as an error.
func (e *Engine) Run(ctx context.Context, q Query) (Result, error) {
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}

	spaces, err := e.selectSpaces(ctx, q)
	if err != nil {
		return Result{}, err
	}

	// Scanning every space with no lower bound would drag in all history, so
	// default to a week. --unread needs no default: each space's lastReadTime
	// is already a lower bound, and a user-supplied --since still applies as
	// the later of the two.
	since := q.Since
	if q.Space == "" && since.IsZero() && !q.UnreadOnly {
		since = now.Add(-defaultScanWindow)
	}

	readAt := map[string]time.Time{}
	if q.UnreadOnly {
		readAt = e.readStates(ctx, spaces)
	}
	var meID string
	if len(spaces) > 0 {
		id, err := e.userID(ctx, spaces[0].Name)
		if err != nil {
			if q.UnreadOnly || q.MentionsMe {
				return Result{}, fmt.Errorf("cannot determine your own user ID, "+
					"which --unread and --mention-me need: %w", err)
			}
		} else {
			meID = id
		}
	}

	scans, spaceErrs := e.scanSpaces(ctx, spaces, q, since, readAt, meID)
	scans = applyLimit(scans, q.Limit)
	groups := e.group(ctx, scans, q, since, meID)
	groups = applyThreadLimit(groups, q.ThreadLimit)
	recount(groups)

	res := Result{
		Spaces:  groups,
		Errors:  spaceErrs,
		Summary: Summary{Spaces: len(spaces), Window: windowLabel(since, q.Until, now)},
	}
	for _, g := range groups {
		res.Summary.Threads += len(g.Threads)
		for _, tg := range g.Threads {
			res.Summary.Messages += len(tg.Messages)
			for _, m := range tg.Messages {
				if m.Unread {
					res.Summary.Unread++
				}
			}
		}
	}
	return res, nil
}

// selectSpaces resolves --space, or lists every space and applies --type.
func (e *Engine) selectSpaces(ctx context.Context, q Query) ([]*chatapi.Space, error) {
	if q.Space != "" {
		sp, err := e.resolveSpace(ctx, q.Space)
		if err != nil {
			return nil, err
		}
		return []*chatapi.Space{sp}, nil
	}
	all, err := e.api.ListSpaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*chatapi.Space, 0, len(all))
	for _, sp := range all {
		if matchesSpaceType(sp, q.SpaceTypes) {
			out = append(out, sp)
		}
	}
	return out, nil
}

// readStates fetches every space's read marker in parallel. A space whose read
// state cannot be fetched is simply treated as never read.
func (e *Engine) readStates(ctx context.Context, spaces []*chatapi.Space) map[string]time.Time {
	out := make(map[string]time.Time, len(spaces))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSpaces)
	for _, sp := range spaces {
		wg.Add(1)
		go func(sp *chatapi.Space) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			name, ts, err := e.api.SpaceReadState(ctx, sp.Name)
			if err != nil {
				return
			}
			e.rememberMe(name)
			mu.Lock()
			out[sp.Name] = ts
			mu.Unlock()
		}(sp)
	}
	wg.Wait()
	return out
}

// rememberMe extracts the caller's own ID from a canonical read-state name,
// "users/123456789/spaces/…/spaceReadState". The API resolves the `me` alias in
// the response, which is the cheapest way to learn our ID: it needs no scope
// beyond the read-state one we already hold.
func (e *Engine) rememberMe(readStateName string) {
	rest, ok := strings.CutPrefix(readStateName, "users/")
	if !ok {
		return
	}
	id, _, _ := strings.Cut(rest, "/")
	if id == "" || id == "me" {
		return
	}
	e.meMu.Lock()
	defer e.meMu.Unlock()
	if !e.meDone {
		e.meID, e.meDone = "users/"+id, true
	}
}

// userID returns the caller's own user ID, resolved once per command.
func (e *Engine) userID(ctx context.Context, anySpace string) (string, error) {
	e.meMu.Lock()
	done, id := e.meDone, e.meID
	e.meMu.Unlock()
	if done {
		return id, nil
	}

	name, _, err := e.api.SpaceReadState(ctx, anySpace)
	if err != nil {
		return "", err
	}
	e.rememberMe(name)

	e.meMu.Lock()
	defer e.meMu.Unlock()
	if !e.meDone {
		return "", fmt.Errorf("the Chat API did not resolve users/me to a numeric ID (got %q)", name)
	}
	return e.meID, nil
}

// spaceScan is one space's slice of the fan-out.
type spaceScan struct {
	space    *chatapi.Space
	lastRead time.Time
	msgs     []MessageInfo
}

// scanSpaces fans messages.list out across spaces, bounded to
// maxConcurrentSpaces. Each space gets its own filter, because --unread folds
// that space's own lastReadTime into the lower bound.
func (e *Engine) scanSpaces(ctx context.Context, spaces []*chatapi.Space, q Query,
	since time.Time, readAt map[string]time.Time, meID string) ([]spaceScan, []SpaceError) {

	scans := make([]spaceScan, len(spaces))
	failed := make([]*SpaceError, len(spaces))

	var wg sync.WaitGroup
	var progMu sync.Mutex
	var done int32
	sem := make(chan struct{}, maxConcurrentSpaces)

	for i, sp := range spaces {
		wg.Add(1)
		go func(i int, sp *chatapi.Space) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			lastRead := readAt[sp.Name]
			spaceSince := since
			if q.UnreadOnly && lastRead.After(spaceSince) {
				spaceSince = lastRead
			}
			filter := buildMessageFilter(spaceSince, q.Until, q.Thread)
			raw, err := e.api.ListMessages(ctx, sp.Name, filter, q.Limit)
			if err != nil {
				failed[i] = &SpaceError{SpaceID: sp.Name, SpaceName: sp.DisplayName, Err: err}
			} else {
				scans[i] = spaceScan{
					space:    sp,
					lastRead: lastRead,
					msgs:     convert(sp, raw, lastRead, meID, q),
				}
			}
			if q.Progress != nil {
				n := atomic.AddInt32(&done, 1)
				progMu.Lock()
				fmt.Fprintf(q.Progress, "\rscanning spaces %d/%d", n, len(spaces))
				progMu.Unlock()
			}
		}(i, sp)
	}
	wg.Wait()
	if q.Progress != nil {
		fmt.Fprintln(q.Progress)
	}

	var errs []SpaceError
	out := make([]spaceScan, 0, len(spaces))
	for i := range spaces {
		if failed[i] != nil {
			errs = append(errs, *failed[i])
			continue
		}
		out = append(out, scans[i])
	}
	return out, errs
}

// convert turns API messages into output shapes and applies the client-side
// filters the API cannot express.
func convert(sp *chatapi.Space, raw []*chatapi.Message, lastRead time.Time, meID string, q Query) []MessageInfo {
	out := make([]MessageInfo, 0, len(raw))
	for _, m := range raw {
		ts, err := time.Parse(time.RFC3339Nano, m.CreateTime)
		if err != nil {
			continue // a message we cannot place in time cannot be sorted or filtered
		}
		head, _ := isThreadHead(m.Name)
		mi := MessageInfo{
			ID:           m.Name,
			ThreadID:     threadIDOf(m),
			IsThreadHead: head,
			CreateTime:   ts.Local(),
			Text:         m.Text,
			Mentions:     []string{},
			Link:         messageLink(m.Name),
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
		// Unread deliberately excludes your own messages: comparing times alone
		// would mark everything you just sent as unread.
		mi.Unread = !lastRead.IsZero() && mi.CreateTime.After(lastRead) && !mi.Sender.IsMe

		if q.MentionsMe && !mi.MentionsMe {
			continue
		}
		if q.UnreadOnly && !mi.Unread {
			continue
		}
		out = append(out, mi)
	}
	return out
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

// applyLimit keeps the newest limit messages across every space. Cutting before
// grouping means counts, thread labels, and partial flags are all computed on
// exactly what will be displayed.
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
func (e *Engine) group(ctx context.Context, scans []spaceScan, q Query, since time.Time, meID string) []SpaceGroup {
	var out []SpaceGroup
	for _, sc := range scans {
		if len(sc.msgs) == 0 {
			continue
		}
		out = append(out, SpaceGroup{
			Space:   e.spaceInfo(ctx, sc, q, meID),
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
		if _, _, _, ok := splitMessageName(m.ID); ok {
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

func (e *Engine) spaceInfo(ctx context.Context, sc spaceScan, q Query, meID string) SpaceInfo {
	info := SpaceInfo{
		ID:        sc.space.Name,
		Name:      e.spaceName(ctx, sc, meID),
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

// spaceName names a space. DMs have an empty displayName, so the other party is
// inferred from the messages already in hand; only a window containing nothing
// but our own messages costs an extra members.list call.
func (e *Engine) spaceName(ctx context.Context, sc spaceScan, meID string) string {
	if sc.space.DisplayName != "" {
		return sc.space.DisplayName
	}
	for _, m := range sc.msgs {
		if !m.Sender.IsMe && m.Sender.Name != "" {
			return m.Sender.Name
		}
	}
	if members, err := e.api.ListMembers(ctx, sc.space.Name); err == nil {
		for _, mem := range members {
			if mem.Member == nil || mem.Member.Name == meID {
				continue
			}
			if mem.Member.DisplayName != "" {
				return mem.Member.DisplayName
			}
			return mem.Member.Name
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

// Spaces lists the spaces the caller belongs to. Without UnreadOnly this is a
// single spaces.list call and every space is returned, including quiet ones.
// With UnreadOnly it runs a full scan and keeps only spaces that actually have
// unread messages, annotated with the count.
func (e *Engine) Spaces(ctx context.Context, q Query) (SpaceList, error) {
	if q.UnreadOnly {
		res, err := e.Run(ctx, q)
		if err != nil {
			return SpaceList{}, err
		}
		var out SpaceList
		for _, sg := range res.Spaces {
			if sg.Space.UnreadCount == 0 {
				continue
			}
			out.Spaces = append(out.Spaces, sg.Space)
		}
		return out, nil
	}

	spaces, err := e.selectSpaces(ctx, q)
	if err != nil {
		return SpaceList{}, err
	}
	out := SpaceList{Spaces: make([]SpaceInfo, 0, len(spaces))}
	for _, sp := range spaces {
		name := sp.DisplayName
		if name == "" {
			name = shortID(sp.Name) // DMs have no display name and no messages to infer one from
		}
		out.Spaces = append(out.Spaces, SpaceInfo{
			ID:        sp.Name,
			Name:      name,
			Type:      sp.SpaceType,
			Threading: sp.SpaceThreadingState,
			Link:      spaceLink(sp.Name, sp.SpaceUri, q.AccountIndex),
		})
	}
	return out, nil
}
