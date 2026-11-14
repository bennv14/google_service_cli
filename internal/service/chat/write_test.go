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
