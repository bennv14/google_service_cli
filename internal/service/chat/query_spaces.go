package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"

	chatapi "google.golang.org/api/chat/v1"
)

// spaceTypeAliases maps the --type flag's values onto Space.spaceType.
var spaceTypeAliases = map[string]string{
	"space": "SPACE",
	"dm":    "DIRECT_MESSAGE",
	"group": "GROUP_CHAT",
}

// matchesSpaceType reports whether sp passes the --type filter. No filter
// matches everything.
func matchesSpaceType(sp *chatapi.Space, types []string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if spaceTypeAliases[strings.ToLower(t)] == sp.SpaceType {
			return true
		}
	}
	return false
}

type spaceResolver interface {
	GetSpace(ctx context.Context, name string) (*chatapi.Space, error)
	FindDirectMessage(ctx context.Context, userRef string) (*chatapi.Space, error)
	ListSpaces(ctx context.Context) ([]*chatapi.Space, error)
}

func (e *Engine) resolveSpace(ctx context.Context, ref string) (*chatapi.Space, error) {
	return resolveSpace(ctx, e.api, ref)
}

// resolveSpace turns a user-supplied --space value into a Space. A raw
// "spaces/…" ID is used directly; a value containing "@" is treated as a user
// and resolved to the DM with them; anything else is matched against display
// names. Nothing is cached: every command re-resolves, which costs one extra
// call but never shows a stale answer.
func resolveSpace(ctx context.Context, api spaceResolver, ref string) (*chatapi.Space, error) {
	switch {
	case strings.HasPrefix(ref, "spaces/"):
		return api.GetSpace(ctx, ref)
	case strings.Contains(ref, "@"):
		return api.FindDirectMessage(ctx, "users/"+ref)
	}

	spaces, err := api.ListSpaces(ctx)
	if err != nil {
		return nil, err
	}
	var exact, partial []*chatapi.Space
	lower := strings.ToLower(ref)
	for _, sp := range spaces {
		name := strings.ToLower(sp.DisplayName)
		switch {
		case name == lower:
			exact = append(exact, sp)
		case name != "" && strings.Contains(name, lower):
			partial = append(partial, sp)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return nil, fmt.Errorf("no space matches %q (try the space ID, or an email for a DM)", ref)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d spaces; use one of these IDs:", ref, len(candidates))
		for _, sp := range candidates {
			fmt.Fprintf(&b, "\n  %s  (%s)", sp.DisplayName, sp.Name)
		}
		return nil, fmt.Errorf("%s", b.String())
	}
}

// selectSpaces resolves --space, or lists every space and applies --type.
func (e *Engine) selectSpaces(ctx context.Context, q Query) ([]*chatapi.Space, error) {
	if q.Space != "" {
		sp, err := e.resolveSpace(ctx, q.Space)
		if err != nil {
			return nil, err
		}
		return []*chatapi.Space{sp}, nil
	}
	all, err := e.api.ListSpaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*chatapi.Space, 0, len(all))
	for _, sp := range all {
		if matchesSpaceType(sp, q.SpaceTypes) {
			out = append(out, sp)
		}
	}
	return out, nil
}

// spaceProbeMessages is how deep the DM-naming probe reads. It is more than one
// because the newest message in a DM you spoke in last is your own, and naming
// the space after yourself is worse than not naming it at all.
const spaceProbeMessages = 5

// spaceNames names the spaces the API left nameless — DMs and group chats never
// carry a displayName. No endpoint answers this, so the other party is inferred
// from the space's newest messages: one request per nameless space, which on a
// 333-space account is the difference between 0.7s and 3s. The answer is
// therefore remembered under the space's own resource name, and later runs skip
// the probe entirely.
func (e *Engine) spaceNames(ctx context.Context, spaces []*chatapi.Space) map[string]string {
	cache, _ := e.dir.(nameCache)
	out := map[string]string{}
	var probe []*chatapi.Space
	for _, sp := range spaces {
		if sp.DisplayName != "" {
			continue
		}
		if cache != nil {
			if p, ok := cache.Recall(sp.Name); ok {
				out[sp.Name] = p.Name
				continue
			}
		}
		probe = append(probe, sp)
	}
	if len(probe) == 0 {
		return out
	}

	// Our own ID is what keeps a DM from being named after us. Failing to learn
	// it only risks a wrong-looking name, so it is not worth failing over.
	meID, _ := e.userID(ctx, probe[0].Name)

	senders := e.probeSenders(ctx, probe, meID)
	ids := make([]string, 0, len(senders))
	seen := map[string]bool{}
	for _, id := range senders {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out
	}
	people, err := e.dir.Lookup(ctx, ids)
	if err != nil {
		e.warn(nameWarning(err))
	}
	for spaceName, senderID := range senders {
		p, ok := people[senderID]
		if !ok {
			continue
		}
		out[spaceName] = p.Name
		if cache != nil {
			cache.Remember(spaceName, Person{Name: p.Name})
		}
	}
	return out
}

// probeSenders finds the other party in each space by reading its newest
// messages. A space we cannot read simply goes unnamed.
func (e *Engine) probeSenders(ctx context.Context, spaces []*chatapi.Space, meID string) map[string]string {
	out := make(map[string]string, len(spaces))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSpaces)
launch:
	for _, sp := range spaces {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(sp *chatapi.Space) {
			defer wg.Done()
			defer func() { <-sem }()

			msgs, err := e.api.LatestMessages(ctx, sp.Name, spaceProbeMessages)
			if err != nil {
				return
			}
			for _, m := range msgs {
				if m.Sender == nil || m.Sender.Name == "" || m.Sender.Name == meID {
					continue
				}
				mu.Lock()
				out[sp.Name] = m.Sender.Name
				mu.Unlock()
				return
			}
		}(sp)
	}
	wg.Wait()
	return out
}

// Spaces lists the spaces the caller belongs to. Without UnreadOnly this is a
// single spaces.list call and every space is returned, including quiet ones.
// With UnreadOnly it runs a full scan and keeps only spaces that actually have
// unread messages, annotated with the count.
func (e *Engine) Spaces(ctx context.Context, q Query) (SpaceList, error) {
	if q.UnreadOnly {
		q.skipThreadTitles = true // this output has no thread level
		res, err := e.Run(ctx, q)
		if err != nil {
			return SpaceList{}, err
		}
		var out SpaceList
		out.Warnings = res.Warnings
		for _, sg := range res.Spaces {
			if sg.Space.UnreadCount == 0 {
				continue
			}
			out.Spaces = append(out.Spaces, sg.Space)
		}
		return out, nil
	}

	spaces, err := e.selectSpaces(ctx, q)
	if err != nil {
		return SpaceList{}, err
	}
	names := e.spaceNames(ctx, spaces)
	out := SpaceList{Spaces: make([]SpaceInfo, 0, len(spaces))}
	for _, sp := range spaces {
		name := sp.DisplayName
		if name == "" {
			name = names[sp.Name]
		}
		if name == "" {
			name = shortID(sp.Name) // nothing to infer a name from; show the ID
		}
		out.Spaces = append(out.Spaces, SpaceInfo{
			ID:        sp.Name,
			Name:      name,
			Type:      sp.SpaceType,
			Threading: sp.SpaceThreadingState,
			Link:      spaceLink(sp.Name, sp.SpaceUri, q.AccountIndex),
		})
	}
	out.Warnings = e.warnings()
	e.saveNames()
	return out, nil
}
