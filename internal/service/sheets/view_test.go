package sheets

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSpreadsheetMetaTableView(t *testing.T) {
	meta := SpreadsheetMeta{
		ID:    "sheet-123",
		Title: "Q3 Budget",
		URL:   "https://docs.google.com/spreadsheets/d/sheet-123",
	}

	headers := meta.Headers()
	if len(headers) != 3 || headers[0] != "ID" || headers[1] != "TITLE" || headers[2] != "URL" {
		t.Fatalf("unexpected headers: %v", headers)
	}

	rows := meta.Rows()
	if len(rows) != 1 || rows[0][0] != "sheet-123" || rows[0][1] != "Q3 Budget" || rows[0][2] != "https://docs.google.com/spreadsheets/d/sheet-123" {
		t.Fatalf("unexpected rows: %v", rows)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"spreadsheetId":"sheet-123"`) {
		t.Fatalf("json missing spreadsheetId: %s", string(data))
	}

	var buf bytes.Buffer
	if err := meta.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Q3 Budget") || !strings.Contains(buf.String(), "sheet-123") {
		t.Fatalf("unexpected text output: %s", buf.String())
	}
}

func TestSpreadsheetInfoTableViewAndTextView(t *testing.T) {
	info := SpreadsheetInfo{
		ID:       "sheet-456",
		Title:    "Inventory",
		Locale:   "en_US",
		TimeZone: "America/New_York",
		Sheets: []SheetTab{
			{SheetID: 0, Title: "Items", Index: 0, Rows: 100, Cols: 10, Hidden: false},
			{SheetID: 101, Title: "Archive", Index: 1, Rows: 500, Cols: 10, Hidden: true},
		},
	}

	headers := info.Headers()
	if len(headers) != 5 || headers[0] != "SHEET_ID" || headers[1] != "TITLE" {
		t.Fatalf("unexpected headers: %v", headers)
	}

	rows := info.Rows()
	if len(rows) != 2 || rows[0][0] != "0" || rows[0][1] != "Items" || rows[0][3] != "100x10" || rows[0][4] != "false" {
		t.Fatalf("unexpected rows: %v", rows)
	}
	if rows[1][0] != "101" || rows[1][4] != "true" {
		t.Fatalf("unexpected row 1: %v", rows[1])
	}

	var buf bytes.Buffer
	if err := info.Text(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Title: Inventory") || !strings.Contains(out, "Locale: en_US") || !strings.Contains(out, "Items (ID: 0") {
		t.Fatalf("unexpected text rendering: %s", out)
	}
}

func TestValueRangeTableView(t *testing.T) {
	vr := ValueRange{
		SpreadsheetID:  "sheet-789",
		Range:          "Sheet1!A1:C2",
		MajorDimension: "ROWS",
		Values: [][]interface{}{
			{"Header1", "Header2", "Header3"},
			{"val1", 42, true},
		},
	}

	headers := vr.Headers()
	if len(headers) != 3 || headers[0] != "A" || headers[1] != "B" || headers[2] != "C" {
		t.Fatalf("unexpected headers: %v", headers)
	}

	rows := vr.Rows()
	if len(rows) != 2 || rows[0][0] != "Header1" || rows[1][1] != "42" || rows[1][2] != "true" {
		t.Fatalf("unexpected rows: %v", rows)
	}

	// Empty values case
	emptyVR := ValueRange{Range: "Sheet1!A1:B2"}
	if len(emptyVR.Headers()) != 0 || len(emptyVR.Rows()) != 0 {
		t.Fatalf("expected empty headers and rows for empty ValueRange")
	}

	// Text rendering
	var buf bytes.Buffer
	if err := vr.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Header1\tHeader2\tHeader3") || !strings.Contains(buf.String(), "val1\t42\ttrue") {
		t.Fatalf("unexpected ValueRange text output: %s", buf.String())
	}
}

func TestUpdateResultTableView(t *testing.T) {
	ur := UpdateResult{
		SpreadsheetID:  "sheet-101",
		UpdatedRange:   "Sheet1!A1:B5",
		UpdatedRows:    5,
		UpdatedColumns: 2,
		UpdatedCells:   10,
	}

	headers := ur.Headers()
	if len(headers) != 4 || headers[0] != "RANGE" || headers[1] != "UPDATED_ROWS" {
		t.Fatalf("unexpected headers: %v", headers)
	}

	rows := ur.Rows()
	if len(rows) != 1 || rows[0][0] != "Sheet1!A1:B5" || rows[0][1] != "5" || rows[0][2] != "2" || rows[0][3] != "10" {
		t.Fatalf("unexpected rows: %v", rows)
	}

	var buf bytes.Buffer
	if err := ur.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Sheet1!A1:B5") || !strings.Contains(buf.String(), "10 cells") {
		t.Fatalf("unexpected UpdateResult text output: %s", buf.String())
	}
}

func TestTabResultTableView(t *testing.T) {
	tr := TabResult{
		SheetID:     202,
		Title:       "Summary",
		Index:       2,
		RowCount:    1000,
		ColumnCount: 26,
	}

	headers := tr.Headers()
	if len(headers) != 4 || headers[0] != "SHEET_ID" || headers[1] != "TITLE" || headers[2] != "INDEX" || headers[3] != "GRID_SIZE" {
		t.Fatalf("unexpected headers: %v", headers)
	}

	rows := tr.Rows()
	if len(rows) != 1 || rows[0][0] != "202" || rows[0][1] != "Summary" || rows[0][2] != "2" || rows[0][3] != "1000x26" {
		t.Fatalf("unexpected rows: %v", rows)
	}

	var buf bytes.Buffer
	if err := tr.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Summary") || !strings.Contains(buf.String(), "202") {
		t.Fatalf("unexpected TabResult text output: %s", buf.String())
	}
}

func TestSimpleMessageView(t *testing.T) {
	msg := SimpleMessage{Message: "Range cleared successfully"}
	var buf bytes.Buffer
	if err := msg.Text(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Range cleared successfully") {
		t.Fatalf("unexpected text output: %s", buf.String())
	}
	if len(msg.Headers()) != 1 || msg.Headers()[0] != "RESULT" {
		t.Fatalf("unexpected headers: %v", msg.Headers())
	}
	if len(msg.Rows()) != 1 || msg.Rows()[0][0] != "Range cleared successfully" {
		t.Fatalf("unexpected rows: %v", msg.Rows())
	}
}
