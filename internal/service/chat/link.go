// Package chat implements the Google Chat service commands and API client.
//
// Only this file knows how Chat resource names map to browser URLs. Two shapes
// were verified with Chat UI's "Copy link":
//
//	space   https://chat.google.com/u/0/app/chat/AAQAU1dQC8k
//	message https://chat.google.com/room/AAQAlS_sfCg/qeQhBvDA5Os/yr7GQKDADuw
//
// Compare the message URL against the resource names it came from:
//
//	thread.name  = spaces/AAQAlS_sfCg/threads/qeQhBvDA5Os
//	message.name = spaces/AAQAlS_sfCg/messages/qeQhBvDA5Os.yr7GQKDADuw
//
// The final segment of message.name is {threadID}.{messageID} and the dot
// becomes a slash in the URL — which also gives us the thread-head rule: the
// first message of a thread has messageID == threadID.
//
// None of this is an API contract. If Google changes it, this file is the only
// one that has to change, and JSON output still carries raw IDs at all three
// levels so nothing is lost.
package chat

import (
	"fmt"
	"net/url"
	"strings"
)

const chatBaseURL = "https://chat.google.com"

// splitSpaceName splits "spaces/{sid}".
func splitSpaceName(name string) (sid string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] != "spaces" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// splitThreadName splits "spaces/{sid}/threads/{tid}".
func splitThreadName(name string) (sid, tid string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "spaces" || parts[2] != "threads" || parts[1] == "" || parts[3] == "" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// splitMessageName splits "spaces/{sid}/messages/{tid}.{mid}". A message name
// whose last segment has no dot does not match the observed convention and is
// reported as unparseable rather than guessed at.
func splitMessageName(name string) (sid, tid, mid string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "spaces" || parts[2] != "messages" || parts[1] == "" {
		return "", "", "", false
	}
	t, m, found := strings.Cut(parts[3], ".")
	if !found || t == "" || m == "" {
		return "", "", "", false
	}
	return parts[1], t, m, true
}

// isThreadHead reports whether a message is the first message of its thread.
// known reports whether messageName parsed as a message name at all; head is
// meaningful only when known is true. This two-value contract exists so a
// caller cannot conflate "genuinely a reply" with "name didn't parse" — see
// partial-thread detection in query.go.
func isThreadHead(messageName string) (head, known bool) {
	_, tid, mid, ok := splitMessageName(messageName)
	if !ok {
		return false, false
	}
	return tid == mid, true
}

// spaceLink returns a browser URL for a space. A spaceUri handed back by the
// API is trusted when it points at Chat; otherwise the verified /u/{idx}/
// pattern is used. accountIndex is the browser's signed-in-account index and
// defaults to "0" — it cannot be discovered from the API because it is browser
// state, not account state.
func spaceLink(spaceName, spaceURI, accountIndex string) string {
	if strings.HasPrefix(spaceURI, chatBaseURL+"/") {
		return spaceURI
	}
	sid, ok := splitSpaceName(spaceName)
	if !ok {
		return ""
	}
	if accountIndex == "" {
		accountIndex = "0"
	}
	return chatBaseURL + "/u/" + accountIndex + "/app/chat/" + sid
}

// threadLink returns a URL for a thread. Chat has no thread URL and no "copy
// thread link" action; opening a thread is opening its first message, whose
// messageID equals the threadID.
func threadLink(threadName string) string {
	sid, tid, ok := splitThreadName(threadName)
	if !ok {
		return ""
	}
	return chatBaseURL + "/room/" + sid + "/" + tid + "/" + tid
}

// messageLink returns a URL for a message, or "" if its name does not match the
// observed {tid}.{mid} convention.
func messageLink(messageName string) string {
	sid, tid, mid, ok := splitMessageName(messageName)
	if !ok {
		return ""
	}
	return chatBaseURL + "/room/" + sid + "/" + tid + "/" + mid
}

// shortID returns the last path segment of a resource name, for compact display.
func shortID(resourceName string) string {
	if i := strings.LastIndex(resourceName, "/"); i >= 0 {
		return resourceName[i+1:]
	}
	return resourceName
}

// formatMessageID converts "spaces/{sid}/messages/{tid}.{mid}" into "{sid}/{tid}/{mid}".
// If unparseable, it returns the original string.
func formatMessageID(resourceName string) string {
	sid, tid, mid, ok := splitMessageName(resourceName)
	if !ok {
		return resourceName
	}
	return sid + "/" + tid + "/" + mid
}

// qualifyMessage normalizes various input forms into a canonical resource name
// "spaces/{sid}/messages/{tid}.{mid}".
//
// Supported input forms:
// 1. "AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY" -> "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY"
// 2. "https://chat.google.com/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY" -> "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY"
// 3. "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY" -> as-is
// 4. "t-uT1uhCWAg.emH4eHFJkeY" or "t-uT1uhCWAg/emH4eHFJkeY" with spaceFlag="AAAA9GOspFY"
func qualifyMessage(ref, spaceFlag string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("message ID cannot be empty")
	}

	// 1. Full resource name: spaces/{sid}/messages/{tid}.{mid}
	if strings.HasPrefix(ref, "spaces/") {
		if _, _, _, ok := splitMessageName(ref); ok {
			return ref, nil
		}
		return "", fmt.Errorf("invalid message resource name %q: expected format spaces/{spaceId}/messages/{threadId}.{messageId}", ref)
	}

	// 2. Web URL: https://chat.google.com/...
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		u, err := url.Parse(ref)
		if err != nil {
			return "", fmt.Errorf("invalid message URL %q: %w", ref, err)
		}
		parts := strings.FieldsFunc(strings.Trim(u.Path, "/"), func(r rune) bool { return r == '/' })
		// Find "room" segment in URL path: .../room/{sid}/{tid}/{mid}
		for i, part := range parts {
			if part == "room" && len(parts) >= i+4 {
				sid, tid, mid := parts[i+1], parts[i+2], parts[i+3]
				return fmt.Sprintf("spaces/%s/messages/%s.%s", sid, tid, mid), nil
			}
		}
		return "", fmt.Errorf("invalid chat message URL %q: expected path containing /room/{spaceId}/{threadId}/{messageId}", ref)
	}

	// 3. 3-segment format: sid/tid/mid
	parts := strings.Split(ref, "/")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		return fmt.Sprintf("spaces/%s/messages/%s.%s", parts[0], parts[1], parts[2]), nil
	}

	// 4. Short form with space flag
	if spaceFlag != "" {
		sid := spaceFlag
		if s, ok := splitSpaceName(spaceFlag); ok {
			sid = s
		}
		sid = strings.TrimPrefix(sid, "spaces/")

		// Could be "tid.mid" or "tid/mid"
		if strings.Contains(ref, ".") {
			t, m, found := strings.Cut(ref, ".")
			if found && t != "" && m != "" {
				return fmt.Sprintf("spaces/%s/messages/%s.%s", sid, t, m), nil
			}
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return fmt.Sprintf("spaces/%s/messages/%s.%s", sid, parts[0], parts[1]), nil
		}
	}

	return "", fmt.Errorf("invalid message ID %q: expected format spaceId/threadId/messageId, URL, spaces/{spaceId}/messages/{threadId}.{messageId}, or threadId.messageId with --space", ref)
}

