// Package output renders command results as a human table, JSON, or free-form text.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// TableView is implemented by result types that can render as a table.
type TableView interface {
	Headers() []string
	Rows() [][]string
}

// TextView is implemented by result types that can render themselves as
// free-form text (used by the "text" format).
type TextView interface {
	Text(w io.Writer) error
}

// Writer renders arbitrary data in the configured format.
type Writer interface {
	Render(data any) error
}

type writer struct {
	format string
	w      io.Writer
}

// NewWriter returns a Writer. format is "table" (default), "json", or "text".
func NewWriter(format string, w io.Writer) Writer {
	if format == "" {
		format = "table"
	}
	return &writer{format: format, w: w}
}

func (o *writer) Render(data any) error {
	switch o.format {
	case "json":
		enc := json.NewEncoder(o.w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "table":
		tv, ok := data.(TableView)
		if !ok {
			return fmt.Errorf("value of type %T cannot be rendered as a table (use --output json)", data)
		}
		tw := tabwriter.NewWriter(o.w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, strings.Join(tv.Headers(), "\t"))
		for _, r := range tv.Rows() {
			fmt.Fprintln(tw, strings.Join(r, "\t"))
		}
		return tw.Flush()
	case "text":
		tv, ok := data.(TextView)
		if !ok {
			return fmt.Errorf("value of type %T cannot be rendered as text (use --output json)", data)
		}
		return tv.Text(o.w)
	default:
		return fmt.Errorf("unknown output format %q", o.format)
	}
}
