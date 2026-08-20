// internal/service/sheets/cmd.go
package sheets

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"google.golang.org/api/option"

	"github.com/bennv14/google_service_cli/internal/gerr"
	"github.com/bennv14/google_service_cli/internal/service"
)

// testClientOpts lets unit tests override endpoint and authentication.
var testClientOpts []option.ClientOption

type svc struct{}

// New returns the Google Sheets service implementation.
func New() service.Service {
	return &svc{}
}

func (s *svc) Name() string {
	return "sheets"
}

func (s *svc) Scopes() []string {
	return OAuthScopes
}

func (s *svc) Command(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sheets",
		Aliases: []string{"sheet"},
		Short:   "Interact with Google Sheets",
	}

	cmd.AddCommand(
		createCmd(d),
		infoCmd(d),
		getCmd(d),
		appendCmd(d),
		updateCmd(d),
		clearCmd(d),
		addTabCmd(d),
		deleteTabCmd(d),
		exportCmd(d),
	)

	return cmd
}

func client(ctx context.Context, d *service.Deps, scope string) (*Client, error) {
	hc, err := d.NewClient(ctx, scope)
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, hc, testClientOpts...)
}

func createCmd(d *service.Deps) *cobra.Command {
	var tabTitle string
	c := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new Google Spreadsheet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			meta, err := cl.Create(ctx, args[0], tabTitle)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(meta)
		},
	}
	c.Flags().StringVar(&tabTitle, "tab", "", "title for the default initial sheet")
	return c
}

func infoCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "info <spreadsheetId>",
		Aliases: []string{"stat"},
		Short:   "Show metadata and worksheet tabs for a spreadsheet",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeReadonly)
			if err != nil {
				return err
			}
			info, err := cl.GetMetadata(ctx, args[0])
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(info)
		},
	}
}

func getCmd(d *service.Deps) *cobra.Command {
	var render string
	c := &cobra.Command{
		Use:     "get <spreadsheetId> <range>",
		Aliases: []string{"read"},
		Short:   "Retrieve cell values within a specified range",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeReadonly)
			if err != nil {
				return err
			}
			vr, err := cl.GetValues(ctx, args[0], args[1], render)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(vr)
		},
	}
	c.Flags().StringVar(&render, "render", "formatted", "value rendering: formatted | unformatted | formula")
	return c
}

func appendCmd(d *service.Deps) *cobra.Command {
	var (
		filePath  string
		inputType string
	)
	c := &cobra.Command{
		Use:   "append <spreadsheetId> <range> [values...]",
		Short: "Append rows of data after existing content in a sheet",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			vals, err := ParseValues(filePath, args[2:], cmd.InOrStdin())
			if err != nil {
				return err
			}
			res, err := cl.AppendValues(ctx, args[0], args[1], vals, inputType)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(res)
		},
	}
	c.Flags().StringVarP(&filePath, "file", "f", "", "read rows from a local CSV file")
	c.Flags().StringVar(&inputType, "input-type", "user_entered", "input value type: user_entered | raw")
	return c
}

func updateCmd(d *service.Deps) *cobra.Command {
	var (
		filePath  string
		inputType string
	)
	c := &cobra.Command{
		Use:     "update <spreadsheetId> <range> [values...]",
		Aliases: []string{"write"},
		Short:   "Write or overwrite cell values in a specified range",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			vals, err := ParseValues(filePath, args[2:], cmd.InOrStdin())
			if err != nil {
				return err
			}
			res, err := cl.UpdateValues(ctx, args[0], args[1], vals, inputType)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(res)
		},
	}
	c.Flags().StringVarP(&filePath, "file", "f", "", "read rows from a local CSV file")
	c.Flags().StringVar(&inputType, "input-type", "user_entered", "input value type: user_entered | raw")
	return c
}

func clearCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "clear <spreadsheetId> <range>",
		Short: "Clear all cell values within a specified range",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			if err := cl.ClearValues(ctx, args[0], args[1]); err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(SimpleMessage{Message: fmt.Sprintf("Cleared range %s", args[1])})
		},
	}
}

func addTabCmd(d *service.Deps) *cobra.Command {
	var (
		index int64
		rows  int64
		cols  int64
	)
	c := &cobra.Command{
		Use:   "add-tab <spreadsheetId> <title>",
		Short: "Add a new worksheet tab to a spreadsheet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			res, err := cl.AddTab(ctx, args[0], args[1], index, rows, cols)
			if err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(res)
		},
	}
	c.Flags().Int64Var(&index, "index", -1, "zero-based tab index position")
	c.Flags().Int64Var(&rows, "rows", 1000, "initial row count")
	c.Flags().Int64Var(&cols, "cols", 26, "initial column count")
	return c
}

func deleteTabCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete-tab <spreadsheetId> <sheetId>",
		Short: "Delete a worksheet tab by its integer sheetId",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sheetID, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid sheetId %q: must be an integer", args[1])
			}
			cl, err := client(ctx, d, ScopeSpreadsheets)
			if err != nil {
				return err
			}
			if err := cl.DeleteTab(ctx, args[0], sheetID); err != nil {
				return gerr.Friendly(err)
			}
			return d.Out.Render(SimpleMessage{Message: fmt.Sprintf("Deleted tab %d", sheetID)})
		},
	}
}

func exportCmd(d *service.Deps) *cobra.Command {
	var (
		sheetName string
		outPath   string
	)
	c := &cobra.Command{
		Use:   "export <spreadsheetId>",
		Short: "Export a worksheet tab as CSV",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := client(ctx, d, ScopeReadonly)
			if err != nil {
				return err
			}
			var w io.Writer = cmd.OutOrStdout()
			if outPath != "" {
				w = nil
			}
			if err := cl.ExportCSV(ctx, args[0], sheetName, outPath, w); err != nil {
				return gerr.Friendly(err)
			}
			if outPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s\n", CleanPath(outPath))
			}
			return nil
		},
	}
	c.Flags().StringVar(&sheetName, "sheet", "", "specific tab title to export (default: first tab)")
	c.Flags().StringVar(&outPath, "out", "", "target output file path (default: stdout)")
	return c
}
