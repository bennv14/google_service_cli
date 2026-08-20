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
	cmd.SetArgs([]string{"export", "exp-123", "--out", outFile})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
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
	var stdoutBuf bytes.Buffer
	cmd2.SetOut(&stdoutBuf)
	cmd2.SetArgs([]string{"export", "exp-123"})
	if err := cmd2.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdoutBuf.String()) != "H1,H2" {
		t.Fatalf("export stdout = %q, want %q", stdoutBuf.String(), "H1,H2")
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

