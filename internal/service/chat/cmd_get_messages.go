package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
	"github.com/bennv14/google_service_cli/internal/service/drive"
)

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
	q.skipThreadTitles = !showsThreadTitles(d, f)
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
	addLinksFlag(c, &f.links)
	addRefreshNamesFlag(c, f)
	return c
}

func messageCmd(d *service.Deps) *cobra.Command {
	var (
		download     bool
		outputDir    string
		outFile      string
		space        string
		links        bool
		refreshNames bool
	)
	c := &cobra.Command{
		Use:   "message <messageId>",
		Short: "Read a single message and optionally download its attachments",
		Long: `Read a single message by ID, URL, or resource name.

Supported ID formats:
  - 3-segment ID: AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY
  - Chat web URL: https://chat.google.com/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY
  - Resource name: spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY
  - Short ID with --space: t-uT1uhCWAg.emH4eHFJkeY --space AAAA9GOspFY`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name, err := qualifyMessage(args[0], space)
			if err != nil {
				return err
			}

			flags := &queryFlags{refreshNames: refreshNames}
			e, err := engine(ctx, d, flags)
			if err != nil {
				return err
			}

			sm, err := e.Message(ctx, name, accountIndex(d))
			if err != nil {
				return gerr.Friendly(err)
			}
			sm.Opts = renderOpts(cmd, "", links)

			if err := writer(d).Render(sm); err != nil {
				return err
			}

			reportWarnings(cmd.ErrOrStderr(), sm.Warnings)

			if download {
				if len(sm.Message.Attachments) == 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "No attachments found in message.")
					return nil
				}

				hc, err := d.NewClient(ctx, OAuthScopes...)
				if err != nil {
					return err
				}
				chatCl, err := NewClient(ctx, hc, testClientOpts...)
				if err != nil {
					return err
				}

				var driveCl *drive.Client
				hasDriveFile := false
				for _, att := range sm.Message.Attachments {
					if att.Source == "DRIVE_FILE" || att.DriveFileID != "" {
						hasDriveFile = true
						break
					}
				}
				if hasDriveFile {
					hcDrive, err := d.NewClient(ctx, drive.OAuthScopes...)
					if err != nil {
						return err
					}
					driveCl, err = drive.NewClient(ctx, hcDrive, testClientOpts...)
					if err != nil {
						return err
					}
				}

				strategies := []DownloadStrategy{
					NewChatMediaStrategy(chatCl),
				}
				if driveCl != nil {
					strategies = append(strategies, NewDriveFileStrategy(driveCl))
				}

				dl := NewDownloader(strategies...)
				results, err := dl.DownloadAll(ctx, sm.Message.Attachments, outputDir, outFile)
				if err != nil {
					return err
				}

				outW := cmd.OutOrStdout()
				if d.OutputFormat == "json" {
					outW = cmd.ErrOrStderr()
				}
				for _, r := range results {
					if r.Err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to download %s: %v\n", r.Attachment.ContentName, r.Err)
					} else {
						fmt.Fprintf(outW, "Downloaded %s -> %s\n", r.Attachment.ContentName, r.LocalPath)
					}
				}
			}

			return nil
		},
	}

	c.Flags().BoolVar(&download, "download", false, "download all assets attached to this message")
	c.Flags().StringVar(&outputDir, "output-dir", ".", "directory to save downloaded assets")
	c.Flags().StringVar(&outFile, "out", "", "custom output filename for a single asset")
	c.Flags().StringVar(&space, "space", "", "space ID if passing a shortened message ID")
	addLinksFlag(c, &links)
	addRefreshNamesBoolFlag(c, &refreshNames)

	return c
}
