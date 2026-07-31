package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/api/option"
)

// peopleServer fakes people.getBatchGet. It answers for every requested
// resourceName present in known, and silently omits the rest — which is
// exactly what the real API does for a person the caller cannot see.
func peopleServer(t *testing.T, known map[string]Person, calls *int32) *peopleDirectory {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/people:batchGet" {
			http.NotFound(w, r)
			return
		}
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		var responses []map[string]any
		for _, name := range r.URL.Query()["resourceNames"] {
			p, ok := known[name]
			if !ok {
				continue
			}
			responses = append(responses, map[string]any{
				"requestedResourceName": name,
				"person": map[string]any{
					"names":          []map[string]any{{"displayName": p.Name}},
					"emailAddresses": []map[string]any{{"value": p.Email}},
				},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	t.Cleanup(srv.Close)

	d, err := newPeopleDirectory(context.Background(), srv.Client(),
		option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPeopleDirectoryResolvesNamesAndEmails(t *testing.T) {
	d := peopleServer(t, map[string]Person{
		"people/1": {Name: "Linh Tran", Email: "linh@example.com"},
		"people/2": {Name: "Huy Nguyen", Email: "huy@example.com"},
	}, nil)

	got, err := d.Lookup(context.Background(), []string{"users/1", "users/2"})
	if err != nil {
		t.Fatal(err)
	}
	if got["users/1"].Name != "Linh Tran" || got["users/1"].Email != "linh@example.com" {
		t.Fatalf("users/1 = %+v", got["users/1"])
	}
	if got["users/2"].Name != "Huy Nguyen" {
		t.Fatalf("users/2 = %+v", got["users/2"])
	}
}

// An ID the directory does not know must be absent, not an error: partial
// knowledge is the normal case.
func TestPeopleDirectoryOmitsUnknownIDsWithoutFailing(t *testing.T) {
	d := peopleServer(t, map[string]Person{
		"people/1": {Name: "Linh Tran"},
		"people/2": {Name: "Huy Nguyen"},
	}, nil)

	got, err := d.Lookup(context.Background(), []string{"users/1", "users/2", "users/3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d people, want 2: %+v", len(got), got)
	}
	if _, ok := got["users/3"]; ok {
		t.Fatal("an unresolvable ID must be absent from the map")
	}
}

func TestPeopleDirectorySplitsBatchesAtFifty(t *testing.T) {
	known := map[string]Person{}
	var ids []string
	for i := 0; i < 51; i++ {
		id := "users/" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		ids = append(ids, id)
		known["people/"+strings.TrimPrefix(id, "users/")] = Person{Name: id}
	}
	var calls int32
	d := peopleServer(t, known, &calls)

	got, err := d.Lookup(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 51 {
		t.Fatalf("got %d people, want 51", len(got))
	}
	if calls != 2 {
		t.Fatalf("51 IDs must split into 2 requests of at most %d, got %d requests", peopleBatchSize, calls)
	}
}

func TestPeopleDirectoryReturnsErrorWithoutPanicking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":403,"message":"denied"}}`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	d, err := newPeopleDirectory(context.Background(), srv.Client(),
		option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}

	got, lookupErr := d.Lookup(context.Background(), []string{"users/1"})
	if lookupErr == nil {
		t.Fatal("expected an error from a 403")
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want an empty map", got)
	}
}

func TestNullDirectoryResolvesNothing(t *testing.T) {
	got, err := nullDirectory{}.Lookup(context.Background(), []string{"users/1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("nullDirectory returned %+v", got)
	}
}

// The warning has to name the fix, because a token minted before
// directory.readonly was requested is by far the most likely cause.
func TestNameWarningNamesTheFix(t *testing.T) {
	got := nameWarning(errors.New("boom"))
	for _, want := range []string{"cannot resolve sender names", "boom", "gsvc auth login"} {
		if !strings.Contains(got, want) {
			t.Errorf("nameWarning() = %q, want it to mention %q", got, want)
		}
	}
}
