package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// New returns the Chat service.
func New() service.Service { return &svc{} }

func (s *svc) Name() string { return "chat" }

func (s *svc) Scopes() []string { return OAuthScopes }

func (s *svc) Command(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Read and write Google Chat messages",
		Long: `Read, send, edit, delete, and react to Google Chat messages from the terminal.

Requires a Google Workspace account: the Chat API does not serve consumer
(@gmail.com) accounts. The Chat API must also be enabled in the GCP project
behind your OAuth client.`,
	}
	cmd.AddCommand(
		spacesCmd(d),
		threadsCmd(d),
		messagesCmd(d),
		unreadCmd(d),
		mentionsCmd(d),
		threadCmd(d),
		messageCmd(d),
		sendCmd(d),
		editCmd(d),
		deleteCmd(d),
		reactCmd(d),
	)
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

// annotationNoDirectory marks chat subcommands that never display person names
// and therefore do not query the People directory or accept --refresh-names.
const annotationNoDirectory = "noDirectory"

// addQueryFlags registers the flags shared by every message-reading command.
func addQueryFlags(c *cobra.Command, f *queryFlags, limitDefault int, sinceDefault string) {
	c.Flags().StringVar(&f.space, "space", "", "space ID, display name, or a DM partner's email")
	c.Flags().StringVar(&f.thread, "thread", "", "thread resource name (spaces/X/threads/Y)")
	c.Flags().StringVar(&f.since, "since", sinceDefault, "lower time bound: 3d, 12h, 30m, 2026-07-25, or RFC3339")
	c.Flags().StringVar(&f.until, "until", "", "upper time bound: same forms as --since")
	c.Flags().BoolVar(&f.unread, "unread", false, "only messages you have not read")
	c.Flags().BoolVar(&f.mentionsMe, "mention-me", false, "only messages that mention you")
	c.Flags().StringVar(&f.group, "group", "", "grouping: space | thread | flat (default: adapt to each space)")
	addLinksFlag(c, &f.links)
	c.Flags().IntVar(&f.limit, "limit", limitDefault, "maximum number of messages in total (0 = no limit)")
	addRefreshNamesFlag(c, f)
}

func addLinksFlag(c *cobra.Command, target *bool) {
	c.Flags().BoolVar(target, "links", false, "print URLs on their own lines instead of embedding them")
}

func addRefreshNamesBoolFlag(c *cobra.Command, target *bool) {
	c.Flags().BoolVar(target, "refresh-names", false,
		"ignore cached display names and look them up again")
}

// addRefreshNamesFlag registers --refresh-names. Every chat subcommand that
// prints person names can be told to re-fetch them.
func addRefreshNamesFlag(c *cobra.Command, f *queryFlags) {
	addRefreshNamesBoolFlag(c, &f.refreshNames)
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

// newChatWriter builds a Writer for message write commands, optionally wiring
// the People directory so returned messages resolve sender display names.
func newChatWriter(ctx context.Context, d *service.Deps, withDir, refreshNames bool) (*Writer, error) {
	hc, err := d.NewClient(ctx, OAuthScopes...)
	if err != nil {
		return nil, err
	}
	cl, err := NewClient(ctx, hc, testClientOpts...)
	if err != nil {
		return nil, err
	}
	var dir Directory
	if withDir {
		dir, err = newDirectory(ctx, d, hc, refreshNames)
		if err != nil {
			return nil, err
		}
	}
	return NewWriter(cl, dir), nil
}

// resolveTextInput resolves message text from at most one explicit source
// (--text flag or positional arg), falling back to non-interactive stdin.
func resolveTextInput(cmd *cobra.Command, hasTextFlag bool, textFlag string, posArgs []string) (string, error) {
	hasArg := len(posArgs) > 0
	if hasTextFlag && hasArg {
		return "", errors.New("cannot specify message text via both --text and positional argument")
	}
	if hasTextFlag {
		return textFlag, nil
	}
	if hasArg {
		return posArgs[0], nil
	}
	in := cmd.InOrStdin()
	if in != nil && !isTerminalReader(in) {
		b, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", nil
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

// showsThreadTitles reports whether the chosen output has a thread header to put
// a title in. A title costs one request per thread whose opening message the
// scan missed, so an output with no thread level should not pay for it — the
// same reasoning as `chat spaces --unread`, which skips them in Engine.Spaces.
//
// JSON always carries thread.title, whatever the grouping. The table flattens to
// one row per message and shows the short thread ID. The text renderer heads
// each thread with its label unless --group flat put every message on a single
// timeline, which also shows only the ID.
func showsThreadTitles(d *service.Deps, f *queryFlags) bool {
	if d.OutputExplicit {
		switch d.OutputFormat {
		case "json":
			return true
		case "table":
			return false
		}
	}
	return f.group != "flat"
}

func renderOpts(cmd *cobra.Command, group string, showLinks bool) RenderOpts {
	tty := isTerminal(cmd.OutOrStdout())
	return RenderOpts{Group: group, Color: tty, Hyperlinks: tty, ShowLinks: showLinks}
}

// isTerminal reports whether w is an interactive terminal; colour and OSC 8
// hyperlinks are suppressed everywhere else so pipes stay clean.
func isTerminal(w io.Writer) bool {
	return isCharDevice(w)
}

func isTerminalReader(r io.Reader) bool {
	return isCharDevice(r)
}

func isCharDevice(v any) bool {
	f, ok := v.(*os.File)
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

