// internal/service/sheets/sheets.go
package sheets

import (
	"context"
	"net/http"
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
func resolveRenderOption(opt string) string {
	switch strings.ToLower(opt) {
	case "unformatted":
		return "UNFORMATTED_VALUE"
	case "formula":
		return "FORMULA"
	default:
		return "FORMATTED_VALUE"
	}
}

// resolveInputOption maps friendly strings to Sheets API ValueInputOption enum values.
func resolveInputOption(opt string) string {
	switch strings.ToLower(opt) {
	case "raw":
		return "RAW"
	default:
		return "USER_ENTERED"
	}
}

// GetValues reads cell values in the specified A1 range.
func (c *Client) GetValues(ctx context.Context, spreadsheetID, readRange, renderOption string) (*ValueRange, error) {
	render := resolveRenderOption(renderOption)
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
	inputOpt := resolveInputOption(inputOption)
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
	inputOpt := resolveInputOption(inputOption)
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
