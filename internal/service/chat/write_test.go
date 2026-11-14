package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

func TestClientCreateMessage(t *testing.T) {
	var gotMethod, gotPath, gotReplyOpt string
	var gotBody map[string]any

	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotReplyOpt = r.URL.Query().Get("messageReplyOption")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":       "spaces/A/messages/t1.m2",
			"text":       gotBody["text"],
			"createTime": "2026-11-14T10:00:00Z",
			"thread":     map[string]any{"name": "spaces/A/threads/t1"},
		})
	})

	msg := &chatapi.Message{
		Text:   "*hello*",
		Thread: &chatapi.Thread{Name: "spaces/A/threads/t1"},
	}
	created, err := cl.CreateMessage(context.Background(), "spaces/A", msg, "REPLY_MESSAGE_OR_FAIL")
	if err != nil {
		t.Fatalf("CreateMessage error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/spaces/A/messages" {
		t.Fatalf("request = %s %s, want POST /v1/spaces/A/messages", gotMethod, gotPath)
	}
	if gotReplyOpt != "REPLY_MESSAGE_OR_FAIL" {
		t.Fatalf("messageReplyOption = %q, want REPLY_MESSAGE_OR_FAIL", gotReplyOpt)
	}
	if gotBody["text"] != "*hello*" {
		t.Fatalf("body text = %v, want *hello*", gotBody["text"])
	}
	if created.Name != "spaces/A/messages/t1.m2" {
		t.Fatalf("created.Name = %q", created.Name)
	}
}

func TestClientPatchMessage(t *testing.T) {
	var gotMethod, gotPath, gotMask string
	var gotBody map[string]any

	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotMask = r.URL.Query().Get("updateMask")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":       "spaces/A/messages/t1.m1",
			"text":       gotBody["text"],
			"createTime": "2026-11-14T10:00:00Z",
		})
	})

	updated, err := cl.PatchMessage(context.Background(), "spaces/A/messages/t1.m1", "edited body")
	if err != nil {
		t.Fatalf("PatchMessage error: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/v1/spaces/A/messages/t1.m1" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotMask != "text" {
		t.Fatalf("updateMask = %q, want text", gotMask)
	}
	if gotBody["text"] != "edited body" {
		t.Fatalf("body text = %v, want edited body", gotBody["text"])
	}
	if updated.Text != "edited body" {
		t.Fatalf("updated.Text = %q", updated.Text)
	}
}

func TestClientDeleteMessage(t *testing.T) {
	var gotMethod, gotPath string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})

	if err := cl.DeleteMessage(context.Background(), "spaces/A/messages/t1.m1"); err != nil {
		t.Fatalf("DeleteMessage error: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/spaces/A/messages/t1.m1" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
}

func TestClientUploadAttachment(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(subDir, "report.txt")
	if err := os.WriteFile(filePath, []byte("attachment-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotMethod, gotPath, gotUploadType, gotPayload string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotUploadType = r.URL.Query().Get("uploadType")
		b, _ := io.ReadAll(r.Body)
		gotPayload = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attachmentDataRef": map[string]any{
				"resourceName": "spaces/A/attachments/att123",
			},
		})
	})

	ref, err := cl.UploadAttachment(context.Background(), "spaces/A", filePath)
	if err != nil {
		t.Fatalf("UploadAttachment error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/upload/v1/spaces/A/attachments:upload" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotUploadType != "multipart" {
		t.Fatalf("uploadType = %q, want multipart", gotUploadType)
	}
	if !strings.Contains(gotPayload, `"filename":"report.txt"`) {
		t.Fatalf("multipart payload missing sanitized filename: %s", gotPayload)
	}
	if strings.Contains(gotPayload, "nested") {
		t.Fatalf("multipart payload leaked local directory path: %s", gotPayload)
	}
	if !strings.Contains(gotPayload, "attachment-payload") {
		t.Fatalf("multipart payload missing file bytes: %s", gotPayload)
	}
	if ref == nil || ref.ResourceName != "spaces/A/attachments/att123" {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestClientReactions(t *testing.T) {
	var listPages int
	var gotFilter, gotCreateEmoji, gotDeletePath string

	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/spaces/A/messages/t1.m1/reactions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			emoji := body["emoji"].(map[string]any)
			gotCreateEmoji = emoji["unicode"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":  "spaces/A/messages/t1.m1/reactions/r1",
				"emoji": map[string]any{"unicode": gotCreateEmoji},
				"user":  map[string]any{"name": "users/100"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/spaces/A/messages/t1.m1/reactions":
			listPages++
			gotFilter = r.URL.Query().Get("filter")
			if r.URL.Query().Get("pageToken") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"reactions": []map[string]any{{
						"name":  "spaces/A/messages/t1.m1/reactions/r_other",
						"emoji": map[string]any{"unicode": "👍"},
						"user":  map[string]any{"name": "users/999"},
					}},
					"nextPageToken": "p2",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reactions": []map[string]any{{
					"name":  "spaces/A/messages/t1.m1/reactions/r_me",
					"emoji": map[string]any{"unicode": "👍"},
					"user":  map[string]any{"name": "users/100"},
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/spaces/A/messages/t1.m1/reactions/r_me":
			gotDeletePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	})

	created, err := cl.CreateReaction(context.Background(), "spaces/A/messages/t1.m1", "👍")
	if err != nil {
		t.Fatalf("CreateReaction error: %v", err)
	}
	if gotCreateEmoji != "👍" || created.Name != "spaces/A/messages/t1.m1/reactions/r1" {
		t.Fatalf("CreateReaction = %+v (emoji=%q)", created, gotCreateEmoji)
	}

	list, err := cl.ListReactions(context.Background(), "spaces/A/messages/t1.m1", `emoji.unicode = "👍"`)
	if err != nil {
		t.Fatalf("ListReactions error: %v", err)
	}
	if listPages != 2 || len(list) != 2 || gotFilter != `emoji.unicode = "👍"` {
		t.Fatalf("ListReactions pages=%d len=%d filter=%q", listPages, len(list), gotFilter)
	}

	if err := cl.DeleteReaction(context.Background(), "spaces/A/messages/t1.m1/reactions/r_me"); err != nil {
		t.Fatalf("DeleteReaction error: %v", err)
	}
	if gotDeletePath != "/v1/spaces/A/messages/t1.m1/reactions/r_me" {
		t.Fatalf("DeleteReaction path = %q", gotDeletePath)
	}
}

type fakeWriteAPI struct {
	spaces        []*chatapi.Space
	dmSpaces      map[string]*chatapi.Space
	messages      map[string]*chatapi.Message
	readStateName string
	reactions     []*chatapi.Reaction

	uploadedFiles   []string
	createdMessages []struct {
		parent      string
		msg         *chatapi.Message
		replyOption string
	}
	patchedMessages []struct {
		name string
		text string
	}
	deletedMessages  []string
	createdReactions []struct {
		parent  string
		unicode string
	}
	deletedReactions []string
	lastReactionList struct {
		parent string
		filter string
	}
}

func (f *fakeWriteAPI) GetSpace(_ context.Context, name string) (*chatapi.Space, error) {
	for _, s := range f.spaces {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *fakeWriteAPI) FindDirectMessage(_ context.Context, userRef string) (*chatapi.Space, error) {
	if sp, ok := f.dmSpaces[userRef]; ok {
		return sp, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeWriteAPI) ListSpaces(_ context.Context) ([]*chatapi.Space, error) {
	return f.spaces, nil
}

func (f *fakeWriteAPI) GetMessage(_ context.Context, name string) (*chatapi.Message, error) {
	if m, ok := f.messages[name]; ok {
		return m, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeWriteAPI) SpaceReadState(_ context.Context, _ string) (string, time.Time, error) {
	return f.readStateName, time.Time{}, nil
}

func (f *fakeWriteAPI) CreateMessage(_ context.Context, parent string, msg *chatapi.Message, replyOption string) (*chatapi.Message, error) {
	f.createdMessages = append(f.createdMessages, struct {
		parent      string
		msg         *chatapi.Message
		replyOption string
	}{parent, msg, replyOption})
	thread := msg.Thread
	if thread == nil {
		thread = &chatapi.Thread{Name: parent + "/threads/tnew"}
	}
	return &chatapi.Message{
		Name:       parent + "/messages/tnew.mnew",
		Text:       msg.Text,
		CreateTime: "2026-11-14T10:00:00Z",
		Thread:     thread,
		Sender:     &chatapi.User{Name: "users/100", Type: "HUMAN"},
		Attachment: msg.Attachment,
	}, nil
}

func (f *fakeWriteAPI) PatchMessage(_ context.Context, name, text string) (*chatapi.Message, error) {
	f.patchedMessages = append(f.patchedMessages, struct {
		name string
		text string
	}{name, text})
	return &chatapi.Message{
		Name:       name,
		Text:       text,
		CreateTime: "2026-11-14T10:00:00Z",
		Sender:     &chatapi.User{Name: "users/100", Type: "HUMAN"},
	}, nil
}

func (f *fakeWriteAPI) DeleteMessage(_ context.Context, name string) error {
	f.deletedMessages = append(f.deletedMessages, name)
	return nil
}

func (f *fakeWriteAPI) UploadAttachment(_ context.Context, parent, filePath string) (*chatapi.AttachmentDataRef, error) {
	f.uploadedFiles = append(f.uploadedFiles, filePath)
	return &chatapi.AttachmentDataRef{ResourceName: parent + "/attachments/" + filepath.Base(filePath)}, nil
}

func (f *fakeWriteAPI) CreateReaction(_ context.Context, parent, unicode string) (*chatapi.Reaction, error) {
	f.createdReactions = append(f.createdReactions, struct {
		parent  string
		unicode string
	}{parent, unicode})
	return &chatapi.Reaction{
		Name:  parent + "/reactions/rnew",
		Emoji: &chatapi.Emoji{Unicode: unicode},
	}, nil
}

func (f *fakeWriteAPI) ListReactions(_ context.Context, parent, filter string) ([]*chatapi.Reaction, error) {
	f.lastReactionList = struct {
		parent string
		filter string
	}{parent, filter}
	return f.reactions, nil
}

func (f *fakeWriteAPI) DeleteReaction(_ context.Context, name string) error {
	f.deletedReactions = append(f.deletedReactions, name)
	return nil
}

func TestWriterSendTargetingAndText(t *testing.T) {
	ctx := context.Background()

	t.Run("space by ID preserves verbatim Chat markup", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}}}
		w := NewWriter(api, nil)
		raw := "*bold* _italic_ `code` <https://example.com|link> <users/all>"
		sm, err := w.Send(ctx, SendOpts{Space: "spaces/A", Text: raw})
		if err != nil {
			t.Fatalf("Send error: %v", err)
		}
		if len(api.createdMessages) != 1 || api.createdMessages[0].parent != "spaces/A" {
			t.Fatalf("createdMessages = %+v", api.createdMessages)
		}
		if api.createdMessages[0].replyOption != "" {
			t.Fatalf("replyOption = %q, want empty", api.createdMessages[0].replyOption)
		}
		if api.createdMessages[0].msg.Text != raw {
			t.Fatalf("sent text = %q, want verbatim %q", api.createdMessages[0].msg.Text, raw)
		}
		if sm.Message.Text != raw {
			t.Fatalf("MessageInfo.Text = %q", sm.Message.Text)
		}
	})

	t.Run("space by display name and DM email", func(t *testing.T) {
		api := &fakeWriteAPI{
			spaces:   []*chatapi.Space{{Name: "spaces/TEAM", DisplayName: "Backend Team"}},
			dmSpaces: map[string]*chatapi.Space{"users/linh@example.com": {Name: "spaces/DM1"}},
		}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{Space: "Backend Team", Text: "hi team"}); err != nil {
			t.Fatalf("Send display name error: %v", err)
		}
		if _, err := w.Send(ctx, SendOpts{Space: "linh@example.com", Text: "hi linh"}); err != nil {
			t.Fatalf("Send DM email error: %v", err)
		}
		if api.createdMessages[0].parent != "spaces/TEAM" || api.createdMessages[1].parent != "spaces/DM1" {
			t.Fatalf("parents = %s, %s", api.createdMessages[0].parent, api.createdMessages[1].parent)
		}
	})

	t.Run("thread short ID with space, full resource name, and URL", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}}}
		w := NewWriter(api, nil)

		if _, err := w.Send(ctx, SendOpts{Space: "Alpha", Thread: "t1", Text: "short"}); err != nil {
			t.Fatalf("short thread error: %v", err)
		}
		if _, err := w.Send(ctx, SendOpts{Thread: "spaces/A/threads/t2", Text: "full"}); err != nil {
			t.Fatalf("full thread error: %v", err)
		}
		if _, err := w.Send(ctx, SendOpts{Thread: "https://chat.google.com/room/A/t3/m9", Text: "url"}); err != nil {
			t.Fatalf("url thread error: %v", err)
		}

		if got := api.createdMessages[0]; got.parent != "spaces/A" || got.msg.Thread.Name != "spaces/A/threads/t1" || got.replyOption != "REPLY_MESSAGE_OR_FAIL" {
			t.Fatalf("short thread sent = %+v", got)
		}
		if got := api.createdMessages[1]; got.parent != "spaces/A" || got.msg.Thread.Name != "spaces/A/threads/t2" {
			t.Fatalf("full thread sent = %+v", got)
		}
		if got := api.createdMessages[2]; got.parent != "spaces/A" || got.msg.Thread.Name != "spaces/A/threads/t3" {
			t.Fatalf("url thread sent = %+v", got)
		}
	})

	t.Run("reply-to derives space and thread", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}}}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{ReplyTo: "t1.m5", Space: "Alpha", Text: "reply"}); err != nil {
			t.Fatalf("Send --reply-to error: %v", err)
		}
		got := api.createdMessages[0]
		if got.parent != "spaces/A" || got.msg.Thread.Name != "spaces/A/threads/t1" || got.replyOption != "REPLY_MESSAGE_OR_FAIL" {
			t.Fatalf("reply-to sent = %+v", got)
		}
	})

	t.Run("reply-to falls back to GetMessage for non-standard message name", func(t *testing.T) {
		api := &fakeWriteAPI{
			messages: map[string]*chatapi.Message{
				"spaces/A/messages/custom-id": {
					Name:   "spaces/A/messages/custom-id",
					Space:  &chatapi.Space{Name: "spaces/A"},
					Thread: &chatapi.Thread{Name: "spaces/A/threads/t77"},
				},
			},
		}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{ReplyTo: "spaces/A/messages/custom-id", Text: "reply custom"}); err != nil {
			t.Fatalf("Send custom reply-to error: %v", err)
		}
		got := api.createdMessages[0]
		if got.parent != "spaces/A" || got.msg.Thread.Name != "spaces/A/threads/t77" || got.replyOption != "REPLY_MESSAGE_OR_FAIL" {
			t.Fatalf("custom reply-to sent = %+v", got)
		}
	})

	t.Run("conflict checks between reply-to, thread, and space", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		}}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{ReplyTo: "A/t1/m1", Space: "Beta", Text: "x"}); err == nil {
			t.Fatal("expected conflict error for --reply-to vs --space")
		}
		if _, err := w.Send(ctx, SendOpts{ReplyTo: "A/t1/m1", Thread: "spaces/A/threads/t2", Text: "x"}); err == nil {
			t.Fatal("expected conflict error for --reply-to vs --thread")
		}
		if _, err := w.Send(ctx, SendOpts{Space: "Beta", Thread: "spaces/A/threads/t1", Text: "x"}); err == nil {
			t.Fatal("expected conflict error for --space vs --thread")
		}
	})

	t.Run("empty text rejected unless attachment provided", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A"}}}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{Space: "spaces/A", Text: "   "}); err == nil {
			t.Fatal("expected error for whitespace-only text without attachments")
		}

		f := filepath.Join(t.TempDir(), "pic.png")
		if err := os.WriteFile(f, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		var prog strings.Builder
		sm, err := w.Send(ctx, SendOpts{Space: "spaces/A", Attachments: []string{f}, Progress: &prog})
		if err != nil {
			t.Fatalf("Send attachment-only error: %v", err)
		}
		if len(api.uploadedFiles) != 1 || len(sm.Message.Attachments) != 1 {
			t.Fatalf("uploadedFiles=%v attachments=%+v", api.uploadedFiles, sm.Message.Attachments)
		}
		if len(api.createdMessages) != 1 || len(api.createdMessages[0].msg.Attachment) != 1 {
			t.Fatalf("attachments not attached to CreateMessage: %+v", api.createdMessages)
		}
		if !strings.Contains(prog.String(), "Uploading pic.png...") {
			t.Fatalf("progress = %q", prog.String())
		}
	})

	t.Run("missing or directory attachment fails fast before API calls", func(t *testing.T) {
		api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A"}}}
		w := NewWriter(api, nil)
		if _, err := w.Send(ctx, SendOpts{Space: "spaces/A", Text: "hi", Attachments: []string{t.TempDir()}}); err == nil {
			t.Fatal("expected error when attachment is a directory")
		}
		if len(api.uploadedFiles) != 0 || len(api.createdMessages) != 0 {
			t.Fatal("API must not be called when attachment validation fails")
		}
	})
}

