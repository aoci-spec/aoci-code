package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// ProcessMemoEnv set to "off" (or "0") disables every in-process memo for
// one process: the decoded Baseline and fingerprint-cache documents here and
// the Managed Scope evaluation memo in managedscope. Every call then reads
// and decodes and evaluates exactly as before the memos existed.
const ProcessMemoEnv = "AOCI_PROCESS_MEMO"

// ProcessMemoDisabled reports whether ProcessMemoEnv turns the memos off.
func ProcessMemoDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ProcessMemoEnv))) {
	case "off", "0", "false":
		return true
	}
	return false
}

// processMemoHits and processMemoMisses count the document memos' answers
// for tests and diagnostics.
var processMemoHits, processMemoMisses atomic.Int64

// documentDigest keys a decoded document by the exact bytes it was decoded
// from: a file rewritten with the same size inside the same second, which a
// stat-based key would miss, is a different digest.
func documentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// baselineMemo remembers, per Baseline path, the last decoded document. A hit
// returns a fresh Baseline: the small header is decoded again from its own
// JSON and the file map, which is where decoding spends its time on a large
// repository, is copied, so a caller that mutates what it got (UpdateOne,
// the planners) never changes what the next caller gets.
var baselineMemo = struct {
	mu     sync.Mutex
	byPath map[string]baselineMemoRecord
}{byPath: map[string]baselineMemoRecord{}}

type baselineMemoRecord struct {
	digest string
	header []byte
	files  map[string]Fingerprint
}

func lookupBaselineMemo(path string, data []byte) (*Baseline, bool) {
	if ProcessMemoDisabled() {
		return nil, false
	}
	digest := documentDigest(data)
	baselineMemo.mu.Lock()
	record, ok := baselineMemo.byPath[path]
	baselineMemo.mu.Unlock()
	if !ok || record.digest != digest {
		processMemoMisses.Add(1)
		return nil, false
	}
	var value Baseline
	if json.Unmarshal(record.header, &value) != nil {
		processMemoMisses.Add(1)
		return nil, false
	}
	value.Files = cloneFingerprintMap(record.files)
	processMemoHits.Add(1)
	return &value, true
}

func storeBaselineMemo(path string, data []byte, value *Baseline) {
	if ProcessMemoDisabled() || value == nil {
		return
	}
	header := *value
	header.Files = nil
	encoded, err := json.Marshal(header)
	if err != nil {
		return
	}
	record := baselineMemoRecord{digest: documentDigest(data), header: encoded, files: cloneFingerprintMap(value.Files)}
	baselineMemo.mu.Lock()
	baselineMemo.byPath[path] = record
	baselineMemo.mu.Unlock()
}

func cloneFingerprintMap(source map[string]Fingerprint) map[string]Fingerprint {
	result := make(map[string]Fingerprint, len(source))
	for path, fingerprint := range source {
		result[path] = fingerprint
	}
	return result
}

// fingerprintCacheMemo remembers the decoded fingerprint cache the same way.
// Entries are copied on both store and hit: every open gets its own map to
// mutate and save, as it did when every open decoded the file.
var fingerprintCacheMemo = struct {
	mu     sync.Mutex
	byPath map[string]fingerprintCacheMemoRecord
}{byPath: map[string]fingerprintCacheMemoRecord{}}

type fingerprintCacheMemoRecord struct {
	digest  string
	entries map[string]fingerprintCacheEntry
}

func lookupFingerprintCacheMemo(path string, data []byte) (map[string]fingerprintCacheEntry, bool) {
	if ProcessMemoDisabled() {
		return nil, false
	}
	digest := documentDigest(data)
	fingerprintCacheMemo.mu.Lock()
	record, ok := fingerprintCacheMemo.byPath[path]
	fingerprintCacheMemo.mu.Unlock()
	if !ok || record.digest != digest {
		processMemoMisses.Add(1)
		return nil, false
	}
	processMemoHits.Add(1)
	return cloneFingerprintEntries(record.entries), true
}

func storeFingerprintCacheMemo(path string, data []byte, entries map[string]fingerprintCacheEntry) {
	if ProcessMemoDisabled() {
		return
	}
	record := fingerprintCacheMemoRecord{digest: documentDigest(data), entries: cloneFingerprintEntries(entries)}
	fingerprintCacheMemo.mu.Lock()
	fingerprintCacheMemo.byPath[path] = record
	fingerprintCacheMemo.mu.Unlock()
}

func cloneFingerprintEntries(source map[string]fingerprintCacheEntry) map[string]fingerprintCacheEntry {
	result := make(map[string]fingerprintCacheEntry, len(source))
	for rel, entry := range source {
		result[rel] = entry
	}
	return result
}
