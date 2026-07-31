package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/api/option"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
)

// testClientOpts lets tests point the Chat client at an httptest server.
// It is nil in production.
var testClientOpts []option.ClientOption

type svc struct{}

// New returns the Chat service. Everything it does is read-only: it never
// sends, edits, deletes, or marks anything as read.
func New() service.Service { return &svc{} }

func (s *svc) Name() string { return "chat" }

func (s *svc) Scopes() []string { return OAuthScopes }

func (s *svc) Command(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Read Google Chat messages (read-only)",
		Long: `Read Google Chat messages from the terminal.

Requires a Google Workspace account: the Chat API does not serve consumer
(@gmail.com) accounts. The Chat API must also be enabled in the GCP project
behind your OAuth client.`,
	}
	cmd.AddCommand(spacesCmd(d), threadsCmd(d), messagesCmd(d), unreadCmd(d), mentionsCmd(d), threadCmd(d))
	return cmd
}

// queryFlags is the full set of message-query flags; each command registers the
// subset it needs and leaves the rest at their zero values.
type queryFlags struct {
	space        string
	thread       string
	since        string
	until        string
	group        string
	types        []string
	unread       bool
	mentionsMe   bool
	links        bool
	limit        int
	refreshNames bool
}

func (f *queryFlags) query(now time.Time) (Query, error) {
	since, err := parseWhen(f.since, now)
	if err != nil {
		return Query{}, err
	}
	until, err := parseWhen(f.until, now)
	if err != nil {
		return Query{}, err
	}
	switch f.group {
	case "", "space", "thread", "flat":
	default:
		return Query{}, fmt.Errorf("invalid --group %q: use space, thread, or flat", f.group)
	}
	return Query{
		Space:      f.space,
		Thread:     f.thread,
		Since:      since,
		Until:      until,
		UnreadOnly: f.unread,
		MentionsMe: f.mentionsMe,
		SpaceTypes: f.types,
		Limit:      f.limit,
		Now:        now,
	}, nil
}

// addQueryFlags registers the flags shared by every message-reading command.
func addQueryFlags(c *cobra.Command, f *queryFlags, limitDefault int, sinceDefault string) {
	c.Flags().StringVar(&f.space, "space", "", "space ID, display name, or a DM partner's email")
	c.Flags().StringVar(&f.thread, "thread", "", "thread resource name (spaces/X/threads/Y)")
	c.Flags().StringVar(&f.since, "since", sinceDefault, "lower time bound: 3d, 12h, 30m, 2026-07-25, or RFC3339")
	c.Flags().StringVar(&f.until, "until", "", "upper time bound: same forms as --since")
	c.Flags().BoolVar(&f.unread, "unread", false, "only messages you have not read")
	c.Flags().BoolVar(&f.mentionsMe, "mention-me", false, "only messages that mention you")
	c.Flags().StringVar(&f.group, "group", "", "grouping: space | thread | flat (default: adapt to each space)")
	c.Flags().BoolVar(&f.links, "links", false, "print URLs on their own lines instead of embedding them")
	c.Flags().IntVar(&f.limit, "limit", limitDefault, "maximum number of messages in total (0 = no limit)")
	addRefreshNamesFlag(c, f)
}

// addRefreshNamesFlag registers --refresh-names. Every chat subcommand prints
// somebody's name, so every one of them can be told to re-fetch it.
func addRefreshNamesFlag(c *cobra.Command, f *queryFlags) {
	c.Flags().BoolVar(&f.refreshNames, "refresh-names", false,
		"ignore cached display names and look them up again")
}

// addSpaceTypeFlag registers --type on the commands that scan more than one
// space. `chat threads` requires --space, so it has nothing left to narrow.
func addSpaceTypeFlag(c *cobra.Command, f *queryFlags) {
	c.Flags().StringSliceVar(&f.types, "type", nil, "restrict to space types: space, dm, group")
}

