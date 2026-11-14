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
