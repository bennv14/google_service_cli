package sheets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/option"

	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
)

func setupTestDeps(srv *httptest.Server, format string, buf *bytes.Buffer) *service.Deps {
	testClientOpts = []option.ClientOption{option.WithEndpoint(srv.URL + "/"), option.WithoutAuthentication()}
	return &service.Deps{
		Out: output.NewWriter(format, buf),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
}

func TestSheetsServiceNameAndScopes(t *testing.T) {
	s := New()
	if s.Name() != "sheets" {
		t.Fatalf("expected 'sheets', got %s", s.Name())
	}
	if len(s.Scopes()) != 2 {
		t.Fatalf("expected 2 scopes, got %v", s.Scopes())
	}
}

func TestCreateCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId":  "created-123",
			"spreadsheetUrl": "https://docs.google.com/spreadsheets/d/created-123",
			"properties": map[string]any{
				"title": "My Sheet",
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"create", "My Sheet", "--tab", "DefaultTab"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), `"spreadsheetId": "created-123"`) {
		t.Fatalf("output missing spreadsheetId: %s", buf.String())
	}
}

func TestInfoCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"spreadsheetId": "info-123",
			"properties": map[string]any{
				"title":    "Info Sheet",
				"locale":   "en_US",
				"timeZone": "UTC",
			},
			"sheets": []map[string]any{
				{
					"properties": map[string]any{
						"sheetId": 0,
						"title":   "Sheet1",
						"index":   0,
						"gridProperties": map[string]any{
							"rowCount":    100,
							"columnCount": 26,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"info", "info-123"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), `"info-123"`) {
		t.Fatalf("output missing info-123: %s", buf.String())
	}
}

func TestGetCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"range":          "Sheet1!A1:B2",
			"majorDimension": "ROWS",
			"values": []any{
				[]any{"Col1", "Col2"},
				[]any{"A", "B"},
			},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"get", "get-123", "Sheet1!A1:B2", "--render", "formatted"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), `"Col1"`) {
		t.Fatalf("output missing Col1: %s", buf.String())
	}
}

func TestAppendAndUpdateCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "append-123",
				"updates": map[string]any{
					"updatedRange": "Sheet1!A2:B2",
					"updatedRows":  1,
				},
			})
			return
		}
		if r.Method == http.MethodPut {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "append-123",
				"updatedRange":  "Sheet1!A1:B1",
				"updatedRows":   1,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"append", "append-123", "Sheet1!A1", "val1", "val2"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"Sheet1!A2:B2"`) {
		t.Fatalf("append output missing range: %s", buf.String())
	}

	buf.Reset()
	cmd.SetArgs([]string{"update", "append-123", "Sheet1!A1:B1", "val1", "val2"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"Sheet1!A1:B1"`) {
		t.Fatalf("update output missing range: %s", buf.String())
	}
}

func TestClearCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"clearedRange": "Sheet1!A1:D10",
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "text", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"clear", "clear-123", "Sheet1!A1:D10"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Cleared range Sheet1!A1:D10") {
		t.Fatalf("clear output unexpected: %s", buf.String())
	}
}

func TestAddAndDeleteTabCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests, _ := req["requests"].([]any)
		if len(requests) != 1 {
			http.Error(w, "expected 1 request", http.StatusBadRequest)
			return
		}
		first := requests[0].(map[string]any)
		if _, ok := first["addSheet"]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "tab-123",
				"replies": []any{
					map[string]any{
						"addSheet": map[string]any{
							"properties": map[string]any{
								"sheetId": 333,
								"title":   "NewTab",
								"index":   1,
								"gridProperties": map[string]any{
									"rowCount":    500,
									"columnCount": 20,
								},
							},
						},
					},
				},
			})
			return
		}
		if del, ok := first["deleteSheet"].(map[string]any); ok {
			if del["sheetId"].(float64) != 333 {
				http.Error(w, "unexpected sheetId", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "tab-123",
				"replies":       []any{map[string]any{}},
			})
			return
		}
		http.Error(w, "unknown batchUpdate request", http.StatusBadRequest)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"add-tab", "tab-123", "NewTab", "--rows", "500", "--cols", "20", "--index", "1"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"sheetId": 333`) {
		t.Fatalf("add-tab output missing sheetId: %s", buf.String())
	}

	buf.Reset()
	cmd.SetArgs([]string{"delete-tab", "tab-123", "333"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Deleted tab 333") {
		t.Fatalf("delete-tab output missing confirmation: %s", buf.String())
	}

	// Also test invalid sheetId
	cmd.SetArgs([]string{"delete-tab", "tab-123", "not-a-number"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for non-integer sheetId")
	}
}

func TestExportCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/spreadsheets/exp-123" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "exp-123",
				"sheets": []map[string]any{
					{"properties": map[string]any{"title": "Data"}},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"range":  "Data!A1:B1",
			"values": []any{[]any{"H1", "H2"}},
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "out.csv")

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "text", &buf)
	defer func() { testClientOpts = nil }()

	cmd := New().Command(deps)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)
	cmd.SetArgs([]string{"export", "exp-123", "--out", outFile})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stdoutBuf.Len() != 0 {
		t.Fatalf("expected empty stdout when --out is used, got %q", stdoutBuf.String())
	}
	if !strings.Contains(stderrBuf.String(), "Exported to ") {
		t.Fatalf("expected stderr notice, got %q", stderrBuf.String())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "H1,H2" {
		t.Fatalf("export CSV file = %q, want %q", string(data), "H1,H2")
	}

	// Test export to stdout
	cmd2 := New().Command(deps)
	var outBuf2 bytes.Buffer
	cmd2.SetOut(&outBuf2)
	cmd2.SetArgs([]string{"export", "exp-123"})
	if err := cmd2.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(outBuf2.String()) != "H1,H2" {
		t.Fatalf("export stdout = %q, want %q", outBuf2.String(), "H1,H2")
	}
}

func TestAppendAndUpdateWithFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "append-file-123",
				"updates": map[string]any{
					"updatedRange": "Sheet1!A1:B2",
					"updatedRows":  2,
				},
			})
			return
		}
		if r.Method == http.MethodPut {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "append-file-123",
				"updatedRange":  "Sheet1!A1:B2",
				"updatedRows":   2,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	csvFile := filepath.Join(tmpDir, "data.csv")
	if err := os.WriteFile(csvFile, []byte("r1c1,r1c2\nr2c1,r2c2\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	// Append with file
	cmd := New().Command(deps)
	cmd.SetArgs([]string{"append", "append-file-123", "Sheet1!A1", "--file", csvFile})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"updatedRows": 2`) {
		t.Fatalf("append output missing updatedRows: %s", buf.String())
	}

	// Update with file
	buf.Reset()
	cmd2 := New().Command(deps)
	cmd2.SetArgs([]string{"update", "append-file-123", "Sheet1!A1", "-f", csvFile})
	if err := cmd2.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"updatedRows": 2`) {
		t.Fatalf("update output missing updatedRows: %s", buf.String())
	}
}

func TestCommandErrors(t *testing.T) {
	// 1. Client auth error
	authErrDeps := &service.Deps{
		Out: output.NewWriter("json", &bytes.Buffer{}),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			return nil, context.Canceled
		},
	}

	subcommands := [][]string{
		{"create", "Title"},
		{"info", "id"},
		{"get", "id", "A1"},
		{"append", "id", "A1", "val"},
		{"update", "id", "A1", "val"},
		{"clear", "id", "A1"},
		{"add-tab", "id", "Tab"},
		{"delete-tab", "id", "1"},
		{"export", "id"},
	}

	for _, sub := range subcommands {
		cmd := New().Command(authErrDeps)
		cmd.SetArgs(sub)
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Fatalf("expected client init error for %v, got nil", sub)
		}
	}

	// 2. HTTP 500 error from API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	for _, sub := range subcommands {
		cmd := New().Command(deps)
		cmd.SetArgs(sub)
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Fatalf("expected API error for %v, got nil", sub)
		}
	}

	// 3. Append / Update missing values
	cmdNoVal := New().Command(deps)
	cmdNoVal.SetArgs([]string{"append", "id", "A1"})
	if err := cmdNoVal.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error when append has no values or file")
	}

	cmdNoVal2 := New().Command(deps)
	cmdNoVal2.SetArgs([]string{"update", "id", "A1"})
	if err := cmdNoVal2.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error when update has no values or file")
	}
}

func TestFriendlyErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(path, "404") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"Requested entity was not found."}}`))
			return
		}
		if strings.Contains(path, "403-scope") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"code":403,"message":"Request had insufficient authentication scopes."}}`))
			return
		}
		if strings.Contains(path, "403-disabled") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"code":403,"message":"Google Sheets API has not been used in project 123 before or it is disabled."}}`))
			return
		}
		if strings.Contains(path, "403") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"code":403,"message":"The caller does not have permission"}}`))
			return
		}
		if strings.Contains(path, "400") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":400,"message":"Unable to parse range: InvalidRange"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	deps := setupTestDeps(srv, "json", &buf)
	defer func() { testClientOpts = nil }()

	// Test 404
	cmd := New().Command(deps)
	cmd.SetArgs([]string{"info", "404-id"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "spreadsheet not found (verify spreadsheet ID)") {
		t.Fatalf("expected 404 friendly error, got: %v", err)
	}

	// Test 403
	cmd = New().Command(deps)
	cmd.SetArgs([]string{"info", "403-id"})
	err = cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied (ensure your account has access to this spreadsheet)") {
		t.Fatalf("expected 403 friendly error, got: %v", err)
	}

	// Test 403 insufficient scope
	cmd = New().Command(deps)
	cmd.SetArgs([]string{"info", "403-scope-id"})
	err = cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing OAuth scopes: run 'gsvc auth login' again") {
		t.Fatalf("expected 403 scope error, got: %v", err)
	}

	// Test 403 API disabled
	cmd = New().Command(deps)
	cmd.SetArgs([]string{"info", "403-disabled-id"})
	err = cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Google Sheets API is not enabled") {
		t.Fatalf("expected 403 API disabled error, got: %v", err)
	}

	// Test 400
	cmd = New().Command(deps)
	cmd.SetArgs([]string{"get", "400-id", "InvalidRange"})
	err = cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid range notation") {
		t.Fatalf("expected 400 friendly error, got: %v", err)
	}
}

func TestInvalidOptionFlags(t *testing.T) {
	authErrDeps := &service.Deps{
		Out: output.NewWriter("json", &bytes.Buffer{}),
		NewClient: func(ctx context.Context, scopes ...string) (*http.Client, error) {
			t.Fatal("NewClient should not be called when option flags are invalid")
			return nil, nil
		},
	}

	cmd := New().Command(authErrDeps)
	cmd.SetArgs([]string{"get", "id", "Sheet1!A1", "--render", "bogus"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for invalid --render")
	}

	cmd = New().Command(authErrDeps)
	cmd.SetArgs([]string{"append", "id", "Sheet1!A1", "--input-type", "bogus", "val"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for invalid --input-type")
	}

	// Missing values should fail before calling NewClient
	cmd = New().Command(authErrDeps)
	cmd.SetArgs([]string{"update", "id", "Sheet1!A1"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error when no values provided")
	}
}

func TestTextOutputCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId":  "id-1",
				"spreadsheetUrl": "https://example.com/1",
				"properties":     map[string]any{"title": "My Sheet"},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/values/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range":  "Sheet1!A1:B1",
				"values": []any{[]any{"A", "B"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":append"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "id-1",
				"updates": map[string]any{
					"updatedRange": "Sheet1!A2:B2",
					"updatedRows":  1,
				},
			})
		case r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "id-1",
				"updatedRange":  "Sheet1!A1:B1",
				"updatedRows":   1,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":batchUpdate"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spreadsheetId": "id-1",
				"replies": []any{
					map[string]any{
						"addSheet": map[string]any{
							"properties": map[string]any{"sheetId": 5, "title": "Tab5"},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cases := [][]string{
		{"create", "My Sheet"},
		{"get", "id-1", "Sheet1!A1:B1"},
		{"append", "id-1", "Sheet1!A1", "x", "y"},
		{"update", "id-1", "Sheet1!A1:B1", "x", "y"},
		{"add-tab", "id-1", "Tab5"},
	}
	for _, args := range cases {
		var buf bytes.Buffer
		deps := setupTestDeps(srv, "text", &buf)
		cmd := New().Command(deps)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(context.Background())
		testClientOpts = nil
		if err != nil {
			t.Fatalf("command %v failed with -o text: %v", args, err)
		}
		if buf.Len() == 0 {
			t.Fatalf("command %v produced empty text output", args)
		}
	}
}
