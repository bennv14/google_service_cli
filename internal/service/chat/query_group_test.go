package chat

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	chatapi "google.golang.org/api/chat/v1"
)

func TestLimitCutsNewestFirstAcrossAllSpaces(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			switch parent {
			case "spaces/A":
				return []*chatapi.Message{
					rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "a-old", "2026-07-20T00:00:00Z"),
					rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "a-new", "2026-07-25T00:00:00Z"),
				}, nil
			default:
				return []*chatapi.Message{
					rawMsg("spaces/B/messages/t3.t3", "spaces/B/threads/t3", "users/2", "Huy", "b-newest", "2026-07-25T12:00:00Z"),
				}, nil
			}
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 2 {
		t.Fatalf("limit is a total across spaces, got %+v", res.Summary)
	}
	var texts []string
	for _, sg := range res.Spaces {
		for _, tg := range sg.Threads {
			for _, m := range tg.Messages {
				texts = append(texts, m.Text)
			}
		}
	}
	sort.Strings(texts)
	if len(texts) != 2 || texts[0] != "a-new" || texts[1] != "b-newest" {
		t.Fatalf("kept %v, want the two newest", texts)
	}
}

func TestThreadLimitCutsThreadsNotMessages(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "old-1", "2026-07-20T00:00:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "old-2", "2026-07-20T00:01:00Z"),
				rawMsg("spaces/A/messages/t2.t2", "spaces/A/threads/t2", "users/2", "Huy", "new-1", "2026-07-25T00:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{ThreadLimit: 1, Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Threads != 1 || res.Summary.Messages != 1 {
		t.Fatalf("summary = %+v", res.Summary)
	}
	if res.Spaces[0].Threads[0].Thread.ID != "spaces/A/threads/t2" {
		t.Fatalf("kept the wrong thread: %+v", res.Spaces[0].Threads[0].Thread)
	}
}

func TestSenderNamesAndEmailsComeFromTheDirectory(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran", Email: "linh@example.com"},
	}}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Spaces[0].Threads[0].Messages[0]
	if m.Sender.Name != "Linh Tran" || m.Sender.Email != "linh@example.com" {
		t.Fatalf("sender = %+v", m.Sender)
	}
	if m.Sender.ID != "users/1" {
		t.Fatalf("the raw ID must survive in Sender.ID, got %q", m.Sender.ID)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}
}

func TestUnresolvedSenderKeepsItsRawID(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/9", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, &fakeDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Sender.Name; got != "users/9" {
		t.Fatalf("sender name = %q, want the raw ID", got)
	}
}

