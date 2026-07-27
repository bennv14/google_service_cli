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

import "strings"

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
