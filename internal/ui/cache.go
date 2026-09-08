package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// watchedFiles are the on-disk facts whose change can change what the page
// shows. Their size and mtime form the cache key; nothing is hashed and no
// file is opened until the key moves, so a 2-second poll costs eight stats.
var watchedFiles = []string{
	"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt",
	filepath.Join(".aoci", "config.json"), filepath.Join(".aoci", "baseline.json"),
	filepath.Join(".aoci", "curation.json"), filepath.Join(".aoci", "database-baseline.json"),
	filepath.Join(".git", "HEAD"),
}

// A snapshot depends on managed source files, host integrations, recovery
// state, and manifest-selected Volume paths in addition to watchedFiles. A
// short recheck interval keeps those dynamic inputs current without rebuilding
// the snapshot for the page's state, entries, and raw requests in one refresh.
const cacheRecheckInterval = 2 * time.Second

// EntryView is one Code or Database object as the entries table shows it.
type EntryView struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Tag     string `json:"tag"`
	C       string `json:"c"`
	E       string `json:"e"`
	F       string `json:"f"`
	R       string `json:"r"`
	A       string `json:"a"`
	S       string `json:"s"`
	Section string `json:"section,omitempty"`
}

type cacheEntry struct {
	fingerprint string
	etag        string
	checkedAt   time.Time
	snapshot    Snapshot
	body        []byte
	entries     []EntryView
}

type repoCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
}

func newRepoCache() *repoCache { return &repoCache{entries: map[string]*cacheEntry{}} }

func fingerprint(root string, running []Instance) string {
	digest := sha256.New()
	for _, name := range watchedFiles {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			fmt.Fprintf(digest, "%s|absent\n", name)
			continue
		}
		fmt.Fprintf(digest, "%s|%d|%d\n", name, info.Size(), info.ModTime().UnixNano())
	}
	for _, instance := range running {
		fmt.Fprintf(digest, "pid|%d|%s|%v\n", instance.PID, instance.Executable, instance.ExecutableDeleted)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// get returns the cached snapshot for root. A fast fingerprint catches common
// changes immediately; the bounded recheck catches every other input that can
// affect governance. Rebuilds are serialized per cache so two concurrent page
// loads never assess the same repository twice.
func (c *repoCache) get(root string, options Options, running []Instance, force bool) (*cacheEntry, error) {
	current := fingerprint(root, running)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.entries[root]
	if !force && previous != nil && previous.fingerprint == current && now.Sub(previous.checkedAt) < cacheRecheckInterval {
		return previous, nil
	}
	snapshot, entries := buildSnapshot(root, options, running)
	generatedAt := snapshot.GeneratedAt
	if previous != nil {
		snapshot.GeneratedAt = previous.snapshot.GeneratedAt
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if previous != nil && bytes.Equal(body, previous.body) {
		previous.fingerprint = current
		previous.checkedAt = time.Now()
		return previous, nil
	}
	snapshot.GeneratedAt = generatedAt
	body, err = json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	entry := &cacheEntry{fingerprint: current, etag: `"` + hex.EncodeToString(sum[:8]) + `"`, checkedAt: time.Now(),
		snapshot: snapshot, body: body, entries: entries}
	c.entries[root] = entry
	return entry, nil
}

// filterEntries applies the table's free-text and tag filters.
func filterEntries(entries []EntryView, query, c, e string) []EntryView {
	query = strings.ToLower(strings.TrimSpace(query))
	matched := []EntryView{}
	for _, entry := range entries {
		if c != "" && entry.C != c {
			continue
		}
		if e != "" && entry.E != e {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Path+" "+entry.F+" "+entry.S+" "+entry.R), query) {
			continue
		}
		matched = append(matched, entry)
	}
	return matched
}
