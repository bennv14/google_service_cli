package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// countingDirectory records what it was asked for, so a test can prove the
// cache did not ask.
type countingDirectory struct {
	people map[string]Person
	err    error
	calls  int32
	asked  []string
}

func (d *countingDirectory) Lookup(_ context.Context, ids []string) (map[string]Person, error) {
	atomic.AddInt32(&d.calls, 1)
	d.asked = append(d.asked, ids...)
	out := map[string]Person{}
	for _, id := range ids {
		if p, ok := d.people[id]; ok {
			out[id] = p
		}
	}
	return out, d.err
}

// newTestCache builds a cachedDirectory with a frozen clock, over a fresh temp
// file. (It is not called `at` — render_test.go already owns that name in this
// package.)
func newTestCache(t *testing.T, inner Directory, now time.Time, refresh bool) *cachedDirectory {
	t.Helper()
	c := newCachedDirectory(inner, filepath.Join(t.TempDir(), "cache", "people-test.json"), refresh)
	c.now = func() time.Time { return now }
	return c
}

func TestCacheMissFallsThroughAndIsWrittenBack(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran", Email: "linh@example.com"},
	}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)

	got, err := c.Lookup(context.Background(), []string{"users/1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["users/1"].Name != "Linh Tran" {
		t.Fatalf("got %+v", got)
	}
	c.Flush()

	b, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("cache was not written: %v", err)
	}
	var entries map[string]cacheEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("cache file is not the documented shape: %v\n%s", err, b)
	}
	e := entries["users/1"]
	if e.Name != "Linh Tran" || e.Email != "linh@example.com" || !e.At.Equal(now) {
		t.Fatalf("entry = %+v", e)
	}
}

func TestCacheHitDoesNotCallInner(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	first := newTestCache(t, inner, now, false)
	if _, err := first.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatal(err)
	}
	first.Flush()

	second := newCachedDirectory(inner, first.path, false)
	second.now = func() time.Time { return now.Add(24 * time.Hour) }
	got, err := second.Lookup(context.Background(), []string{"users/1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["users/1"].Name != "Linh Tran" {
		t.Fatalf("got %+v", got)
	}
	if inner.calls != 1 {
		t.Fatalf("inner was called %d times; the second lookup must be served from disk", inner.calls)
	}
}

func TestCacheAsksOnlyForWhatItIsMissing(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran"},
		"users/2": {Name: "Huy Nguyen"},
	}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)
	if _, err := c.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatal(err)
	}

	inner.asked = nil
	got, err := c.Lookup(context.Background(), []string{"users/1", "users/2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want both people", got)
	}
	if len(inner.asked) != 1 || inner.asked[0] != "users/2" {
		t.Fatalf("inner was asked for %v, want only the missing users/2", inner.asked)
	}
}

func TestCacheEntryOlderThanThirtyDaysIsAMiss(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)
	if _, err := c.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatal(err)
	}
	c.Flush()

	stale := newCachedDirectory(inner, c.path, false)
	stale.now = func() time.Time { return now.Add(nameCacheTTL + time.Hour) }
	if _, err := stale.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("inner called %d times; an entry past the TTL must be refetched", inner.calls)
	}
}

func TestCorruptCacheIsTreatedAsEmpty(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	dir := t.TempDir()
	path := filepath.Join(dir, "people-test.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCachedDirectory(inner, path, false)

	got, err := c.Lookup(context.Background(), []string{"users/1"})
	if err != nil {
		t.Fatalf("a corrupt cache must not surface as an error: %v", err)
	}
	if got["users/1"].Name != "Linh Tran" {
		t.Fatalf("got %+v", got)
	}
}

// An unwritable path must cost a lookup next time and nothing else.
func TestUnwritableCacheIsSilent(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCachedDirectory(inner, filepath.Join(file, "people-test.json"), false)

	if _, err := c.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatalf("write failure must not surface as an error: %v", err)
	}
	c.Flush() // must not panic
}

