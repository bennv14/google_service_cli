package chat

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

// isInteractiveTTY determines whether delete confirmation can safely prompt on
// stdin/stderr. Tests override it to simulate an interactive terminal.
var isInteractiveTTY = func(in io.Reader, errOut io.Writer) bool {
	return isTerminalReader(in) && isTerminal(errOut)
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
		Annotations: map[string]string{annotationNoDirectory: "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := newChatWriter(ctx, d, false, false)
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