// engine builds a query engine for a command. The directory shares the Chat
// client's transport: it is the same OAuth token, just a second API.
func engine(ctx context.Context, d *service.Deps, f *queryFlags) (*Engine, error) {
	hc, err := d.NewClient(ctx, OAuthScopes...)
	if err != nil {
		return nil, err
	}
	cl, err := NewClient(ctx, hc, testClientOpts...)
	if err != nil {
		return nil, err
	}
	dir, err := newDirectory(ctx, d, hc, f.refreshNames)
	if err != nil {
		return nil, err
	}
	return NewEngine(cl, dir), nil
}

// newDirectory builds the name resolver for a command. Production always stacks
// the cache over the People API: there is no way to learn whether the stored
// token carries directory.readonly without asking, so a refusal is handled as a
// warning at call time rather than detected up front.
func newDirectory(ctx context.Context, d *service.Deps, hc *http.Client, refresh bool) (Directory, error) {
	people, err := newPeopleDirectory(ctx, hc, testClientOpts...)
	if err != nil {
		return nil, err
	}
	path := nameCachePath(d)
	if path == "" {
		return people, nil
	}
	return newCachedDirectory(people, path, refresh), nil
}

// nameCachePath is where this profile's remembered names live. Splitting by
// profile keeps one account from seeing another's directory. An empty result
// means "do not cache": with no config directory and no profile there is
// nowhere safe to write.
func nameCachePath(d *service.Deps) string {
	if d.ConfigDir == "" || d.Profile.Name == "" {
		return ""
	}
	return filepath.Join(d.ConfigDir, "cache", "people-"+d.Profile.Name+".json")
}

// writer picks the output writer. chat's natural default is `text`, but the
// global default is `table`, so build our own writer unless the user chose one.
func writer(d *service.Deps) output.Writer {
	if !d.OutputExplicit && d.NewOut != nil {
		return d.NewOut("text")
	}
	return d.Out
}

// accountIndex is the browser's signed-in-account index used in space links.
// It is browser state, so it cannot be discovered from the API.
func accountIndex(d *service.Deps) string {
	if v := d.Profile.Defaults["chat_account_index"]; v != "" {
		return v
	}
	return "0"
}

func renderOpts(cmd *cobra.Command, group string, showLinks bool) RenderOpts {
	tty := isTerminal(cmd.OutOrStdout())
	return RenderOpts{Group: group, Color: tty, Hyperlinks: tty, ShowLinks: showLinks}
}

// isTerminal reports whether w is an interactive terminal; colour and OSC 8
// hyperlinks are suppressed everywhere else so pipes stay clean.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// report writes warnings, per-space failures, and the scan summary to stderr,
// so stdout carries nothing but the result.
func report(cmd *cobra.Command, res Result) {
	w := cmd.ErrOrStderr()
	reportWarnings(w, res.Warnings)
	for _, se := range res.Errors {
		name := se.SpaceName
		if name == "" {
			name = se.SpaceID
		}
		fmt.Fprintf(w, "warning: %s: %v\n", name, gerr.Friendly(se.Err))
	}
	fmt.Fprintln(w, res.Summary)
}

func reportWarnings(w io.Writer, msgs []string) {
	for _, m := range msgs {
		fmt.Fprintf(w, "warning: %s\n", m)
	}
}

// runQuery is the shared body of every message-reading command; adjust applies
// a preset's overrides after the flags have been parsed.
func runQuery(cmd *cobra.Command, d *service.Deps, f *queryFlags, adjust func(*Query)) error {
	ctx := cmd.Context()
	q, err := f.query(time.Now())
	if err != nil {
		return err
	}
	q.AccountIndex = accountIndex(d)
	if adjust != nil {
		adjust(&q)
	}
	// Scanning every space for mentions is the most expensive thing this tool
	// does; say so while it happens.
	if q.MentionsMe && q.Space == "" {
		q.Progress = cmd.ErrOrStderr()
	}
	e, err := engine(ctx, d, f)
	if err != nil {
		return err
	}
	res, err := e.Run(ctx, q)
	if err != nil {
		return gerr.Friendly(err)
	}
	res.Opts = renderOpts(cmd, f.group, f.links)
	if err := writer(d).Render(res); err != nil {
		return err
	}
	report(cmd, res)
	return nil
}

func messagesCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "messages",
		Short: "Read messages, filtered by time, space, thread, unread state, or mentions",
		Long: `Read messages. Every filter combines freely.

Filters that the API cannot express server-side (mentions) are applied locally,
so --mention-me across every space reads a lot and prints progress to stderr.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, d, f, nil)
		},
	}
	addQueryFlags(c, f, 50, "")
	addSpaceTypeFlag(c, f)
	return c
}

func unreadCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "unread",
		Short: "Read unread messages across every space (preset for: messages --unread)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, d, f, func(q *Query) { q.UnreadOnly = true })
		},
	}
	addQueryFlags(c, f, 50, "")
	addSpaceTypeFlag(c, f)
	return c
}

func mentionsCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "mentions",
		Short: "Read messages that mention you (preset for: messages --mention-me --since 7d)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, d, f, func(q *Query) { q.MentionsMe = true })
		},
	}
	addQueryFlags(c, f, 50, "7d")
	addSpaceTypeFlag(c, f)
	return c
}

func threadCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "thread <threadId>",
		Short: "Read one thread in full (preset for: messages --thread T --limit 0)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, d, f, func(q *Query) {
				q.Thread = qualifyThread(args[0], f.space)
				q.Limit = 0
			})
		},
	}
	addQueryFlags(c, f, 0, "")
	return c
}

// qualifyThread accepts either a full thread resource name or a bare thread ID
// alongside --space.
func qualifyThread(ref, space string) string {
	if strings.HasPrefix(ref, "spaces/") {
		return ref
	}
	if sid, ok := splitSpaceName(space); ok {
		return "spaces/" + sid + "/threads/" + ref
	}
	return ref
}

func threadsCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "threads",
		Short: "List threads in a space within a time window",
		Long: `List threads in a space.

The Chat API has no threads.list, so this scans messages in the window and
groups them. --limit therefore cuts the number of threads after grouping and
saves no API calls. The window is scanned in full rather than stopped early,
because stopping early would show only each thread's tail and mislabel it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, d, f, func(q *Query) {
				q.ThreadLimit, q.Limit = f.limit, 0
			})
		},
	}
	addQueryFlags(c, f, 20, "30d")
	c.Flags().Lookup("limit").Usage = "maximum number of threads (0 = no limit)"
	_ = c.MarkFlagRequired("space")
	return c
}

func spacesCmd(d *service.Deps) *cobra.Command {
	f := &queryFlags{}
	c := &cobra.Command{
		Use:   "spaces",
		Short: "List Chat spaces",
		Long: `List the spaces you belong to.

With --unread, only spaces that currently have unread messages are listed, each
with its unread count. This lists spaces, never messages.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			q, err := f.query(time.Now())
			if err != nil {
				return err
			}
			q.AccountIndex = accountIndex(d)
			e, err := engine(ctx, d, f)
			if err != nil {
				return err
			}
			list, err := e.Spaces(ctx, q)
			if err != nil {
				return gerr.Friendly(err)
			}
			list.Opts = renderOpts(cmd, "", f.links)
			if err := writer(d).Render(list); err != nil {
				return err
			}
			reportWarnings(cmd.ErrOrStderr(), list.Warnings)
			fmt.Fprintf(cmd.ErrOrStderr(), "listed %s\n", plural(len(list.Spaces), "space", "spaces"))
			return nil
		},
	}
	addSpaceTypeFlag(c, f)
	c.Flags().BoolVar(&f.unread, "unread", false, "only spaces with unread messages, with counts")
	c.Flags().BoolVar(&f.links, "links", false, "print URLs on their own lines instead of embedding them")
	addRefreshNamesFlag(c, f)
	return c
}
