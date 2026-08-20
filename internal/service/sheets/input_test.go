package sheets

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseValuesFromPositionalArgs(t *testing.T) {
	args := []string{"apple", "123", "true"}
	vals, err := ParseValues("", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := [][]interface{}{
		{"apple", "123", "true"},
	}
	if !reflect.DeepEqual(vals, expected) {
		t.Fatalf("got %v, want %v", vals, expected)
	}
}

func TestParseValuesFromCSVReader(t *testing.T) {
	csvData := "Name,Age,City\nAlice,30,New York\nBob,25,San Francisco\n"
	r := strings.NewReader(csvData)

	vals, err := ParseValues("", nil, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := [][]interface{}{
		{"Name", "Age", "City"},
		{"Alice", "30", "New York"},
		{"Bob", "25", "San Francisco"},
	}
	if !reflect.DeepEqual(vals, expected) {
		t.Fatalf("got %v, want %v", vals, expected)
	}
}

func TestParseValuesFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	csvFile := filepath.Join(tmpDir, "data.csv")
	content := "Product,Price\nWidget,19.99\nGadget,29.99\n"
	if err := os.WriteFile(csvFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	vals, err := ParseValues(csvFile, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := [][]interface{}{
		{"Product", "Price"},
		{"Widget", "19.99"},
		{"Gadget", "29.99"},
	}
	if !reflect.DeepEqual(vals, expected) {
		t.Fatalf("got %v, want %v", vals, expected)
	}
}

func TestParseValuesPriority(t *testing.T) {
	// File should take priority over args and reader
	tmpDir := t.TempDir()
	csvFile := filepath.Join(tmpDir, "data.csv")
	if err := os.WriteFile(csvFile, []byte("From,File\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reader := strings.NewReader("From,Reader\n")
	args := []string{"From", "Args"}

	vals, err := ParseValues(csvFile, args, reader)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0][0] != "From" || vals[0][1] != "File" {
		t.Fatalf("expected file content to take priority, got %v", vals)
	}
}

func TestParseValuesFileError(t *testing.T) {
	_, err := ParseValues("non_existent_file_12345.csv", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestParseValuesFromOSFile(t *testing.T) {
	tmpDir := t.TempDir()
	csvFile := filepath.Join(tmpDir, "data.csv")
	content := "ColA,ColB\nVal1,Val2\n"
	if err := os.WriteFile(csvFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(csvFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	vals, err := ParseValues("", nil, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := [][]interface{}{
		{"ColA", "ColB"},
		{"Val1", "Val2"},
	}
	if !reflect.DeepEqual(vals, expected) {
		t.Fatalf("got %v, want %v", vals, expected)
	}
}

func TestParseValuesEmptyReaderFallsBackToArgs(t *testing.T) {
	r := strings.NewReader("")
	args := []string{"fallback", "arg"}

	vals, err := ParseValues("", args, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := [][]interface{}{
		{"fallback", "arg"},
	}
	if !reflect.DeepEqual(vals, expected) {
		t.Fatalf("got %v, want %v", vals, expected)
	}
}

func TestParseValuesNoInputError(t *testing.T) {
	_, err := ParseValues("", nil, nil)
	if err == nil {
		t.Fatal("expected error when no input is provided")
	}
}

func TestParseCSVEmpty(t *testing.T) {
	r := strings.NewReader("")
	vals, err := ParseCSV(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty slice, got %v", vals)
	}
}

func TestParseCSVInvalid(t *testing.T) {
	// Unclosed quotes in CSV trigger a parsing error
	r := strings.NewReader(`"unclosed quote,val2`)
	_, err := ParseCSV(r)
	if err == nil {
		t.Fatal("expected error for invalid CSV format")
	}
}

func TestCleanPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/foo/bar/../baz", "/foo/baz"},
		{"./local/file.csv", "local/file.csv"},
		{"", ""},
	}
	for _, tt := range tests {
		got := CleanPath(tt.input)
		if got != tt.want {
			t.Errorf("CleanPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
