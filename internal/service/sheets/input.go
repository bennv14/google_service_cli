package sheets

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CleanPath standardizes and cleans a filesystem path to prevent traversal vulnerabilities.
func CleanPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// ParseValues converts input from a file, reader (stdin), or positional arguments into [][]interface{}.
// Priority order:
// 1. filePath (if non-empty)
// 2. reader (if non-nil and has content)
// 3. args (if non-empty)
func ParseValues(filePath string, args []string, r io.Reader) ([][]interface{}, error) {
	if filePath != "" {
		f, err := os.Open(CleanPath(filePath))
		if err != nil {
			return nil, fmt.Errorf("failed to open CSV file: %w", err)
		}
		defer f.Close()
		return ParseCSV(f)
	}

	if r != nil {
		// If r is *os.File, verify it is a pipe or regular file with data
		if f, ok := r.(*os.File); ok {
			stat, err := f.Stat()
			if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
				return ParseCSV(f)
			}
		} else {
			// For buffers and custom readers used in tests
			vals, err := ParseCSV(r)
			if err == nil && len(vals) > 0 {
				return vals, nil
			}
		}
	}

	if len(args) > 0 {
		row := make([]interface{}, len(args))
		for i, a := range args {
			row[i] = a
		}
		return [][]interface{}{row}, nil
	}

	return nil, errors.New("no input data provided: specify values, a CSV file via -f/--file, or pipe CSV to stdin")
}

// ParseCSV reads CSV data from an io.Reader and converts it into a 2D interface slice.
func ParseCSV(r io.Reader) ([][]interface{}, error) {
	csvReader := csv.NewReader(r)
	csvReader.FieldsPerRecord = -1 // Allow variable number of fields per row
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}
	if len(records) == 0 {
		return [][]interface{}{}, nil
	}

	vals := make([][]interface{}, len(records))
	for i, record := range records {
		row := make([]interface{}, len(record))
		for j, cell := range record {
			row[j] = cell
		}
		vals[i] = row
	}
	return vals, nil
}
