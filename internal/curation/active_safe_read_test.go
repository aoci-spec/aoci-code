//go:build !windows

package curation

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

// A decision whose path left the Baseline is matched only against a regular
// file inside the repository: a link to an outside file is not followed and a
// FIFO is not opened (rv34).
func TestActiveExclusionsReadsOnlyRegularFilesInTheRepository(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	data := []byte("package excluded\n")
	if err := os.WriteFile(filepath.Join(outside, "excluded.go"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	document := &Document{Version: 1, Decisions: []Decision{{Path: "excluded.go", Decision: DecisionExclude, SourceSHA256: hexSum(data)}}}
	active := baseline.NewBaseline(nil)

	if err := os.Symlink(filepath.Join(outside, "excluded.go"), filepath.Join(root, "excluded.go")); err != nil {
		t.Fatal(err)
	}
	if got := ActiveExclusions(root, nil, document, active); len(got) != 0 {
		t.Fatalf("a link to an outside file matched the decision: %v", got)
	}

	if err := os.Remove(filepath.Join(root, "excluded.go")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "excluded.go"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan []string, 1)
	go func() { done <- ActiveExclusions(root, nil, document, active) }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("a FIFO matched the decision: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ActiveExclusions blocked opening a FIFO")
	}
}

func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
