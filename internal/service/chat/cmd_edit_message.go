package chat

import (
	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

func editCmd(d *service.Deps) *cobra.Command {
	var (
		space        string
		text         string
		links        bool
		refreshNames bool
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

			w, err := newChatWriter(ctx, d, true, refreshNames)
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
	addLinksFlag(c, &links)
	addRefreshNamesBoolFlag(c, &refreshNames)
	return c
}
