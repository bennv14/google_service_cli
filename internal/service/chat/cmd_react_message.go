package chat

import (
	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

func reactCmd(d *service.Deps) *cobra.Command {
	var (
		space  string
		remove bool
	)
	c := &cobra.Command{
		Use:         "react <messageId|URL> <emoji>",
		Short:       "Add or remove an emoji reaction on a Google Chat message",
		Annotations: map[string]string{annotationNoDirectory: "true"},
		Args:        cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := newChatWriter(ctx, d, false, false)
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