// The lookup runs after --limit, so nobody who was cut is ever resolved.
func TestResolveNamesRunsAfterTheLimit(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "old", "2026-07-20T09:00:00Z"),
				rawMsg("spaces/A/messages/t2.t2", "spaces/A/threads/t2", "users/2", "", "new", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran"},
		"users/2": {Name: "Huy Nguyen"},
	}}
	if _, err := NewEngine(api, dir).Run(context.Background(), Query{Limit: 1, Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	if len(dir.asked) != 1 || dir.asked[0] != "users/2" {
		t.Fatalf("directory was asked for %v, want only the sender that survived --limit", dir.asked)
	}
}

func TestDirectoryIsAskedOnceForEachDistinctSender(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/A", DisplayName: "Alpha"},
			{Name: "spaces/B", DisplayName: "Beta"},
		},
		messages: func(parent string, _ ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg(parent+"/messages/t1.t1", parent+"/threads/t1", "users/1", "", "one", "2026-07-25T09:00:00Z"),
				rawMsg(parent+"/messages/t1.t2", parent+"/threads/t1", "users/1", "", "two", "2026-07-25T09:01:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	if _, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	if dir.calls != 1 || len(dir.asked) != 1 {
		t.Fatalf("directory calls=%d asked=%v, want one call for one distinct sender", dir.calls, dir.asked)
	}
}

func TestDMIsNamedAfterTheResolvedOtherParticipant(t *testing.T) {
	api := &fakeAPI{
		t: t,
		spaces: []*chatapi.Space{
			{Name: "spaces/DM1", SpaceType: "DIRECT_MESSAGE", SpaceThreadingState: "UNTHREADED_MESSAGES"},
		},
		readState: readStateFor(nil),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/DM1/messages/t1.t1", "spaces/DM1/threads/t1", meUser, "", "mine", "2026-07-25T09:00:00Z"),
				rawMsg("spaces/DM1/messages/t2.t2", "spaces/DM1/threads/t2", "users/1", "", "theirs", "2026-07-25T09:01:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{people: map[string]Person{
		meUser:    {Name: "Ben"},
		"users/1": {Name: "Linh Tran"},
	}}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Spaces[0].Space.Name != "Linh Tran" {
		t.Fatalf("DM name = %q, want the resolved other participant", res.Spaces[0].Space.Name)
	}
}

// A directory failure warns and leaves raw IDs; it never fails the read.
func TestDirectoryFailureWarnsAndKeepsTheResult(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	dir := &fakeDirectory{err: errors.New("403 ACCESS_TOKEN_SCOPE_INSUFFICIENT")}
	res, err := NewEngine(api, dir).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatalf("a directory failure must not fail the run: %v", err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("summary = %+v, want the message to have survived", res.Summary)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Sender.Name; got != "users/1" {
		t.Fatalf("sender name = %q, want the raw ID", got)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "gsvc auth login") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

// The degenerate case has to stay safe: no directory, no change in output.
func TestNullDirectoryLeavesTheOutputUnchanged(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "", "hello", "2026-07-25T09:00:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{Now: fixedTime(t, "2026-07-26T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Spaces[0].Threads[0].Messages[0]
	if m.Sender.Name != "users/1" || m.Sender.Email != "" {
		t.Fatalf("sender = %+v, want the untouched raw ID", m.Sender)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}
}

// A limit is a limit on messages the user asked for. Spending it on messages
// that the client-side filter then discards makes `chat mentions --limit 50`
// return nothing whenever the 50 newest messages happen not to mention you.
func TestMentionsSurviveTheMessageLimit(t *testing.T) {
	api := &fakeAPI{
		t:      t,
		spaces: []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "hi ben", "2026-07-25T09:00:00Z", meUser),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:01:00Z"),
				rawMsg("spaces/A/messages/t1.t3", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:02:00Z"),
				rawMsg("spaces/A/messages/t1.t4", "spaces/A/threads/t1", "users/1", "Linh", "chatter", "2026-07-25T09:03:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		MentionsMe: true, Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("the limit must be spent on mentions, not on messages that are filtered away: %+v", res.Summary)
	}
	if got := res.Spaces[0].Threads[0].Messages[0].Text; got != "hi ben" {
		t.Fatalf("message = %q, want %q", got, "hi ben")
	}
}

func TestUnreadSurvivesTheMessageLimit(t *testing.T) {
	api := &fakeAPI{
		t:         t,
		spaces:    []*chatapi.Space{{Name: "spaces/A", DisplayName: "Alpha"}},
		readState: readStateFor(map[string]string{"spaces/A": "2026-07-25T09:00:30Z"}),
		messages: func(string, ListOpts) ([]*chatapi.Message, error) {
			return []*chatapi.Message{
				rawMsg("spaces/A/messages/t1.t1", "spaces/A/threads/t1", "users/1", "Linh", "unread one", "2026-07-25T09:01:00Z"),
				rawMsg("spaces/A/messages/t1.t2", "spaces/A/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:02:00Z"),
				rawMsg("spaces/A/messages/t1.t3", "spaces/A/threads/t1", meUser, "Ben", "mine", "2026-07-25T09:03:00Z"),
			}, nil
		},
	}
	res, err := NewEngine(api, nullDirectory{}).Run(context.Background(), Query{
		UnreadOnly: true, Limit: 2, Now: fixedTime(t, "2026-07-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Messages != 1 {
		t.Fatalf("your own newer messages must not eat the --limit budget: %+v", res.Summary)
	}
}

func TestMessageInfoExtractsAttachments(t *testing.T) {
	m := &chatapi.Message{
		Name:       "spaces/AAAA/messages/t1.m1",
		CreateTime: "2026-08-19T12:00:00Z",
		Text:       "Here are files",
		Attachment: []*chatapi.Attachment{
			{
				Name:        "spaces/AAAA/messages/t1.m1/attachments/att1",
				ContentName: "spec.pdf",
				ContentType: "application/pdf",
				Source:      "UPLOADED_CONTENT",
				AttachmentDataRef: &chatapi.AttachmentDataRef{
					ResourceName: "spaces/AAAA/messages/t1.m1/attachments/att1/media",
				},
				DownloadUri: "https://chat.google.com/download/spec.pdf",
			},
			{
				Name:        "spaces/AAAA/messages/t1.m1/attachments/att2",
				ContentName: "sheet.xlsx",
				ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				Source:      "DRIVE_FILE",
				DriveDataRef: &chatapi.DriveDataRef{
					DriveFileId: "drive-file-123",
				},
			},
		},
	}

	mi, ok := messageInfo(m, time.Time{}, false, "")
	if !ok {
		t.Fatal("messageInfo returned ok=false")
	}
	if mi.ID != "AAAA/t1/m1" {
		t.Errorf("mi.ID = %q, want AAAA/t1/m1", mi.ID)
	}
	if mi.RawName != "spaces/AAAA/messages/t1.m1" {
		t.Errorf("mi.RawName = %q, want spaces/AAAA/messages/t1.m1", mi.RawName)
	}
	if len(mi.Attachments) != 2 {
		t.Fatalf("len(mi.Attachments) = %d, want 2", len(mi.Attachments))
	}
	att1 := mi.Attachments[0]
	if att1.ContentName != "spec.pdf" || att1.Source != "UPLOADED_CONTENT" || att1.ResourceName != "spaces/AAAA/messages/t1.m1/attachments/att1/media" {
		t.Errorf("unexpected att1: %+v", att1)
	}
	att2 := mi.Attachments[1]
	if att2.ContentName != "sheet.xlsx" || att2.Source != "DRIVE_FILE" || att2.DriveFileID != "drive-file-123" {
		t.Errorf("unexpected att2: %+v", att2)
	}
}
