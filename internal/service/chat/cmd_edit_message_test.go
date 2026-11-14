package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
