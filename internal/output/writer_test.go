package output

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type fakeView struct{}

func (fakeView) Headers() []string   { return []string{"ID", "NAME"} }
func (fakeView) Rows() [][]string    { return [][]string{{"1", "alpha"}, {"2", "beta"}} }

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWriter("table", &buf).Render(fakeView{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("table output missing rows:\n%s", out)
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"name": "alpha"}
	if err := NewWriter("json", &buf).Render(data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"name": "alpha"`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestRenderTableRejectsNonTableView(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWriter("table", &buf).Render(map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected error rendering non-TableView as table")
	}
}

type fakeTextView struct{}

func (fakeTextView) Text(w io.Writer) error {
	_, err := io.WriteString(w, "◆ hello\n")
	return err
}

func TestRenderText(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWriter("text", &buf).Render(fakeTextView{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "◆ hello\n" {
		t.Fatalf("text output = %q", buf.String())
	}
}

func TestRenderTextRejectsNonTextView(t *testing.T) {
	var buf bytes.Buffer
	err := NewWriter("text", &buf).Render(map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("expected error rendering non-TextView as text")
	}
	if !strings.Contains(err.Error(), "--output json") {
		t.Fatalf("error should point at a working format, got %q", err)
	}
}
