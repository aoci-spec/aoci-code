package mcptools

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
)

// codebatchSelectForTest orders candidates as Maintain does and takes the
// leading count, so a test can add up the sizes of exactly the issued batch.
func codebatchSelectForTest(candidates []codebatch.Candidate, count int) (ordered, selected []codebatch.Candidate) {
	ordered, _ = codebatch.Select(candidates, 0)
	if count > len(ordered) {
		count = len(ordered)
	}
	return ordered, ordered[:count]
}

// ageRepositoryFiles sets every file's mtime ten seconds back, so the
// fingerprint cache trusts the entries the next walk stores (git's
// racily-clean rule distrusts a file hashed within two seconds of its mtime).
func ageRepositoryFiles(t *testing.T, root string) {
	t.Helper()
	when := time.Now().Add(-10 * time.Second)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		return os.Chtimes(path, when, when)
	})
	if err != nil {
		t.Fatal(err)
	}
}
