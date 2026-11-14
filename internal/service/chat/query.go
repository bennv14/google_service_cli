package chat

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

// API is the slice of the Chat API the query engine needs. *Client implements
// it; tests supply a fake so the engine can be exercised without a network.
type API interface {
	ListSpaces(ctx context.Context) ([]*chatapi.Space, error)
	GetSpace(ctx context.Context, name string) (*chatapi.Space, error)
	FindDirectMessage(ctx context.Context, userRef string) (*chatapi.Space, error)
	ListMessages(ctx context.Context, parent string, o ListOpts) ([]*chatapi.Message, error)
	SpaceReadState(ctx context.Context, spaceName string) (string, time.Time, error)
	LatestMessages(ctx context.Context, parent string, n int) ([]*chatapi.Message, error)
	// GetMessage fetches one message by name. It exists for thread titles: a
	// thread's opening message is often outside the scanned window, and there
	// is no other way to read it.
	GetMessage(ctx context.Context, name string) (*chatapi.Message, error)
}

var _ API = (*Client)(nil)

// Engine runs queries against the Chat API. One Engine serves one command
// invocation; nothing it caches in memory outlives that.
type Engine struct {
	api API
	dir Directory

	meMu   sync.Mutex
	meID   string // "users/123456789", resolved lazily
	meDone bool

	warnMu sync.Mutex
	warns  []string
}

// NewEngine returns an Engine backed by api, naming people through dir.
func NewEngine(api API, dir Directory) *Engine { return &Engine{api: api, dir: dir} }

// warn records a non-fatal problem for the command to print, ignoring repeats
// so one broken scope cannot produce one line per space.
func (e *Engine) warn(msg string) {
	e.warnMu.Lock()
	defer e.warnMu.Unlock()
	for _, w := range e.warns {
		if w == msg {
			return
		}
	}
	e.warns = append(e.warns, msg)
}

func (e *Engine) warnings() []string {
	e.warnMu.Lock()
	defer e.warnMu.Unlock()
	return e.warns
}

