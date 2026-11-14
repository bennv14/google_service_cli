// internal/service/sheets/sheets_test.go
package sheets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/option"
)

func TestOAuthScopes(t *testing.T) {
	if len(OAuthScopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(OAuthScopes))
	}
	if OAuthScopes[0] != ScopeSpreadsheets || OAuthScopes[1] != ScopeReadonly {
		t.Fatalf("unexpected scopes: %v", OAuthScopes)
	}
}

func TestResolveOptions(t *testing.T) {
	// Test render option resolution
	renderCases := []struct {
		in   string
		want string
	}{
		{"", "FORMATTED_VALUE"},
		{"formatted", "FORMATTED_VALUE"},
		{"unformatted", "UNFORMATTED_VALUE"},
		{"FORMULA", "FORMULA"},
	}
	for _, tc := range renderCases {
		got, err := resolveRenderOption(tc.in)
		if err != nil {
			t.Fatalf("resolveRenderOption(%q) unexpected err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("resolveRenderOption(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if _, err := resolveRenderOption("other"); err == nil {
		t.Errorf("expected error for invalid render option")
	}

	// Test input option resolution
	inputCases := []struct {
		in   string
		want string
	}{
		{"", "USER_ENTERED"},
		{"user_entered", "USER_ENTERED"},
		{"raw", "RAW"},
		{"RAW", "RAW"},
	}
	for _, tc := range inputCases {
		got, err := resolveInputOption(tc.in)
		if err != nil {
			t.Fatalf("resolveInputOption(%q) unexpected err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("resolveInputOption(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if _, err := resolveInputOption("anything_else"); err == nil {
		t.Errorf("expected error for invalid input option")
	}
}

func TestClientCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v4/spreadsheets" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		props, ok := req["properties"].(map[string]any)
		if !ok || props["title"] != "New Budget" {
			http.Error(w, "invalid title", http.StatusBadRequest)
			return
		}
		sheetsList, ok := req["sheets"].([]any)
		if !ok || len(sheetsList) != 1 {
			http.Error(w, "expected 1 sheet", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId":  "created-id-123",
			"spreadsheetUrl": "https://docs.google.com/spreadsheets/d/created-id-123",
			"properties": map[string]any{
				"title": "New Budget",
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	meta, err := cl.Create(ctx, "New Budget", "Sheet1")
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if meta.ID != "created-id-123" || meta.Title != "New Budget" || meta.URL != "https://docs.google.com/spreadsheets/d/created-id-123" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestClientCreate_WithoutDefaultTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v4/spreadsheets" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, exists := req["sheets"]; exists {
			http.Error(w, "sheets should not be present", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId":  "created-id-456",
			"spreadsheetUrl": "https://docs.google.com/spreadsheets/d/created-id-456",
			"properties": map[string]any{
				"title": "Default Sheet",
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	meta, err := cl.Create(ctx, "Default Sheet", "")
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if meta.ID != "created-id-456" || meta.Title != "Default Sheet" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestClientGetMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v4/spreadsheets/sheet-abc" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId": "sheet-abc",
			"properties": map[string]any{
				"title":    "Company Metrics",
				"locale":   "en_US",
				"timeZone": "UTC",
			},
			"sheets": []map[string]any{
				{
					"properties": map[string]any{
						"sheetId": 0,
						"title":   "Overview",
						"index":   0,
						"hidden":  false,
						"gridProperties": map[string]any{
							"rowCount":    50,
							"columnCount": 10,
						},
					},
				},
				{
					// Sheet without properties or grid properties edge cases
				},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	info, err := cl.GetMetadata(ctx, "sheet-abc")
	if err != nil {
		t.Fatalf("GetMetadata() failed: %v", err)
	}
	if info.ID != "sheet-abc" || info.Title != "Company Metrics" || info.Locale != "en_US" || info.TimeZone != "UTC" || len(info.Sheets) != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.Sheets[0].Title != "Overview" || info.Sheets[0].Rows != 50 || info.Sheets[0].Cols != 10 {
		t.Fatalf("unexpected sheet tab: %+v", info.Sheets[0])
	}
}

func TestClientGetValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v4/spreadsheets/sheet-abc/values/Sheet1!A1:B2" {
			http.NotFound(w, r)
			return
		}
		renderOpt := r.URL.Query().Get("valueRenderOption")
		if renderOpt != "FORMATTED_VALUE" {
			http.Error(w, "invalid render option: "+renderOpt, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"range":          "Sheet1!A1:B2",
			"majorDimension": "ROWS",
			"values": []any{
				[]any{"Name", "Score"},
				[]any{"Alice", "100"},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	vr, err := cl.GetValues(ctx, "sheet-abc", "Sheet1!A1:B2", "formatted")
	if err != nil {
		t.Fatalf("GetValues() failed: %v", err)
	}
	if vr.SpreadsheetID != "sheet-abc" || vr.Range != "Sheet1!A1:B2" || len(vr.Values) != 2 || vr.Values[1][0] != "Alice" {
		t.Fatalf("unexpected value range: %+v", vr)
	}
}

func TestClientAppendAndUpdateValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets/sheet-abc/values/Sheet1!A1:append" {
			inputOpt := r.URL.Query().Get("valueInputOption")
			if inputOpt != "USER_ENTERED" {
				http.Error(w, "unexpected input option: "+inputOpt, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-abc",
				"updates": map[string]any{
					"updatedRange":   "Sheet1!A2:B2",
					"updatedRows":    1,
					"updatedColumns": 2,
					"updatedCells":   2,
				},
			})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/v4/spreadsheets/sheet-abc/values/Sheet1!A1:B1" {
			inputOpt := r.URL.Query().Get("valueInputOption")
			if inputOpt != "RAW" {
				http.Error(w, "unexpected input option: "+inputOpt, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId":  "sheet-abc",
				"updatedRange":   "Sheet1!A1:B1",
				"updatedRows":    1,
				"updatedColumns": 2,
				"updatedCells":   2,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	appRes, err := cl.AppendValues(ctx, "sheet-abc", "Sheet1!A1", [][]interface{}{{"test", 123}}, "user_entered")
	if err != nil {
		t.Fatalf("AppendValues() failed: %v", err)
	}
	if appRes.SpreadsheetID != "sheet-abc" || appRes.UpdatedRange != "Sheet1!A2:B2" || appRes.UpdatedRows != 1 || appRes.UpdatedColumns != 2 || appRes.UpdatedCells != 2 {
		t.Fatalf("unexpected append result: %+v", appRes)
	}

	upRes, err := cl.UpdateValues(ctx, "sheet-abc", "Sheet1!A1:B1", [][]interface{}{{"test", 123}}, "raw")
	if err != nil {
		t.Fatalf("UpdateValues() failed: %v", err)
	}
	if upRes.SpreadsheetID != "sheet-abc" || upRes.UpdatedRange != "Sheet1!A1:B1" || upRes.UpdatedRows != 1 || upRes.UpdatedColumns != 2 || upRes.UpdatedCells != 2 {
		t.Fatalf("unexpected update result: %+v", upRes)
	}
}

func TestClientClearValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets/sheet-abc/values/Sheet1!A1:B5:clear" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-abc",
				"clearedRange":  "Sheet1!A1:B5",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	if err := cl.ClearValues(ctx, "sheet-abc", "Sheet1!A1:B5"); err != nil {
		t.Fatalf("ClearValues() failed: %v", err)
	}
}

func TestClientAddAndDeleteTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v4/spreadsheets/sheet-tab-test:batchUpdate" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests, _ := req["requests"].([]any)
		if len(requests) == 0 {
			http.Error(w, "empty requests", http.StatusBadRequest)
			return
		}
		firstReq := requests[0].(map[string]any)
		if addSheet, ok := firstReq["addSheet"].(map[string]any); ok {
			props := addSheet["properties"].(map[string]any)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-tab-test",
				"replies": []any{
					map[string]any{
						"addSheet": map[string]any{
							"properties": map[string]any{
								"sheetId": 999,
								"title":   props["title"],
								"index":   props["index"],
								"gridProperties": map[string]any{
									"rowCount":    1000,
									"columnCount": 26,
								},
							},
						},
					},
				},
			})
			return
		}
		if _, ok := firstReq["deleteSheet"].(map[string]any); ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-tab-test",
				"replies":       []any{map[string]any{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	tab, err := cl.AddTab(ctx, "sheet-tab-test", "NewTab", 1, 1000, 26)
	if err != nil {
		t.Fatalf("AddTab() failed: %v", err)
	}
	if tab.SheetID != 999 || tab.Title != "NewTab" || tab.RowCount != 1000 || tab.ColumnCount != 26 {
		t.Fatalf("unexpected tab result: %+v", tab)
	}

	if err := cl.DeleteTab(ctx, "sheet-tab-test", 999); err != nil {
		t.Fatalf("DeleteTab() failed: %v", err)
	}
}

func TestClientExportCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/spreadsheets/sheet-export" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-export",
				"sheets": []map[string]any{
					{
						"properties": map[string]any{
							"title": "FirstTab",
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/v4/spreadsheets/sheet-export/values/'FirstTab'" ||
			r.URL.Path == "/v4/spreadsheets/sheet-export/values/FirstTab" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "FirstTab!A1:B2",
				"values": []any{
					[]any{"HeaderA", "HeaderB"},
					[]any{"Val1", "Val2"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	// Test export to writer
	var buf strings.Builder
	if err := cl.ExportCSV(ctx, "sheet-export", "", "", &buf); err != nil {
		t.Fatalf("ExportCSV() to writer failed: %v", err)
	}
	wantCSV := "HeaderA,HeaderB\nVal1,Val2\n"
	if buf.String() != wantCSV {
		t.Fatalf("got CSV %q, want %q", buf.String(), wantCSV)
	}

	// Test export to file
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "exported.csv")
	if err := cl.ExportCSV(ctx, "sheet-export", "FirstTab", outFile, nil); err != nil {
		t.Fatalf("ExportCSV() to file failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantCSV {
		t.Fatalf("file content = %q, want %q", string(data), wantCSV)
	}
}

func TestClientExportCSV_EmptySpreadsheet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/spreadsheets/sheet-empty" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "sheet-empty",
				"sheets":        []map[string]any{},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	err = cl.ExportCSV(ctx, "sheet-empty", "", "", &buf)
	if err == nil {
		t.Fatalf("expected error exporting empty spreadsheet, got nil")
	}
}

func TestClientAddTab_Defaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests := req["requests"].([]any)
		firstReq := requests[0].(map[string]any)
		addSheet := firstReq["addSheet"].(map[string]any)
		props := addSheet["properties"].(map[string]any)

		// Verify default rows and cols
		grid := props["gridProperties"].(map[string]any)
		if grid["rowCount"].(float64) != 1000 || grid["columnCount"].(float64) != 26 {
			http.Error(w, "invalid grid defaults", http.StatusBadRequest)
			return
		}
		if _, hasIndex := props["index"]; hasIndex {
			http.Error(w, "index should not be set when negative", http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId": "sheet-tab-test",
			"replies": []any{
				map[string]any{
					"addSheet": map[string]any{
						"properties": map[string]any{
							"sheetId": 123,
							"title":   props["title"],
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	tab, err := cl.AddTab(ctx, "sheet-tab-test", "DefaultTab", -1, 0, 0)
	if err != nil {
		t.Fatalf("AddTab() failed: %v", err)
	}
	if tab.SheetID != 123 || tab.Title != "DefaultTab" || tab.RowCount != 1000 || tab.ColumnCount != 26 {
		t.Fatalf("unexpected tab result: %+v", tab)
	}
}

func TestClientAddTab_ZeroIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests := req["requests"].([]any)
		firstReq := requests[0].(map[string]any)
		addSheet := firstReq["addSheet"].(map[string]any)
		props := addSheet["properties"].(map[string]any)

		// Verify index is present and zero
		if index, ok := props["index"]; !ok || index.(float64) != 0 {
			http.Error(w, "index 0 should be serialized", http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId": "sheet-tab-test",
			"replies": []any{
				map[string]any{
					"addSheet": map[string]any{
						"properties": map[string]any{
							"sheetId": 124,
							"title":   props["title"],
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	tab, err := cl.AddTab(ctx, "sheet-tab-test", "ZeroIndexTab", 0, 10, 10)
	if err != nil {
		t.Fatalf("AddTab() failed: %v", err)
	}
	if tab.SheetID != 124 || tab.Title != "ZeroIndexTab" {
		t.Fatalf("unexpected tab result: %+v", tab)
	}
}

func TestClientExportCSV_SpecialTabName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/spreadsheets/sheet-special/values/'John''s Data'":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "'John''s Data'!A1:A2",
				"values": []any{
					[]any{"Score"},
					[]any{"99"},
				},
			})
		case "/v4/spreadsheets/sheet-special/values/'Sales!'":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "'Sales!'!A1:A1",
				"values": []any{
					[]any{"OK"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	if err := cl.ExportCSV(ctx, "sheet-special", "John's Data", "", &buf); err != nil {
		t.Fatalf("ExportCSV() with quotes failed: %v", err)
	}
	if buf.String() != "Score\n99\n" {
		t.Fatalf("got %q, want %q", buf.String(), "Score\n99\n")
	}

	buf.Reset()
	if err := cl.ExportCSV(ctx, "sheet-special", "Sales!", "", &buf); err != nil {
		t.Fatalf("ExportCSV() with exclamation mark failed: %v", err)
	}
	if buf.String() != "OK\n" {
		t.Fatalf("got %q, want %q", buf.String(), "OK\n")
	}
}

func TestClientExportCSV_InvalidFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"range": "Sheet1!A1",
			"values": []any{
				[]any{"test"},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()
	cl, err := NewClient(ctx, srv.Client(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	// Non-existent directory should fail file creation
	err = cl.ExportCSV(ctx, "sheet-123", "Sheet1", "/nonexistent_dir/never_create/test.csv", nil)
	if err == nil {
		t.Fatalf("expected error for invalid file path, got nil")
	}
}

func TestClientExportCSV_NoDestination(t *testing.T) {
	ctx := context.Background()
	cl := &Client{}
	err := cl.ExportCSV(ctx, "sheet-123", "Sheet1", "", nil)
	if err == nil {
		t.Fatalf("expected error when neither outPath nor outWriter is provided, got nil")
	}
	if !strings.Contains(err.Error(), "outPath or outWriter must be provided") {
		t.Fatalf("expected 'outPath or outWriter must be provided' error, got %v", err)
	}
}
