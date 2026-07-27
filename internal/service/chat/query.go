package chat

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
