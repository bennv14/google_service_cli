// internal/service/sheets/sheets.go
package sheets

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"google.golang.org/api/option"
	sheetsapi "google.golang.org/api/sheets/v4"
)

const (
	// ScopeSpreadsheets gives read/write access to spreadsheets.
	ScopeSpreadsheets = "https://www.googleapis.com/auth/spreadsheets"
	// ScopeReadonly gives read-only access to spreadsheets.
	ScopeReadonly = "https://www.googleapis.com/auth/spreadsheets.readonly"
)

// OAuthScopes is the list of scopes required for Google Sheets.
var OAuthScopes = []string{ScopeSpreadsheets, ScopeReadonly}

// Client wraps the Google Sheets v4 API service.
type Client struct {
	svc *sheetsapi.Service
}

// NewClient creates a new Google Sheets API Client wrapper.
func NewClient(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	all := append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := sheetsapi.NewService(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

// Create creates a new Google Spreadsheet with an optional custom default tab title.
func (c *Client) Create(ctx context.Context, title, defaultTab string) (*SpreadsheetMeta, error) {
	req := &sheetsapi.Spreadsheet{
		Properties: &sheetsapi.SpreadsheetProperties{
			Title: title,
		},
	}
	if defaultTab != "" {
		req.Sheets = []*sheetsapi.Sheet{
			{
				Properties: &sheetsapi.SheetProperties{
					Title: defaultTab,
				},
			},
		}
	}

	res, err := c.svc.Spreadsheets.Create(req).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	resTitle := title
	if res.Properties != nil && res.Properties.Title != "" {
		resTitle = res.Properties.Title
	}
	return &SpreadsheetMeta{
		ID:    res.SpreadsheetId,
		Title: resTitle,
		URL:   res.SpreadsheetUrl,
	}, nil
}

// GetMetadata fetches the spreadsheet properties and sheet tab metadata.
func (c *Client) GetMetadata(ctx context.Context, spreadsheetID string) (*SpreadsheetInfo, error) {
	res, err := c.svc.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	info := &SpreadsheetInfo{
		ID: res.SpreadsheetId,
	}
	if res.Properties != nil {
		info.Title = res.Properties.Title
		info.Locale = res.Properties.Locale
		info.TimeZone = res.Properties.TimeZone
	}

	for _, sheet := range res.Sheets {
		if sheet.Properties == nil {
			continue
		}
		p := sheet.Properties
		var rows, cols int64
		if p.GridProperties != nil {
			rows = p.GridProperties.RowCount
			cols = p.GridProperties.ColumnCount
		}
		info.Sheets = append(info.Sheets, SheetTab{
			SheetID: p.SheetId,
			Title:   p.Title,
			Index:   p.Index,
			Rows:    rows,
			Cols:    cols,
			Hidden:  p.Hidden,
		})
	}
	return info, nil
}

// resolveRenderOption maps friendly strings to Sheets API ValueRenderOption enum values.
func resolveRenderOption(opt string) (string, error) {
	switch strings.ToLower(opt) {
	case "", "formatted":
		return "FORMATTED_VALUE", nil
	case "unformatted":
		return "UNFORMATTED_VALUE", nil
	case "formula":
		return "FORMULA", nil
	default:
		return "", fmt.Errorf("invalid --render %q: use formatted, unformatted, or formula", opt)
	}
}

// resolveInputOption maps friendly strings to Sheets API ValueInputOption enum values.
func resolveInputOption(opt string) (string, error) {
	switch strings.ToLower(opt) {
	case "", "user_entered":
		return "USER_ENTERED", nil
	case "raw":
		return "RAW", nil
	default:
		return "", fmt.Errorf("invalid --input-type %q: use user_entered or raw", opt)
	}
}

// GetValues reads cell values in the specified A1 range.
func (c *Client) GetValues(ctx context.Context, spreadsheetID, readRange, renderOption string) (*ValueRange, error) {
	render, err := resolveRenderOption(renderOption)
	if err != nil {
		return nil, err
	}
	call := c.svc.Spreadsheets.Values.Get(spreadsheetID, readRange).
		ValueRenderOption(render).
		Context(ctx)

	res, err := call.Do()
	if err != nil {
		return nil, err
	}

	return &ValueRange{
		SpreadsheetID:  spreadsheetID,
		Range:          res.Range,
		MajorDimension: res.MajorDimension,
		Values:         res.Values,
	}, nil
}

// AppendValues appends rows of data after existing data in the specified range.
func (c *Client) AppendValues(ctx context.Context, spreadsheetID, writeRange string, values [][]interface{}, inputOption string) (*UpdateResult, error) {
	inputOpt, err := resolveInputOption(inputOption)
	if err != nil {
		return nil, err
	}
	vr := &sheetsapi.ValueRange{
		Values: values,
	}

	call := c.svc.Spreadsheets.Values.Append(spreadsheetID, writeRange, vr).
		ValueInputOption(inputOpt).
		Context(ctx)

	res, err := call.Do()
	if err != nil {
		return nil, err
	}

	result := &UpdateResult{
		SpreadsheetID: spreadsheetID,
	}
	if res.Updates != nil {
		result.UpdatedRange = res.Updates.UpdatedRange
		result.UpdatedRows = res.Updates.UpdatedRows
		result.UpdatedColumns = res.Updates.UpdatedColumns
		result.UpdatedCells = res.Updates.UpdatedCells
	}
	return result, nil
}

// UpdateValues writes or overwrites values in a specified cell range.
func (c *Client) UpdateValues(ctx context.Context, spreadsheetID, writeRange string, values [][]interface{}, inputOption string) (*UpdateResult, error) {
	inputOpt, err := resolveInputOption(inputOption)
	if err != nil {
		return nil, err
	}
	vr := &sheetsapi.ValueRange{
		Values: values,
	}

	call := c.svc.Spreadsheets.Values.Update(spreadsheetID, writeRange, vr).
		ValueInputOption(inputOpt).
		Context(ctx)

	res, err := call.Do()
	if err != nil {
		return nil, err
	}

	return &UpdateResult{
		SpreadsheetID:  spreadsheetID,
		UpdatedRange:   res.UpdatedRange,
		UpdatedRows:    res.UpdatedRows,
		UpdatedColumns: res.UpdatedColumns,
		UpdatedCells:   res.UpdatedCells,
	}, nil
}

// ClearValues clears all values within the specified cell range.
func (c *Client) ClearValues(ctx context.Context, spreadsheetID, clearRange string) error {
	req := &sheetsapi.ClearValuesRequest{}
	_, err := c.svc.Spreadsheets.Values.Clear(spreadsheetID, clearRange, req).Context(ctx).Do()
	return err
}

// AddTab adds a new worksheet tab to a spreadsheet.
func (c *Client) AddTab(ctx context.Context, spreadsheetID, title string, index, rows, cols int64) (*TabResult, error) {
	if rows <= 0 {
		rows = 1000
	}
	if cols <= 0 {
		cols = 26
	}

	sheetProps := &sheetsapi.SheetProperties{
		Title: title,
		GridProperties: &sheetsapi.GridProperties{
			RowCount:    rows,
			ColumnCount: cols,
		},
	}
	if index >= 0 {
		sheetProps.Index = index
		sheetProps.ForceSendFields = []string{"Index"}
	}

	req := &sheetsapi.BatchUpdateSpreadsheetRequest{
		Requests: []*sheetsapi.Request{
			{
				AddSheet: &sheetsapi.AddSheetRequest{
					Properties: sheetProps,
				},
			},
		},
	}

	res, err := c.svc.Spreadsheets.BatchUpdate(spreadsheetID, req).Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	result := &TabResult{
		Title:       title,
		Index:       index,
		RowCount:    rows,
		ColumnCount: cols,
	}
	if len(res.Replies) > 0 && res.Replies[0].AddSheet != nil && res.Replies[0].AddSheet.Properties != nil {
		p := res.Replies[0].AddSheet.Properties
		result.SheetID = p.SheetId
		result.Title = p.Title
		result.Index = p.Index
		if p.GridProperties != nil {
			result.RowCount = p.GridProperties.RowCount
			result.ColumnCount = p.GridProperties.ColumnCount
		}
	}
	return result, nil
}

// DeleteTab deletes a tab by its integer sheetId.
func (c *Client) DeleteTab(ctx context.Context, spreadsheetID string, sheetID int64) error {
	req := &sheetsapi.BatchUpdateSpreadsheetRequest{
		Requests: []*sheetsapi.Request{
			{
				DeleteSheet: &sheetsapi.DeleteSheetRequest{
					SheetId: sheetID,
				},
			},
		},
	}

	_, err := c.svc.Spreadsheets.BatchUpdate(spreadsheetID, req).Context(ctx).Do()
	return err
}

// ExportCSV exports an entire sheet tab to CSV format, writing to outPath or outWriter.
func (c *Client) ExportCSV(ctx context.Context, spreadsheetID, sheetName, outPath string, outWriter io.Writer) error {
	if outPath == "" && outWriter == nil {
		return errors.New("outPath or outWriter must be provided")
	}

	targetSheet := sheetName
	if targetSheet == "" {
		meta, err := c.GetMetadata(ctx, spreadsheetID)
		if err != nil {
			return err
		}
		if len(meta.Sheets) == 0 {
			return fmt.Errorf("spreadsheet %q has no sheets to export", spreadsheetID)
		}
		targetSheet = meta.Sheets[0].Title
	}

	// Always quote the sheet title so names containing '!' or spaces remain valid A1 notation.
	readRange := fmt.Sprintf("'%s'", strings.ReplaceAll(targetSheet, "'", "''"))
	vr, err := c.GetValues(ctx, spreadsheetID, readRange, "formatted")
	if err != nil {
		return err
	}

	var w io.Writer = outWriter
	var targetFile *os.File
	if outPath != "" {
		cleaned := CleanPath(outPath)
		f, err := os.Create(cleaned)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		targetFile = f
		w = f
		defer func() {
			if targetFile != nil {
				_ = targetFile.Close()
			}
		}()
	}

	csvWriter := csv.NewWriter(w)
	for _, row := range vr.Values {
		strRow := make([]string, len(row))
		for i, cell := range row {
			if cell != nil {
				strRow[i] = fmt.Sprintf("%v", cell)
			}
		}
		if err := csvWriter.Write(strRow); err != nil {
			if targetFile != nil {
				_ = targetFile.Close()
				targetFile = nil
				_ = os.Remove(CleanPath(outPath))
			}
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		if targetFile != nil {
			_ = targetFile.Close()
			targetFile = nil
			_ = os.Remove(CleanPath(outPath))
		}
		return fmt.Errorf("failed to flush CSV writer: %w", err)
	}
	if targetFile != nil {
		if err := targetFile.Close(); err != nil {
			targetFile = nil
			_ = os.Remove(CleanPath(outPath))
			return fmt.Errorf("failed to close output file: %w", err)
		}
		targetFile = nil
	}

	return nil
}
