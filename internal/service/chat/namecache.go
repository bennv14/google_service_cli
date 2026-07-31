package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// nameCacheTTL is how long a remembered name stays usable. Display names are
// nearly static, and one a few days stale is harmless. This does not contradict
// query.go's refusal to cache messages and read state: those must be fresh
// because a stale answer there is wrong, not merely old.
const nameCacheTTL = 30 * 24 * time.Hour

// nameCache is implemented by a Directory that can also remember names this
// package worked out for itself — a DM named after the person in it, which no
// API returns. Keying by resource name lets one file hold both people and
// spaces. Callers reach it through a type assertion, so a Directory without a
// cache simply skips the extra work.
type nameCache interface {
	Recall(id string) (Person, bool)
	Remember(id string, p Person)
	Flush()
}

// cacheEntry is one remembered name and when it was learned.
type cacheEntry struct {
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
	At    time.Time `json:"at"`
}

// cachedDirectory answers from a JSON file on disk and asks inner only for what
// it does not already know. Every cache failure is silent: a name is a
// convenience, and no read command may fail for want of one.
type cachedDirectory struct {
	inner   Directory
	path    string
	now     func() time.Time
	refresh bool // --refresh-names: ignore what is on disk, but still write

	mu      sync.Mutex
	entries map[string]cacheEntry
	loaded  bool
	dirty   bool
}

func newCachedDirectory(inner Directory, path string, refresh bool) *cachedDirectory {
	return &cachedDirectory{inner: inner, path: path, now: time.Now, refresh: refresh}
}

// load reads the file on first use. A missing, unreadable, or corrupt file is
// an empty cache, not an error. Callers hold c.mu.
func (c *cachedDirectory) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.entries = map[string]cacheEntry{}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var entries map[string]cacheEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return
	}
	if entries != nil {
		now := c.now()
		for id, e := range entries {
			if now.Sub(e.At) < nameCacheTTL {
				c.entries[id] = e
			}
		}
	}
}

// usable reports whether an entry may be served. --refresh-names makes every
// entry unusable for reading while leaving writes alone, which is exactly what
// "re-fetch and overwrite" means.
func (c *cachedDirectory) usable(e cacheEntry, ok bool) bool {
	return ok && !c.refresh && c.now().Sub(e.At) < nameCacheTTL
}

func (c *cachedDirectory) Lookup(ctx context.Context, ids []string) (map[string]Person, error) {
	c.mu.Lock()
	c.load()
	out := make(map[string]Person, len(ids))
	var missing []string
	seenMissing := make(map[string]bool)
	for _, id := range ids {
		e, ok := c.entries[id]
		if c.usable(e, ok) {
			out[id] = Person{Name: e.Name, Email: e.Email}
			continue
		}
		if !seenMissing[id] {
			seenMissing[id] = true
			missing = append(missing, id)
		}
	}
	c.mu.Unlock()

	if len(missing) == 0 {
		return out, nil
	}
	found, err := c.inner.Lookup(ctx, missing)

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for id, p := range found {
		out[id] = p
		c.entries[id] = cacheEntry{Name: p.Name, Email: p.Email, At: now}
		c.dirty = true
	}
	return out, err
}

func (c *cachedDirectory) Recall(id string) (Person, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	e, ok := c.entries[id]
	if !c.usable(e, ok) {
		return Person{}, false
	}
	return Person{Name: e.Name, Email: e.Email}, true
}

func (c *cachedDirectory) Remember(id string, p Person) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.entries[id] = cacheEntry{Name: p.Name, Email: p.Email, At: c.now()}
	c.dirty = true
}

// Flush writes the cache back if anything changed. It is written through a temp
// file so an interrupted run cannot leave a half-written cache behind, and the
// temp file's 0600 mode carries over: these are real names and addresses.
func (c *cachedDirectory) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return
	}
	now := c.now()
	clean := make(map[string]cacheEntry, len(c.entries))
	for id, e := range c.entries {
		if now.Sub(e.At) < nameCacheTTL {
			clean[id] = e
		}
	}

	b, err := json.Marshal(clean)
	if err != nil {
		return
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "people-*.json.tmp")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp.Name(), c.path); err == nil {
		c.dirty = false
	}
}
