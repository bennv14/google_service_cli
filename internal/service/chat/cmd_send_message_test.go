package chat

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
)

func TestChatSendCLIFlagsStdinJSONAndStderrSeparation(t *testing.T) {
	attachPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(attachPath, []byte("hello-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotText string
	var gotAttachments int
	var peopleLookups int
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
			peopleLookups++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"responses": []any{
					map[string]any{
						"requestedResourceName": "people/1",
						"person": map[string]any{
							"resourceName": "people/1",
							"names":        []map[string]any{{"displayName": "Linh Nguyen"}},
						},
					},
				},
			})
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

	t.Run("refresh-names bypasses cached directory name", func(t *testing.T) {
		var out bytes.Buffer
		deps := testDeps(t, srv, "json", true, &out)
		before := peopleLookups

		cmd1 := New().Command(deps)
		cmd1.SetOut(&out)
		cmd1.SetErr(new(bytes.Buffer))
		cmd1.SetArgs([]string{"send", "--space", "spaces/A", "first"})
		if err := cmd1.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("first send error: %v", err)
		}
		out.Reset()

		// Without --refresh-names, second call hits the People cache.
		cmd2 := New().Command(deps)
		cmd2.SetOut(&out)
		cmd2.SetErr(new(bytes.Buffer))
		cmd2.SetArgs([]string{"send", "--space", "spaces/A", "second"})
		if err := cmd2.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("second send error: %v", err)
		}
		if peopleLookups-before != 1 {
			t.Fatalf("expected 1 people lookup before refresh, got %d", peopleLookups-before)
		}
		out.Reset()

		// With --refresh-names, third call re-fetches from People API.
		cmd3 := New().Command(deps)
		cmd3.SetOut(&out)
		cmd3.SetErr(new(bytes.Buffer))
		cmd3.SetArgs([]string{"send", "--space", "spaces/A", "--refresh-names", "third"})
		if err := cmd3.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("third send error: %v", err)
		}
		if peopleLookups-before != 2 {
			t.Fatalf("expected 2 people lookups after --refresh-names, got %d", peopleLookups-before)
		}
	})
}