// saveNames persists whatever the directory learned this run. Only the caching
// decorator has anything to save; a plain directory does not implement this.
func (e *Engine) saveNames() {
	if c, ok := e.dir.(nameCache); ok {
		c.Flush()
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

	// skipThreadTitles suppresses the head-message fetch for output that has no
	// thread header to put a title in: `chat spaces --unread` prints one line
	// per space, `--group flat` and `--output table` one line per message. All
	// three would otherwise spend a request per partial thread on a label they
	// discard. It is unexported because it is not a user's choice but a
	// consequence of the output they picked — see showsThreadTitles in cmd.go.
	skipThreadTitles bool
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
	// default to a week. --unread gets no global default because each space's
	// own lastReadTime is a better lower bound, however far back it sits; see
	// spaceWindow, which falls back to the same default per space when a space
	// has no marker to start from.
	since := q.Since
	if q.Space == "" && since.IsZero() && !q.UnreadOnly {
		since = now.Add(-defaultScanWindow)
	}

	// A read marker is what makes "unread" meaningful, so --unread drops the
	// spaces whose marker could not be fetched and reports them, rather than
	// scanning them with no lower bound and then discarding every message.
	readAt := map[string]time.Time{}
	var readErrs []SpaceError
	if q.UnreadOnly {
		readAt, readErrs = e.readStates(ctx, spaces)
		spaces = withReadMarker(spaces, readAt)
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

	scans, spaceErrs := e.scanSpaces(ctx, spaces, q, since, now, readAt, meID)
	spaceErrs = append(readErrs, spaceErrs...)
	scans = applyLimit(scans, q.Limit)
	e.resolveNames(ctx, scans)
	groups := group(scans, q, since, e.dir)
	groups = applyThreadLimit(groups, q.ThreadLimit)
	// After the thread limit, so threads that were cut cost nothing; before
	// saveNames, so the names learned from head senders are persisted.
	if !q.skipThreadTitles {
		e.resolveThreadTitles(ctx, groups)
	}
	recount(groups)
	e.saveNames()

	res := Result{
		Spaces:   groups,
		Errors:   spaceErrs,
		Warnings: e.warnings(),
		Summary:  Summary{Spaces: len(spaces), Window: windowLabel(since, q.Until, now)},
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

// readStates fetches every space's read marker in parallel. Presence in the
// returned map is the signal that a marker was fetched at all: a space that is
// present with a zero time has simply never been read, which is a different
// thing from a space whose read state could not be fetched — that one comes
// back as a SpaceError instead.
func (e *Engine) readStates(ctx context.Context, spaces []*chatapi.Space) (map[string]time.Time, []SpaceError) {
	out := make(map[string]time.Time, len(spaces))
	errs := make([]*SpaceError, len(spaces))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSpaces)
launch:
	for i, sp := range spaces {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(i int, sp *chatapi.Space) {
			defer wg.Done()
			defer func() { <-sem }()
			name, ts, err := e.api.SpaceReadState(ctx, sp.Name)
			if err != nil {
				errs[i] = &SpaceError{SpaceID: sp.Name, SpaceName: sp.DisplayName, Err: err}
				return
			}
			e.rememberMe(name)
			mu.Lock()
			out[sp.Name] = ts
			mu.Unlock()
		}(i, sp)
	}
	wg.Wait()

	var failed []SpaceError
	for _, se := range errs {
		if se != nil {
			failed = append(failed, *se)
		}
	}
	return out, failed
}

// withReadMarker keeps only the spaces whose read marker was fetched.
func withReadMarker(spaces []*chatapi.Space, readAt map[string]time.Time) []*chatapi.Space {
	out := make([]*chatapi.Space, 0, len(spaces))
	for _, sp := range spaces {
		if _, ok := readAt[sp.Name]; ok {
			out = append(out, sp)
		}
	}
	return out
}

func parseUserID(readStateName string) (string, bool) {
	rest, ok := strings.CutPrefix(readStateName, "users/")
	if !ok {
		return "", false
	}
	id, _, _ := strings.Cut(rest, "/")
	if id == "" || id == "me" {
		return "", false
	}
	return "users/" + id, true
}

func (e *Engine) rememberMe(readStateName string) {
	id, ok := parseUserID(readStateName)
	if !ok {
		return
	}
	e.meMu.Lock()
	defer e.meMu.Unlock()
	if !e.meDone {
		e.meID, e.meDone = id, true
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
	since, now time.Time, readAt map[string]time.Time, meID string) ([]spaceScan, []SpaceError) {

	scans := make([]spaceScan, len(spaces))
	failed := make([]*SpaceError, len(spaces))

	var wg sync.WaitGroup
	var progMu sync.Mutex
	var done int32
	sem := make(chan struct{}, maxConcurrentSpaces)

launch:
	for i, sp := range spaces {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(i int, sp *chatapi.Space) {
			defer wg.Done()
			defer func() { <-sem }()

			lastRead, known := readAt[sp.Name]
			spaceSince := spaceWindow(q, since, now, lastRead)
			raw, err := e.api.ListMessages(ctx, sp.Name, ListOpts{
				Filter: buildMessageFilter(spaceSince, q.Until, q.Thread),
				Keep:   q.keepFunc(lastRead, known, meID),
				Limit:  q.Limit,
			})
			if err != nil {
				failed[i] = &SpaceError{SpaceID: sp.Name, SpaceName: sp.DisplayName, Err: err}
			} else {
				scans[i] = spaceScan{
					space:    sp,
					lastRead: lastRead,
					msgs:     convert(raw, lastRead, known, meID, q),
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

// spaceWindow is one space's lower time bound. --unread starts from that
// space's own read marker; a space that has never been read has no marker to
// start from, so it falls back to the default window rather than all history.
func spaceWindow(q Query, since, now, lastRead time.Time) time.Time {
	if !q.UnreadOnly {
		return since
	}
	switch {
	case lastRead.After(since):
		return lastRead
	case lastRead.IsZero() && since.IsZero():
		return now.Add(-defaultScanWindow)
	}
	return since
}

// Message fetches a single message by canonical resource name, converts it to
// MessageInfo, and resolves the author's display name and email.
func (e *Engine) Message(ctx context.Context, name, accountIndex string) (SingleMessage, error) {
	m, err := e.api.GetMessage(ctx, name)
	if err != nil {
		return SingleMessage{}, err
	}
	meID, _ := e.userID(ctx, name)
	mi, ok := messageInfo(m, time.Time{}, false, meID)
	if !ok {
		return SingleMessage{}, fmt.Errorf("malformed message create time in message %s", name)
	}

	if e.dir != nil && mi.Sender.ID != "" {
		resolved, err := e.dir.Lookup(ctx, []string{mi.Sender.ID})
		if err != nil {
			e.warn(nameWarning(err))
		}
		if p, ok := resolved[mi.Sender.ID]; ok {
			if p.Name != "" {
				mi.Sender.Name = p.Name
			}
			if p.Email != "" {
				mi.Sender.Email = p.Email
			}
		}
	}
	e.saveNames()

	return SingleMessage{
		Message:  mi,
		Warnings: e.warnings(),
	}, nil
}
