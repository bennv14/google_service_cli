package output

import (
	"bytes"
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
