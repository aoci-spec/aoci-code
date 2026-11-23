package fs

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Retention bounds for runtime records that nothing reads once their work is
// done (#208). A candidate receipt serves the batch it issued; an archived
// Entries or Managed Scope transaction serves the replay of that same
// transaction. Both grew without bound on a large first build.
const (
	CandidateReceiptRetention    = 14 * 24 * time.Hour
	ArchivedTransactionRetention = 30 * 24 * time.Hour
)

// PruneAgedRecords removes the regular .json files directly in dir whose name
// starts with prefix, were last written more than age ago, and are not keep.
// It is best effort and reports nothing: retention never fails the write
// that triggered it, and a record it could not remove is tried again later.
func PruneAgedRecords(dir, prefix string, age time.Duration, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-age)
	for _, entry := range entries {
		name := entry.Name()
		if name == keep || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