func TestWriterEdit(t *testing.T) {
	ctx := context.Background()
	api := &fakeWriteAPI{spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}}}
	w := NewWriter(api, nil)

	if _, err := w.Edit(ctx, "A/t1/m1", "", "   "); err == nil {
		t.Fatal("expected error for empty edit text")
	}

	sm, err := w.Edit(ctx, "t1.m1", "Alpha", "updated text")
	if err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	if len(api.patchedMessages) != 1 || api.patchedMessages[0].name != "spaces/A/messages/t1.m1" || api.patchedMessages[0].text != "updated text" {
		t.Fatalf("patched = %+v", api.patchedMessages)
	}
	if sm.Message.ID != "A/t1/m1" || sm.Message.Text != "updated text" {
		t.Fatalf("sm = %+v", sm.Message)
	}
}

func TestWriterDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("TTY y proceeds", func(t *testing.T) {
		api := &fakeWriteAPI{}
		w := NewWriter(api, nil)
		var errOut strings.Builder
		res, err := w.Delete(ctx, "A/t1/m1", DeleteOpts{
			Interactive: true,
			In:          strings.NewReader("y\n"),
			ErrOut:      &errOut,
		})
		if err != nil {
			t.Fatalf("Delete error: %v", err)
		}
		if !strings.Contains(errOut.String(), "Delete message A/t1/m1 (https://chat.google.com/room/A/t1/m1)? [y/N]: ") {
			t.Fatalf("prompt = %q", errOut.String())
		}
		if len(api.deletedMessages) != 1 || !res.Deleted || res.ID != "A/t1/m1" {
			t.Fatalf("deleted = %v, res = %+v", api.deletedMessages, res)
		}
	})

	t.Run("TTY n or empty aborts without API call", func(t *testing.T) {
		for _, ans := range []string{"n\n", "\n"} {
			api := &fakeWriteAPI{}
			w := NewWriter(api, nil)
			var errOut strings.Builder
			_, err := w.Delete(ctx, "A/t1/m1", DeleteOpts{
				Interactive: true,
				In:          strings.NewReader(ans),
				ErrOut:      &errOut,
			})
			if err == nil || err.Error() != "aborted" {
				t.Fatalf("answer %q: err = %v, want aborted", ans, err)
			}
			if len(api.deletedMessages) != 0 {
				t.Fatalf("answer %q: DeleteMessage should not be called", ans)
			}
		}
	})

	t.Run("--yes skips prompt even when non-interactive", func(t *testing.T) {
		api := &fakeWriteAPI{}
		w := NewWriter(api, nil)
		var errOut strings.Builder
		res, err := w.Delete(ctx, "A/t1/m1", DeleteOpts{
			Yes:         true,
			Interactive: false,
			ErrOut:      &errOut,
		})
		if err != nil {
			t.Fatalf("Delete --yes error: %v", err)
		}
		if errOut.Len() != 0 || !res.Deleted || len(api.deletedMessages) != 1 {
			t.Fatalf("unexpected behavior: errOut=%q res=%+v deleted=%v", errOut.String(), res, api.deletedMessages)
		}
	})

	t.Run("non-TTY without --yes fails fast", func(t *testing.T) {
		api := &fakeWriteAPI{}
		w := NewWriter(api, nil)
		_, err := w.Delete(ctx, "A/t1/m1", DeleteOpts{Interactive: false})
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("err = %v, want --yes requirement error", err)
		}
		if len(api.deletedMessages) != 0 {
			t.Fatal("DeleteMessage must not be called")
		}
	})
}

