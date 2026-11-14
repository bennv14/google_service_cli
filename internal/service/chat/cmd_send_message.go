package chat

import (
	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

func sendCmd(d *service.Deps) *cobra.Command {
	var (
		space        string
		thread       string
		replyTo      string
		text         string
		attachments  []string
		links        bool
		refreshNames bool
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

			w, err := newChatWriter(ctx, d, true, refreshNames)
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
	addLinksFlag(c, &links)
	addRefreshNamesBoolFlag(c, &refreshNames)
	return c
}
