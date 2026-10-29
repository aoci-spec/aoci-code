package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

const (
	fingerprintCacheVersion = "aoci-fingerprint-cache/v1"
	fingerprintCacheFile    = "fingerprints.json"

	// fingerprintCacheRacyWindow is how much older than its own hashing moment
	// a file's modification time must be before the cached digest is trusted.
	// A write landing within that window of the hash can leave size and mtime
	// unchanged (FAT and exFAT keep two-second mtimes), so such an entry is
	// hashed again next time: the rule git's index applies to a racily clean
	// entry.
	fingerprintCacheRacyWindow = 2 * time.Second

	// FingerprintCacheEnv set to "off" (or "0") disables the cache for one
	// process, so every call hashes every file exactly as before it existed.
	FingerprintCacheEnv = "AOCI_FINGERPRINT_CACHE"
)

// fingerprintCacheWrites is cleared by the commands documented to change no
// byte of the repository (the status page, scan --dry-run): they still take
// hits, they never save.
var fingerprintCacheWrites = true

// SetFingerprintCacheWrites decides for this process whether a cache may be
// saved; reads are unaffected either way.
func SetFingerprintCacheWrites(enabled bool) { fingerprintCacheWrites = enabled }

type fingerprintCacheEntry struct {
	Size        int64       `json:"size"`
	MtimeNS     int64       `json:"mtime_ns"`
	HashedAtNS  int64       `json:"hashed_at_ns"`
	Fingerprint Fingerprint `json:"fingerprint"`
	// Lines is the file's line count, kept beside the digest because the
	// curation profile and the scale evidence count the same bytes the
	// digest already read; nil until something asked.
	Lines *int `json:"lines,omitempty"`
}

type fingerprintCacheDocument struct {
	Version string                           `json:"version"`
	Entries map[string]fingerprintCacheEntry `json:"entries"`
}

// FingerprintCache remembers, per repository-relative path, the Fingerprint
// hashFile produced for a file together with the size and modification time
// the file had at that moment. The next call over an unchanged tree then
// costs one stat per file instead of one read, which is where every
// governance call spent its time on a large repository: about 0.27 ms per
// file, on every Maintain, verify, check, and Guide, with nothing reused
// between calls.
//
// It is a runtime asset under .aoci, git-ignored by the init boundary, and
// never an input to Baseline bytes, CAS, or any identity: a hit returns the
// digest the bytes had, a miss computes it, and a missing, corrupt, or foreign
// file simply means every path is hashed. The trust rule is git's: same size,
// same mtime, and an mtime older than the hash by the racy window. A write
// that keeps both size and mtime (touch -r over new content) is not seen,
// exactly as git does not see it; FingerprintCacheEnv=off hashes everything.
type FingerprintCache struct {
	path     string
	entries  map[string]fingerprintCacheEntry
	seen     map[string]bool
	disabled bool
	dirty    bool
	// Hits and Misses count this cache's answers, for tests and diagnostics.
	Hits, Misses int
}

// OpenFingerprintCache reads the repository's cache, or an empty one when the
// file is absent, unreadable, or of another version. It never fails.
func OpenFingerprintCache(repositoryRoot string) *FingerprintCache {
	cache := &FingerprintCache{path: filepath.Join(repositoryRoot, ".aoci", fingerprintCacheFile),
		entries: map[string]fingerprintCacheEntry{}, seen: map[string]bool{}}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(FingerprintCacheEnv))) {
	case "off", "0", "false":
		cache.disabled = true
		return cache
	}
	data, err := os.ReadFile(cache.path)
	if err != nil {
		return cache
	}
	// Opened twice per governance call (the snapshot and the curation
	// profiles) and once per call after that: the decoded entries are
	// remembered by the bytes' digest, and every open still gets its own map.
	if entries, ok := lookupFingerprintCacheMemo(cache.path, data); ok {
		cache.entries = entries
		return cache
	}
	var stored fingerprintCacheDocument
	if json.Unmarshal(data, &stored) != nil || stored.Version != fingerprintCacheVersion || stored.Entries == nil {
		return cache
	}
	cache.entries = stored.Entries
	storeFingerprintCacheMemo(cache.path, data, stored.Entries)
	return cache
}

