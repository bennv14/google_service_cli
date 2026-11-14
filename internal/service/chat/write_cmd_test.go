package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatSendCLIFlagsStdinJSONAndStderrSeparation(t *testing.T) {
	attachPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(attachPath, []byte("hello-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotText string
	var gotAttachments int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/spaces/A":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "spaces/A", "displayName": "Alpha"})
		case r.Method == http.MethodPost && r.URL.Path == "/upload/v1/spaces/A/attachments:upload":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attachmentDataRef": map[string]any{"resourceName": "spaces/A/attachments/att1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/spaces/A/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotText, _ = body["text"].(string)
			if atts, ok := body["attachment"].([]any); ok {
				gotAttachments = len(atts)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":       "spaces/A/messages/t1.m1",
				"text":       gotText,
				"createTime": "2026-11-14T10:00:00Z",
				"sender":     map[string]any{"name": "users/1", "displayName": "Linh", "type": "HUMAN"},
			})
		case r.URL.Path == "/v1/people:batchGet":
			_ = json.NewEncoder(w).Encode(map[string]any{"responses": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Run("piped stdin + attachment with -o json keeps stdout pristine", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		cmd := New().Command(testDeps(t, srv, "json", true, &out))
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetIn(strings.NewReader("from *stdin*"))
		cmd.SetArgs([]string{"send", "--space", "spaces/A", "--attach", attachPath})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext error: %v", err)
		}

		var decoded MessageInfo
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, out.String())
		}
		if decoded.Text != "from *stdin*" || gotAttachments != 1 {
			t.Fatalf("decoded=%+v gotAttachments=%d", decoded, gotAttachments)
		}
		if !strings.Contains(errBuf.String(), "Uploading notes.txt...") {
			t.Fatalf("stderr missing upload progress: %q", errBuf.String())
		}
	})

	t.Run("mutual exclusion between --text and positional arg", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		cmd := New().Command(testDeps(t, srv, "text", false, &out))
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"send", "--space", "spaces/A", "--text", "a", "b"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "both --text and positional") {
			t.Fatalf("err = %v, want mutual exclusion error", err)
		}
	})
}

func TestChatEditCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/spaces/A/messages/t1.m1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":       "spaces/A/messages/t1.m1",
				"text":       body["text"],
				"createTime": "2026-11-14T10:00:00Z",
				"sender":     map[string]any{"name": "users/1", "displayName": "Linh", "type": "HUMAN"},
			})
		case r.URL.Path == "/v1/people:batchGet":
			_ = json.NewEncoder(w).Encode(map[string]any{"responses": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var out, errBuf bytes.Buffer
	cmd := New().Command(testDeps(t, srv, "table", true, &out))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"edit", "A/t1/m1", "new body"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext error: %v", err)
	}
	if !strings.Contains(out.String(), "TIME") || !strings.Contains(out.String(), "new body") {
		t.Fatalf("table output = %q", out.String())
	}
}

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

func TestChatReactCLIAndFriendlyScopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/spaces/A/messages/t1.m1/reactions" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    403,
					"message": "Request had insufficient authentication scopes.",
					"errors": []map[string]any{
						{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	var out, errBuf bytes.Buffer
	cmd := New().Command(testDeps(t, srv, "text", false, &out))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"react", "A/t1/m1", "👍"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gsvc auth login") {
		t.Fatalf("err = %v, want friendly scope error", err)
	}
}
