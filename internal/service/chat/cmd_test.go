package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"google.golang.org/api/option"

	"github.com/bennv14/google_service_cli/internal/config"
	"github.com/bennv14/google_service_cli/internal/output"
	"github.com/bennv14/google_service_cli/internal/service"
)

// chatServer serves one space with two messages in one thread.
func chatServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/spaces":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"spaces": []map[string]any{{
					"name": "spaces/A", "displayName": "Alpha",
					"spaceType": "SPACE", "spaceThreadingState": "THREADED_MESSAGES",
				}},
			})
		case "/v1/spaces/A":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "spaces/A", "displayName": "Alpha",
				"spaceType": "SPACE", "spaceThreadingState": "THREADED_MESSAGES",
			})
		case "/v1/users/me/spaces/A/spaceReadState":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "users/114427003/spaces/A/spaceReadState",
				"lastReadTime": "2026-07-25T00:00:00Z",
			})
		case "/v1/spaces/A/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{
					{
						"name": "spaces/A/messages/t1.t1", "text": "hello",
						"createTime": "2026-07-25T09:12:00Z",
						"thread":     map[string]any{"name": "spaces/A/threads/t1"},
						"sender":     map[string]any{"name": "users/1", "displayName": "Linh", "type": "HUMAN"},
					},
					{
						"name": "spaces/A/messages/t1.t2", "text": "ok",
						"createTime": "2026-07-25T09:20:00Z",
						"thread":     map[string]any{"name": "spaces/A/threads/t1"},
						"sender":     map[string]any{"name": "users/2", "displayName": "Huy", "type": "HUMAN"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testDeps wires Deps at a test server, with the requested --output format.
func testDeps(t *testing.T, srv *httptest.Server, format string, explicit bool, out *bytes.Buffer) *service.Deps {
	t.Helper()
	testClientOpts = []option.ClientOption{
		option.WithEndpoint(srv.URL + "/"), option.WithoutAuthentication(),
	}
	t.Cleanup(func() { testClientOpts = nil })
	return &service.Deps{
		Profile:        config.Profile{Name: "test", Defaults: map[string]string{"chat_account_index": "1"}},
		OutputFormat:   format,
		OutputExplicit: explicit,
		NewOut:         func(f string) output.Writer { return output.NewWriter(f, out) },
		Out:            output.NewWriter(format, out),
		NewClient: func(context.Context, ...string) (*http.Client, error) {
			return srv.Client(), nil
		},
	}
}

func TestChatServiceNameAndScopes(t *testing.T) {
	s := New()
	if s.Name() != "chat" {
		t.Fatalf("Name() = %q", s.Name())
	}
	if len(s.Scopes()) != 3 {
		t.Fatalf("Scopes() = %v", s.Scopes())
	}
	for _, sc := range s.Scopes() {
		if !strings.HasSuffix(sc, ".readonly") {
			t.Errorf("scope %q is not read-only", sc)
		}
	}
}

func TestChatMessagesRendersJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := New().Command(testDeps(t, chatServer(t), "json", true, &out))
	cmd.SetArgs([]string{"messages", "--space", "spaces/A"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d spaces: %s", len(got), out.String())
	}
	sp := got[0]["space"].(map[string]any)
	if sp["link"] != "https://chat.google.com/u/1/app/chat/A" {
		t.Errorf("chat_account_index from the profile should reach the link, got %v", sp["link"])
	}
}

func TestChatMessagesDefaultsToText(t *testing.T) {
	var out bytes.Buffer
	// OutputExplicit is false: the user did not pass -o, so chat picks text.
	cmd := New().Command(testDeps(t, chatServer(t), "table", false, &out))
	cmd.SetArgs([]string{"messages", "--space", "spaces/A"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "◆ Alpha") {
		t.Fatalf("expected the text tree, got:\n%s", out.String())
	}
}

func TestChatExplicitOutputWins(t *testing.T) {
	var out bytes.Buffer
	cmd := New().Command(testDeps(t, chatServer(t), "table", true, &out))
	cmd.SetArgs([]string{"messages", "--space", "spaces/A"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SENDER") {
		t.Fatalf("expected the table header, got:\n%s", out.String())
	}
}

func TestChatSummaryGoesToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	cmd := New().Command(testDeps(t, chatServer(t), "json", true, &out))
	cmd.SetArgs([]string{"messages", "--space", "spaces/A"})
	cmd.SetErr(&errBuf)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "scanned 1 space") {
		t.Fatalf("summary missing from stderr: %q", errBuf.String())
	}
	if strings.Contains(out.String(), "scanned") {
		t.Fatalf("summary leaked into stdout: %s", out.String())
	}
	if err := json.Unmarshal(out.Bytes(), new([]any)); err != nil {
		t.Fatalf("stdout must stay valid JSON: %v", err)
	}
}

func TestChatSpacesListsSpaces(t *testing.T) {
	var out bytes.Buffer
	cmd := New().Command(testDeps(t, chatServer(t), "json", true, &out))
	cmd.SetArgs([]string{"spaces"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"Alpha"`) {
		t.Fatalf("spaces output = %s", out.String())
	}
}

func TestChatPresetsExistWithTheRightDefaults(t *testing.T) {
	cmd := New().Command(&service.Deps{})
	byName := map[string]bool{}
	for _, c := range cmd.Commands() {
		byName[c.Name()] = true
	}
	for _, want := range []string{"spaces", "threads", "messages", "unread", "mentions", "thread"} {
		if !byName[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}

	find := func(name string) *cobra.Command {
		for _, c := range cmd.Commands() {
			if c.Name() == name {
				return c
			}
		}
		t.Fatalf("no subcommand %q", name)
		return nil
	}
	if got := find("messages").Flags().Lookup("limit").DefValue; got != "50" {
		t.Errorf("messages --limit default = %q, want 50", got)
	}
	if got := find("threads").Flags().Lookup("limit").DefValue; got != "20" {
		t.Errorf("threads --limit default = %q, want 20", got)
	}
	if got := find("threads").Flags().Lookup("since").DefValue; got != "30d" {
		t.Errorf("threads --since default = %q, want 30d", got)
	}
	if got := find("mentions").Flags().Lookup("since").DefValue; got != "7d" {
		t.Errorf("mentions --since default = %q, want 7d", got)
	}
	// Every command that scans more than one space can narrow that scan.
	for _, name := range []string{"messages", "unread", "mentions", "spaces"} {
		if find(name).Flags().Lookup("type") == nil {
			t.Errorf("%s is missing --type", name)
		}
	}
}

func TestChatRejectsBadTimeAndGroup(t *testing.T) {
	var out bytes.Buffer
	deps := testDeps(t, chatServer(t), "json", true, &out)

	cmd := New().Command(deps)
	cmd.SetArgs([]string{"messages", "--space", "spaces/A", "--since", "yesterday"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Error("expected --since yesterday to be rejected")
	}

	cmd = New().Command(deps)
	cmd.SetArgs([]string{"messages", "--space", "spaces/A", "--group", "sideways"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Error("expected --group sideways to be rejected")
	}
}