// HashFile returns exactly what HashFileReusing(path, prior) returns for the
// file at path, known in the cache as rel: from the cache when the file's
// size and mtime still match an entry hashed long enough ago, otherwise by
// hashing the bytes and remembering the result.
func (c *FingerprintCache) HashFile(path, rel string, prior Fingerprint) (Fingerprint, error) {
	if c == nil || c.disabled {
		return HashFileReusing(path, prior)
	}
	c.seen[rel] = true
	before, statErr := os.Stat(path)
	regular := statErr == nil && before.Mode().IsRegular()
	if regular {
		if entry, ok := c.entries[rel]; ok && entry.Fingerprint.SHA256 != "" &&
			entry.Size == before.Size() && entry.MtimeNS == before.ModTime().UnixNano() &&
			entry.MtimeNS+int64(fingerprintCacheRacyWindow) < entry.HashedAtNS {
			c.Hits++
			fingerprint := entry.Fingerprint
			fingerprint.Role = ""
			return fingerprint, nil
		}
	}
	hashedAt := time.Now().UnixNano()
	fingerprint, err := HashFileReusing(path, prior)
	if err != nil {
		return fingerprint, err
	}
	c.Misses++
	if regular {
		// Remember the digest only for the bytes it describes: a file written
		// while it was being read is left for the next call to hash again.
		if after, afterErr := os.Stat(path); afterErr == nil && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime()) {
			stored := fingerprint
			stored.Role = ""
			c.entries[rel] = fingerprintCacheEntry{Size: before.Size(), MtimeNS: before.ModTime().UnixNano(), HashedAtNS: hashedAt, Fingerprint: stored}
			c.dirty = true
		}
	}
	return fingerprint, nil
}

// FileLines returns the file's line count as afs.CountFileLines does, from
// the cache when the entry for rel is current, counting and remembering it
// otherwise. HashFile must have run for rel in this process or a previous
// one; a path the cache does not know is simply counted.
func (c *FingerprintCache) FileLines(path, rel string) (int, error) {
	if c != nil && !c.disabled {
		if entry, ok := c.entries[rel]; ok && entry.Lines != nil {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() &&
				entry.Size == info.Size() && entry.MtimeNS == info.ModTime().UnixNano() &&
				entry.MtimeNS+int64(fingerprintCacheRacyWindow) < entry.HashedAtNS {
				c.Hits++
				return *entry.Lines, nil
			}
		}
	}
	lines, err := afs.CountFileLines(path)
	if err != nil {
		return 0, err
	}
	if c != nil && !c.disabled {
		if entry, ok := c.entries[rel]; ok {
			if info, statErr := os.Stat(path); statErr == nil && entry.Size == info.Size() && entry.MtimeNS == info.ModTime().UnixNano() {
				count := lines
				entry.Lines = &count
				c.entries[rel] = entry
				c.dirty = true
			}
		}
	}
	return lines, nil
}

// Save writes the cache back when anything changed, best effort: a failure
// costs the next call a hash, nothing else. With pruneUnseen the entries this
// run never asked for are dropped, which a caller that walked the whole tree
// passes so the file tracks the tree. The .aoci directory is never created
// here, so a read on a repository without AOCI assets leaves no trace.
func (c *FingerprintCache) Save(pruneUnseen bool) {
	if c == nil || c.disabled || !fingerprintCacheWrites {
		return
	}
	if pruneUnseen {
		for rel := range c.entries {
			if !c.seen[rel] {
				delete(c.entries, rel)
				c.dirty = true
			}
		}
	}
	if !c.dirty {
		return
	}
	if info, err := os.Stat(filepath.Dir(c.path)); err != nil || !info.IsDir() {
		return
	}
	data, err := json.Marshal(fingerprintCacheDocument{Version: fingerprintCacheVersion, Entries: c.entries})
	if err != nil {
		return
	}
	if afs.AtomicWriteMode(c.path, data, 0o600) == nil {
		c.dirty = false
	}
}
