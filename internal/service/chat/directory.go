package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"google.golang.org/api/option"
	peopleapi "google.golang.org/api/people/v1"

	"github.com/bennv14/google_service_cli/internal/gerr"
)

// Person is a Chat principal resolved to something a human can read.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"` // empty for a space
}

// Directory resolves Chat resource names into people. An ID it cannot resolve
// is absent from the result rather than an error: partial knowledge is the
// normal case, and a name we failed to learn must never fail a read command.
type Directory interface {
	Lookup(ctx context.Context, ids []string) (map[string]Person, error)
}

// nullDirectory resolves nothing. It exists so tests can pin down the degenerate
// case — raw IDs everywhere — as safe rather than broken. Production never
// selects it: whether the stored token carries directory.readonly cannot be
// known without asking, so the real directory is always built and a refusal is
// handled as a warning at call time.
type nullDirectory struct{}

func (nullDirectory) Lookup(context.Context, []string) (map[string]Person, error) {
	return nil, nil
}

// nameWarning phrases a directory failure for the user. The overwhelmingly
// likely cause is a token minted before directory.readonly was requested, so
// the message leads with that fix. The indent lines the second line up under
// the first, past the "warning: " prefix report() adds.
func nameWarning(err error) string {
	return fmt.Sprintf("cannot resolve sender names: %v\n         "+
		"run 'gsvc auth login' to enable sender names", gerr.Friendly(err))
}

const (
	// peopleBatchSize is well under the API's 200-name maximum because the names
	// travel in the query string, where 200 × ~32 characters overruns what is
	// safe for a URL. 50 is the size verified against the real API.
	peopleBatchSize = 50
	// maxConcurrentBatches caps the fan-out for the same reason
	// maxConcurrentSpaces does: more than this invites 429s.
	maxConcurrentBatches = 4
	// personFields is the smallest mask that answers "who is this?".
	personFields = "names,emailAddresses"
)

// peopleDirectory resolves Chat user IDs through the People API. The Chat API
// documents that the {user} in users/{user} is the same person ID the People
// API uses, so translating between the two is nothing but a prefix swap.
type peopleDirectory struct{ svc *peopleapi.Service }

// newPeopleDirectory builds a People client on an already-authenticated
// transport. Extra options are used by tests (option.WithEndpoint, …).
func newPeopleDirectory(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*peopleDirectory, error) {
	all := append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := peopleapi.NewService(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &peopleDirectory{svc: svc}, nil
}

// Lookup resolves ids in parallel batches. A batch that fails leaves its people
// unresolved and records the error; the batches that succeeded are still
// returned, because half a screen of names beats none.
func (d *peopleDirectory) Lookup(ctx context.Context, ids []string) (map[string]Person, error) {
	out := make(map[string]Person, len(ids))
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentBatches)

	for start := 0; start < len(ids); start += peopleBatchSize {
		end := min(start+peopleBatchSize, len(ids))
		wg.Add(1)
		go func(batch []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			people, err := d.batch(ctx, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for id, p := range people {
				out[id] = p
			}
		}(ids[start:end])
	}
	wg.Wait()
	return out, firstErr
}

// batch resolves one request's worth of IDs. The response is keyed by the name
// we asked for, not the one that came back: the two differ when a contact and a
// profile are linked, and only the former matches the message's sender.
func (d *peopleDirectory) batch(ctx context.Context, ids []string) (map[string]Person, error) {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, toPeopleName(id))
	}
	res, err := d.svc.People.GetBatchGet().Context(ctx).
		ResourceNames(names...).
		PersonFields(personFields).
		Do()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Person, len(res.Responses))
	for _, r := range res.Responses {
		if r == nil || r.Person == nil {
			continue
		}
		name := displayName(r.Person)
		if name == "" {
			continue // a person with no name resolves nothing; leave the raw ID
		}
		out[toChatID(r.RequestedResourceName)] = Person{Name: name, Email: primaryEmail(r.Person)}
	}
	return out, nil
}

// toPeopleName turns a Chat "users/{id}" into a People "people/{id}".
func toPeopleName(chatID string) string {
	return "people/" + strings.TrimPrefix(chatID, "users/")
}

// toChatID is toPeopleName's inverse. Note this is a plain function, not the
// Engine.userID method that answers "who am I".
func toChatID(peopleName string) string {
	return "users/" + strings.TrimPrefix(peopleName, "people/")
}

func displayName(p *peopleapi.Person) string {
	for _, n := range p.Names {
		if n != nil && n.DisplayName != "" {
			return n.DisplayName
		}
	}
	return ""
}

func primaryEmail(p *peopleapi.Person) string {
	for _, e := range p.EmailAddresses {
		if e != nil && e.Value != "" {
			return e.Value
		}
	}
	return ""
}
