package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// get returns the cached snapshot for root, rebuilding it only when the
// fingerprint moved. Rebuilds are serialized per cache so two concurrent
// page loads never assess the same repository twice.
func (c *repoCache) get(root string, options Options, running []Instance) (*cacheEntry, error) {
	current := fingerprint(root, running)
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[root]; entry != nil && entry.fingerprint == current {
		return entry, nil
	}
	snapshot, entries := buildSnapshot(root, options, running)
	body, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	entry := &cacheEntry{fingerprint: current, etag: `"` + current[:16] + `"`, snapshot: snapshot, body: body, entries: entries}
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
