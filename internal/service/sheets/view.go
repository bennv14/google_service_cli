package sheets

import (
	"fmt"
	"io"
	"strconv"

	"github.com/bennv14/google_service_cli/internal/output"
)

var (
	_ output.TableView = SpreadsheetMeta{}
	_ output.TableView = SpreadsheetInfo{}
	_ output.TextView  = SpreadsheetInfo{}
	_ output.TableView = ValueRange{}
	_ output.TableView = UpdateResult{}
	_ output.TableView = TabResult{}
	_ output.TableView = SimpleMessage{}
	_ output.TextView  = SimpleMessage{}
)

// SpreadsheetMeta contains minimal metadata for a newly created spreadsheet.
type SpreadsheetMeta struct {
	ID    string `json:"spreadsheetId"`
	Title string `json:"title"`
	URL   string `json:"spreadsheetUrl"`
}

func (m SpreadsheetMeta) Headers() []string {
	return []string{"ID", "TITLE", "URL"}
}

func (m SpreadsheetMeta) Rows() [][]string {
	return [][]string{{m.ID, m.Title, m.URL}}
}

// SheetTab represents a worksheet within a spreadsheet.
type SheetTab struct {
	SheetID int64  `json:"sheetId"`
	Title   string `json:"title"`
	Index   int64  `json:"index"`
	Rows    int64  `json:"rowCount"`
	Cols    int64  `json:"columnCount"`
	Hidden  bool   `json:"hidden"`
}

// SpreadsheetInfo represents detailed spreadsheet metadata and tabs.
type SpreadsheetInfo struct {
	ID       string     `json:"spreadsheetId"`
	Title    string     `json:"title"`
	Locale   string     `json:"locale"`
	TimeZone string     `json:"timeZone"`
	Sheets   []SheetTab `json:"sheets"`
}

func (i SpreadsheetInfo) Headers() []string {
	return []string{"SHEET_ID", "TITLE", "INDEX", "GRID_SIZE", "HIDDEN"}
}

func (i SpreadsheetInfo) Rows() [][]string {
	rows := make([][]string, 0, len(i.Sheets))
	for _, s := range i.Sheets {
		grid := fmt.Sprintf("%dx%d", s.Rows, s.Cols)
		rows = append(rows, []string{
			strconv.FormatInt(s.SheetID, 10),
			s.Title,
			strconv.FormatInt(s.Index, 10),
			grid,
			strconv.FormatBool(s.Hidden),
		})
	}
	return rows
}

func (i SpreadsheetInfo) Text(w io.Writer) error {
	fmt.Fprintf(w, "Title: %s (%s)\n", i.Title, i.ID)
	fmt.Fprintf(w, "Locale: %s | TimeZone: %s\n", i.Locale, i.TimeZone)
	fmt.Fprintf(w, "Tabs (%d):\n", len(i.Sheets))
	for _, s := range i.Sheets {
		hiddenStr := ""
		if s.Hidden {
			hiddenStr = " [hidden]"
		}
		fmt.Fprintf(w, "  - %s (ID: %d, Index: %d, Grid: %dx%d)%s\n", s.Title, s.SheetID, s.Index, s.Rows, s.Cols, hiddenStr)
	}
	return nil
}

// ValueRange represents a 2D matrix of cell values.
type ValueRange struct {
	SpreadsheetID  string          `json:"spreadsheetId,omitempty"`
	Range          string          `json:"range"`
	MajorDimension string          `json:"majorDimension,omitempty"`
	Values         [][]interface{} `json:"values"`
}

func (v ValueRange) Headers() []string {
	if len(v.Values) == 0 {
		return nil
	}
	maxCols := 0
	for _, row := range v.Values {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	headers := make([]string, maxCols)
	for i := 0; i < maxCols; i++ {
		headers[i] = columnIndexToA1(i)
	}
	return headers
}

func (v ValueRange) Rows() [][]string {
	if len(v.Values) == 0 {
		return nil
	}
	maxCols := len(v.Headers())
	rows := make([][]string, 0, len(v.Values))
	for _, row := range v.Values {
		r := make([]string, maxCols)
		for c := 0; c < maxCols; c++ {
			if c < len(row) && row[c] != nil {
				r[c] = fmt.Sprintf("%v", row[c])
			} else {
				r[c] = ""
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// columnIndexToA1 converts zero-based column index (0 -> "A", 25 -> "Z", 26 -> "AA").
func columnIndexToA1(col int) string {
	result := ""
	col++
	for col > 0 {
		rem := (col - 1) % 26
		result = string(rune('A'+rem)) + result
		col = (col - 1) / 26
	}
	return result
}

// UpdateResult captures result metrics after an append or update operation.
type UpdateResult struct {
	SpreadsheetID  string `json:"spreadsheetId"`
	UpdatedRange   string `json:"updatedRange"`
	UpdatedRows    int64  `json:"updatedRows"`
	UpdatedColumns int64  `json:"updatedColumns"`
	UpdatedCells   int64  `json:"updatedCells"`
}

func (u UpdateResult) Headers() []string {
	return []string{"RANGE", "UPDATED_ROWS", "UPDATED_COLUMNS", "UPDATED_CELLS"}
}

func (u UpdateResult) Rows() [][]string {
	return [][]string{{
		u.UpdatedRange,
		strconv.FormatInt(u.UpdatedRows, 10),
		strconv.FormatInt(u.UpdatedColumns, 10),
		strconv.FormatInt(u.UpdatedCells, 10),
	}}
}

// TabResult captures the result of adding a tab.
type TabResult struct {
	SheetID     int64  `json:"sheetId"`
	Title       string `json:"title"`
	Index       int64  `json:"index"`
	RowCount    int64  `json:"rowCount"`
	ColumnCount int64  `json:"columnCount"`
}

func (t TabResult) Headers() []string {
	return []string{"SHEET_ID", "TITLE", "INDEX", "GRID_SIZE"}
}

func (t TabResult) Rows() [][]string {
	return [][]string{{
		strconv.FormatInt(t.SheetID, 10),
		t.Title,
		strconv.FormatInt(t.Index, 10),
		fmt.Sprintf("%dx%d", t.RowCount, t.ColumnCount),
	}}
}

// SimpleMessage renders a simple string result in text or table form.
type SimpleMessage struct {
	Message string `json:"message"`
}

func (s SimpleMessage) Headers() []string {
	return []string{"RESULT"}
}

func (s SimpleMessage) Rows() [][]string {
	return [][]string{{s.Message}}
}

func (s SimpleMessage) Text(w io.Writer) error {
	_, err := fmt.Fprintln(w, s.Message)
	return err
}
