package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	chatapi "google.golang.org/api/chat/v1"
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
		case "/v1/people:batchGet":
			names := map[string]string{"people/1": "Linh Tran", "people/2": "Huy Nguyen"}
			var responses []map[string]any
			for _, n := range r.URL.Query()["resourceNames"] {
				if display, ok := names[n]; ok {
					responses = append(responses, map[string]any{
						"requestedResourceName": n,
						"person": map[string]any{
							"names":          []map[string]any{{"displayName": display}},
							"emailAddresses": []map[string]any{{"value": strings.ToLower(strings.Split(display, " ")[0]) + "@example.com"}},
						},
					})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
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
		ConfigDir:      t.TempDir(),
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
	if len(s.Scopes()) != 4 {
		t.Fatalf("Scopes() = %v", s.Scopes())
	}
	for _, sc := range s.Scopes() {
		if !strings.HasSuffix(sc, ".readonly") {
			t.Errorf("scope %q is not read-only", sc)
		}
	}
}

func TestNameCachePathIsPerProfile(t *testing.T) {
	d := &service.Deps{ConfigDir: "/cfg", Profile: config.Profile{Name: "work"}}
	if got, want := nameCachePath(d), filepath.Join("/cfg", "cache", "people-work.json"); got != want {
		t.Fatalf("nameCachePath = %q, want %q", got, want)
	}
	// A different profile must never read the first profile's names.
	other := &service.Deps{ConfigDir: "/cfg", Profile: config.Profile{Name: "personal"}}
	if nameCachePath(other) == nameCachePath(d) {
		t.Fatal("two profiles must not share one cache file")
	}
}

// Without somewhere safe to write, caching is skipped rather than guessed at.
func TestNameCachePathIsEmptyWithoutAConfigDirOrProfile(t *testing.T) {
	if got := nameCachePath(&service.Deps{Profile: config.Profile{Name: "work"}}); got != "" {
		t.Errorf("no ConfigDir must disable the cache, got %q", got)
	}
	if got := nameCachePath(&service.Deps{ConfigDir: "/cfg"}); got != "" {
		t.Errorf("no profile name must disable the cache, got %q", got)
	}
}

func TestNewDirectoryCachesWhenItHasSomewhereToWrite(t *testing.T) {
	srv := chatServer(t)
	d := testDeps(t, srv, "json", true, new(bytes.Buffer))

	dir, err := newDirectory(context.Background(), d, srv.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := dir.(*cachedDirectory)
	if !ok {
		t.Fatalf("newDirectory = %T, want a *cachedDirectory", dir)
	}
	if c.path != nameCachePath(d) {
		t.Fatalf("cache path = %q, want %q", c.path, nameCachePath(d))
	}
	if _, ok := c.inner.(*peopleDirectory); !ok {
		t.Fatalf("cached directory wraps %T, want a *peopleDirectory", c.inner)
	}
}

func TestNewDirectorySkipsTheCacheWithNowhereToWrite(t *testing.T) {
	srv := chatServer(t)
	d := testDeps(t, srv, "json", true, new(bytes.Buffer))
	d.ConfigDir = ""

	dir, err := newDirectory(context.Background(), d, srv.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dir.(*peopleDirectory); !ok {
		t.Fatalf("newDirectory = %T, want an uncached *peopleDirectory", dir)
	}
}

// --refresh-names is useless on a command that cannot show a name, so every
// chat subcommand carries it — including spaces, which does not use
// addQueryFlags.
func TestRefreshNamesFlagIsOnEverySubcommand(t *testing.T) {
	cmd := New().Command(&service.Deps{})
	for _, c := range cmd.Commands() {
		if c.Flags().Lookup("refresh-names") == nil {
			t.Errorf("chat %s is missing --refresh-names", c.Name())
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

func TestShowsThreadTitlesFollowsTheChosenOutput(t *testing.T) {
	// A title costs a request per thread whose opening message the scan missed,
	// so only the outputs that actually print one should ask for it.
	cases := []struct {
		name     string
		format   string
		explicit bool
		group    string
		want     bool
	}{
		{"default output is chat's text tree", "table", false, "", true},
		{"--group flat has no thread header", "table", false, "flat", false},
		{"--group space keeps the header", "table", false, "space", true},
		{"--group thread keeps the header", "table", false, "thread", true},
		{"-o table shows the thread ID, not a label", "table", true, "", false},
		{"-o table stays off whatever the grouping", "table", true, "space", false},
		{"-o json always carries thread.title", "json", true, "", true},
		{"-o json carries it even under --group flat", "json", true, "flat", true},
		{"-o text is the tree again", "text", true, "", true},
		{"-o text obeys --group flat", "text", true, "flat", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &service.Deps{OutputFormat: c.format, OutputExplicit: c.explicit}
			if got := showsThreadTitles(d, &queryFlags{group: c.group}); got != c.want {
				t.Fatalf("showsThreadTitles(%q, explicit=%v, --group %q) = %v, want %v",
					c.format, c.explicit, c.group, got, c.want)
			}
		})
	}
}

func TestSkipThreadTitlesSuppressesTheFetch(t *testing.T) {
	// The other half of the decision: the engine must honour it. Only a reply is
	// in hand, so this thread would otherwise cost one GetMessage.
	api := titleAPI(t,
		rawMsg("spaces/A/messages/t1.t9", "spaces/A/threads/t1", "users/2", "Huy", "which server?", "2026-07-25T09:05:00Z"),
	)
	api.getMessage = func(name string) (*chatapi.Message, error) {
		t.Fatalf("unexpected GetMessage(%q): this output has no thread header", name)
		return nil, nil
	}

	res, err := NewEngine(api, &fakeDirectory{}).Run(context.Background(), Query{
		Now: fixedTime(t, "2026-07-26T00:00:00Z"), skipThreadTitles: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Spaces[0].Threads[0].Thread.Title; got != "" {
		t.Fatalf("title = %q, want none", got)
	}
}

func TestChatMessagesShowsResolvedNames(t *testing.T) {
	var out bytes.Buffer
	cmd := New().Command(testDeps(t, chatServer(t), "json", true, &out))
	cmd.SetArgs([]string{"messages", "--space", "spaces/A"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"Linh Tran"`) {
		t.Fatalf("sender name was not resolved:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"linh@example.com"`) {
		t.Fatalf("sender email was not resolved:\n%s", out.String())
	}
}
