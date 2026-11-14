package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatDeleteCLITTYAndNonTTY(t *testing.T) {
	var deletes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/v1/spaces/A/messages/t1.m1" {
			deletes++
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	t.Run("non-TTY without --yes fails fast", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		cmd := New().Command(testDeps(t, srv, "text", false, &out))
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetArgs([]string{"delete", "A/t1/m1"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("err = %v", err)
		}
		if deletes != 0 {
			t.Fatalf("deletes = %d, want 0", deletes)
		}
	})

	t.Run("TTY prompt with -o json writes prompt to stderr and JSON to stdout", func(t *testing.T) {
		orig := isInteractiveTTY
		isInteractiveTTY = func(io.Reader, io.Writer) bool { return true }
		t.Cleanup(func() { isInteractiveTTY = orig })

		var out, errBuf bytes.Buffer
		cmd := New().Command(testDeps(t, srv, "json", true, &out))
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetIn(strings.NewReader("yes\n"))
		cmd.SetArgs([]string{"delete", "A/t1/m1"})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext error: %v", err)
		}
		var dr DeleteResult
		if err := json.Unmarshal(out.Bytes(), &dr); err != nil {
			t.Fatalf("stdout invalid JSON: %v\n%s", err, out.String())
		}
		if !dr.Deleted || dr.ID != "A/t1/m1" {
			t.Fatalf("dr = %+v", dr)
		}
		if !strings.Contains(errBuf.String(), "Delete message A/t1/m1") {
			t.Fatalf("stderr missing prompt: %q", errBuf.String())
		}
	})
}