func TestWriterReact(t *testing.T) {
	ctx := context.Background()

	t.Run("add reaction", func(t *testing.T) {
		api := &fakeWriteAPI{}
		w := NewWriter(api, nil)
		res, err := w.React(ctx, "A/t1/m1", "👍", ReactOpts{})
		if err != nil {
			t.Fatalf("React add error: %v", err)
		}
		if res.Action != "added" || res.Emoji != "👍" || res.MessageID != "A/t1/m1" || res.Name != "spaces/A/messages/t1.m1/reactions/rnew" {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("remove finds caller own reaction and ignores others", func(t *testing.T) {
		api := &fakeWriteAPI{
			readStateName: "users/100/spaces/A/spaceReadState",
			reactions: []*chatapi.Reaction{
				{Name: "spaces/A/messages/t1.m1/reactions/r_other", Emoji: &chatapi.Emoji{Unicode: "👍"}, User: &chatapi.User{Name: "users/999"}},
				{Name: "spaces/A/messages/t1.m1/reactions/r_me", Emoji: &chatapi.Emoji{Unicode: "👍"}, User: &chatapi.User{Name: "users/100"}},
			},
		}
		w := NewWriter(api, nil)
		res, err := w.React(ctx, "A/t1/m1", "👍", ReactOpts{Remove: true})
		if err != nil {
			t.Fatalf("React --remove error: %v", err)
		}
		if api.lastReactionList.filter != `emoji.unicode = "👍"` {
			t.Fatalf("filter = %q", api.lastReactionList.filter)
		}
		if len(api.deletedReactions) != 1 || api.deletedReactions[0] != "spaces/A/messages/t1.m1/reactions/r_me" {
			t.Fatalf("deletedReactions = %v", api.deletedReactions)
		}
		if res.Action != "removed" || res.Name != "spaces/A/messages/t1.m1/reactions/r_me" {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("remove returns friendly error when caller has no matching reaction", func(t *testing.T) {
		api := &fakeWriteAPI{
			readStateName: "users/100/spaces/A/spaceReadState",
			reactions: []*chatapi.Reaction{
				{Name: "spaces/A/messages/t1.m1/reactions/r_other", Emoji: &chatapi.Emoji{Unicode: "👍"}, User: &chatapi.User{Name: "users/999"}},
			},
		}
		w := NewWriter(api, nil)
		_, err := w.React(ctx, "A/t1/m1", "👍", ReactOpts{Remove: true})
		if err == nil || !strings.Contains(err.Error(), "no 👍 reaction from you found on message A/t1/m1") {
			t.Fatalf("err = %v", err)
		}
		if len(api.deletedReactions) != 0 {
			t.Fatal("DeleteReaction must not be called")
		}
	})
}
