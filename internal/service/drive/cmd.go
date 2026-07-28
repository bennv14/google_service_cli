package drive

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/api/option"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

// testClientOpts lets tests point the Drive client at an httptest server.
// It is nil in production.
var testClientOpts []option.ClientOption

type svc struct{}

// New returns the Drive service.
func New() service.Service { return &svc{} }

func (s *svc) Name() string { return "drive" }

func (s *svc) Scopes() []string { return OAuthScopes }

func (s *svc) Command(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "drive", Short: "Interact with Google Drive"}
	cmd.AddCommand(listCmd(d), infoCmd(d), searchCmd(d), aboutCmd(d), uploadCmd(d), downloadCmd(d))
	return cmd
}

// client builds a Drive client for a command using the requested scope.
func client(ctx context.Context, d *service.Deps, scope string) (*Client, error) {
	hc, err := d.NewClient(ctx, scope)
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, hc, testClientOpts...)
}

func listCmd(d *service.Deps) *cobra.Command {
	var folder, query string
	var limit int64
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List files and folders",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeReadonly)
			if err != nil {
				return err
			}
			files, err := cl.List(ctx, folder, query, limit)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(FileList(files))
		},
	}
	c.Flags().StringVar(&folder, "folder", "", "parent folder ID to list within")
	c.Flags().StringVar(&query, "query", "", "filter by name or full text")
	c.Flags().Int64Var(&limit, "limit", 100, "maximum number of results")
	return c
}

func infoCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "info <fileId>",
		Aliases: []string{"stat"},
		Short:   "Show metadata for a file",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeReadonly)
			if err != nil {
				return err
			}
			f, err := cl.Info(ctx, args[0])
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(FileList{f})
		},
	}
}

func searchCmd(d *service.Deps) *cobra.Command {
	var limit int64
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search files by name or full text",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeReadonly)
			if err != nil {
				return err
			}
			files, err := cl.Search(ctx, args[0], limit)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(FileList(files))
		},
	}
	c.Flags().Int64Var(&limit, "limit", 100, "maximum number of results")
	return c
}

func aboutCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "about",
		Short: "Show account and storage quota",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeReadonly)
			if err != nil {
				return err
			}
			a, err := cl.About(ctx)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(a)
		},
	}
}

func uploadCmd(d *service.Deps) *cobra.Command {
	var to, name string
	c := &cobra.Command{
		Use:   "upload <local-path>",
		Short: "Upload a local file to Drive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeFile)
			if err != nil {
				return err
			}
			f, err := cl.Upload(ctx, args[0], to, name)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(FileList{f})
		},
	}
	c.Flags().StringVar(&to, "to", "", "destination folder ID")
	c.Flags().StringVar(&name, "name", "", "name for the uploaded file (default: local filename)")
	return c
}

func downloadCmd(d *service.Deps) *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "download <fileId>",
		Short: "Download a file from Drive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, scopeReadonly)
			if err != nil {
				return err
			}
			path, err := cl.Download(ctx, args[0], out)
			if err != nil {
				return gerr.Friendly(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Downloaded to %s\n", path)
			return nil
		},
	}
	c.Flags().StringVar(&out, "out", "", "output path (default: the file's Drive name)")
	return c
}
