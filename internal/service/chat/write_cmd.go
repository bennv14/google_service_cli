package chat

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

// isInteractiveTTY determines whether delete confirmation can safely prompt on
// stdin/stderr. Tests override it to simulate an interactive terminal.
var isInteractiveTTY = func(in io.Reader, errOut io.Writer) bool {
	return isTerminalReader(in) && isTerminal(errOut)
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func newChatWriter(ctx context.Context, d *service.Deps, withDir bool) (*Writer, error) {
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
		dir, err = newDirectory(ctx, d, hc, false)
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

func sendCmd(d *service.Deps) *cobra.Command {
	var (
		space       string
		thread      string
		replyTo     string
		text        string
		attachments []string
		links       bool
	)
	c := &cobra.Command{
		Use:   "send [text]",
		Short: "Send a Google Chat message or reply",
		Long: `Send a message to a space, DM (by partner email), thread, or in reply to a message.

Message text can come from a positional argument, --text/-m, or piped stdin.
Text is sent verbatim so native Google Chat formatting (*bold*, _italic_,
~strike~, ` + "`code`" + `, <url|label>) works without mangling.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			body, err := resolveTextInput(cmd, cmd.Flags().Changed("text"), text, args)
			if err != nil {
				return err
			}

			w, err := newChatWriter(ctx, d, true)
			if err != nil {
				return err
			}
			sm, err := w.Send(ctx, SendOpts{
				Space:       space,
				Thread:      thread,
				ReplyTo:     replyTo,
				Text:        body,
				Attachments: attachments,
				Progress:    cmd.ErrOrStderr(),
			})
			if err != nil {
				return gerr.Friendly(err)
			}
			sm.Opts = renderOpts(cmd, "", links)
			if err := writer(d).Render(sm); err != nil {
				return err
			}
			reportWarnings(cmd.ErrOrStderr(), sm.Warnings)
			return nil
		},
	}

	c.Flags().StringVar(&space, "space", "", "space resource name (spaces/...), display name, or DM partner's email")
	c.Flags().StringVar(&thread, "thread", "", "thread resource name (spaces/X/threads/Y), short ID, or Chat URL")
	c.Flags().StringVar(&replyTo, "reply-to", "", "message ID, resource name, or Chat URL to reply to")
	c.Flags().StringVarP(&text, "text", "m", "", "message text body")
	c.Flags().StringArrayVarP(&attachments, "attach", "a", nil, "local file path to upload and attach (repeatable)")
	c.Flags().BoolVar(&links, "links", false, "print URLs on their own lines instead of embedding them")
	return c
}

func editCmd(d *service.Deps) *cobra.Command {
	var (
		space string
		text  string
		links bool
	)
	c := &cobra.Command{
		Use:   "edit <messageId|URL> [text]",
		Short: "Edit an existing Google Chat message",
		Long: `Update the text body of an existing Google Chat message while preserving attachments.

New text can come from a second positional argument, --text/-m, or piped stdin.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			body, err := resolveTextInput(cmd, cmd.Flags().Changed("text"), text, args[1:])
			if err != nil {
				return err
			}

			w, err := newChatWriter(ctx, d, true)
			if err != nil {
				return err
			}
			sm, err := w.Edit(ctx, args[0], space, body)
			if err != nil {
				return gerr.Friendly(err)
			}
			sm.Opts = renderOpts(cmd, "", links)
			if err := writer(d).Render(sm); err != nil {
				return err
			}
			reportWarnings(cmd.ErrOrStderr(), sm.Warnings)
			return nil
		},
	}

	c.Flags().StringVar(&space, "space", "", "space ID, display name, or DM email if passing a shortened message ID")
	c.Flags().StringVarP(&text, "text", "m", "", "new message text")
	c.Flags().BoolVar(&links, "links", false, "print URLs on their own lines instead of embedding them")
	return c
}

func deleteCmd(d *service.Deps) *cobra.Command {
	var (
		space string
		yes   bool
	)
	c := &cobra.Command{
		Use:   "delete <messageId|URL>",
		Short: "Delete a Google Chat message",
		Long: `Delete a Google Chat message by ID, URL, or resource name.

Requires interactive confirmation unless --yes/-y is passed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := newChatWriter(ctx, d, false)
			if err != nil {
				return err
			}
			res, err := w.Delete(ctx, args[0], DeleteOpts{
				Space:       space,
				Yes:         yes,
				Interactive: isInteractiveTTY(cmd.InOrStdin(), cmd.ErrOrStderr()),
				In:          cmd.InOrStdin(),
				ErrOut:      cmd.ErrOrStderr(),
			})
			if err != nil {
				return gerr.Friendly(err)
			}
			return writer(d).Render(res)
		},
	}

	c.Flags().StringVar(&space, "space", "", "space ID, display name, or DM email if passing a shortened message ID")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return c
}

func reactCmd(d *service.Deps) *cobra.Command {
	var (
		space  string
		remove bool
	)
	c := &cobra.Command{
		Use:   "react <messageId|URL> <emoji>",
		Short: "Add or remove an emoji reaction on a Google Chat message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := newChatWriter(ctx, d, false)
			if err != nil {
				return err
			}
			res, err := w.React(ctx, args[0], args[1], ReactOpts{
				Space:  space,
				Remove: remove,
			})
			if err != nil {
				return gerr.Friendly(err)
			}
			return writer(d).Render(res)
		},
	}

	c.Flags().StringVar(&space, "space", "", "space ID, display name, or DM email if passing a shortened message ID")
	c.Flags().BoolVar(&remove, "remove", false, "remove your matching reaction instead of adding it")
	return c
}