func TestRefreshSkipsTheReadButStillWrites(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	warm := newTestCache(t, inner, now, false)
	if _, err := warm.Lookup(context.Background(), []string{"users/1"}); err != nil {
		t.Fatal(err)
	}
	warm.Flush()

	inner.people["users/1"] = Person{Name: "Linh Tran (renamed)"}
	later := now.Add(48 * time.Hour)
	refresh := newCachedDirectory(inner, warm.path, true)
	refresh.now = func() time.Time { return later }

	got, err := refresh.Lookup(context.Background(), []string{"users/1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["users/1"].Name != "Linh Tran (renamed)" {
		t.Fatalf("--refresh-names must ignore the cached answer, got %+v", got)
	}
	if inner.calls != 2 {
		t.Fatalf("inner calls = %d, want the refresh to have gone to the source", inner.calls)
	}
	refresh.Flush()

	b, _ := os.ReadFile(warm.path)
	var entries map[string]cacheEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	if entries["users/1"].Name != "Linh Tran (renamed)" || !entries["users/1"].At.Equal(later) {
		t.Fatalf("refresh must overwrite the entry, got %+v", entries["users/1"])
	}
}

// Recall/Remember exist for names this package works out for itself — a DM
// named after the person in it, which no API returns.
func TestRememberAndRecallRoundTripThroughDisk(t *testing.T) {
	inner := &countingDirectory{}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)

	if _, ok := c.Recall("spaces/DM1"); ok {
		t.Fatal("an empty cache must not recall anything")
	}
	c.Remember("spaces/DM1", Person{Name: "Linh Tran"})
	c.Flush()

	reload := newCachedDirectory(inner, c.path, false)
	reload.now = func() time.Time { return now.Add(time.Hour) }
	p, ok := reload.Recall("spaces/DM1")
	if !ok || p.Name != "Linh Tran" {
		t.Fatalf("Recall = %+v, %v", p, ok)
	}

	expired := newCachedDirectory(inner, c.path, false)
	expired.now = func() time.Time { return now.Add(nameCacheTTL + time.Hour) }
	if _, ok := expired.Recall("spaces/DM1"); ok {
		t.Fatal("a stale entry must not be recalled")
	}
}

func TestCachedDirectorySatisfiesBothInterfaces(t *testing.T) {
	var _ Directory = (*cachedDirectory)(nil)
	var _ nameCache = (*cachedDirectory)(nil)
}

func TestExpiredEntriesPrunedOnLoadAndFlush(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{"users/1": {Name: "Linh Tran"}}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)

	c.Remember("users/old", Person{Name: "Old User"})
	c.Flush()

	// Re-open cache 31 days later and add a new entry
	later := newCachedDirectory(inner, c.path, false)
	later.now = func() time.Time { return now.Add(nameCacheTTL + 24*time.Hour) }
	later.Remember("users/new", Person{Name: "New User"})
	later.Flush()

	b, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]cacheEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["users/old"]; ok {
		t.Fatalf("expired entry 'users/old' was not pruned from disk cache: %+v", entries)
	}
	if entries["users/new"].Name != "New User" {
		t.Fatalf("new entry 'users/new' missing or incorrect: %+v", entries)
	}
}

func TestLookupDeduplicatesMissingIDs(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{
		"users/1": {Name: "Linh Tran"},
	}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)

	got, err := c.Lookup(context.Background(), []string{"users/1", "users/1", "users/1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["users/1"].Name != "Linh Tran" {
		t.Fatalf("got %+v", got)
	}
	if len(inner.asked) != 1 {
		t.Fatalf("inner was asked for %v, want exactly 1 deduplicated ID", inner.asked)
	}
}

func TestFlushPreservesDirtyOnWriteFailure(t *testing.T) {
	inner := &countingDirectory{people: map[string]Person{}}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	c := newTestCache(t, inner, now, false)

	c.path = filepath.Join(t.TempDir(), "not-a-dir", "cache.json")
	if err := os.WriteFile(filepath.Dir(c.path), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	c.Remember("users/1", Person{Name: "Linh"})
	if !c.dirty {
		t.Fatal("expected dirty = true after Remember")
	}
	c.Flush()
	if !c.dirty {
		t.Fatal("expected dirty to remain true when Flush fails to write to disk")
	}
}
