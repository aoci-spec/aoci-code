package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func legacyReceiptRepo(t *testing.T) string {
	t.Helper()
	root := buildRepo(t)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeStaleLegacyReceipt(t *testing.T, root, rel, line string) {
	t.Helper()
	if err := saveRemoveRecovery(root, removeRecovery{Version: 1, Rel: rel, RemovedLine: line,
		PreIndexSHA256: strings.Repeat("a", 64), PostIndexSHA256: strings.Repeat("b", 64)}); err != nil {
		t.Fatal(err)
	}
}

// A Legacy index closes a stale receipt the same two ways the Volumes path
// does; before rc15 the gate refused every Legacy write on such a receipt and
// the Legacy plan could only answer recovery_postimage_drift.
// A commit that refuses after the plan judged a receipt stale leaves the
// receipt where it was: the discard happens only after every admission check,
// immediately before the fresh receipt takes its place.
func TestLegacyRefusedCommitKeepsTheStaleReceipt(t *testing.T) {
	const line = "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读"
	root := legacyReceiptRepo(t)
	if err := os.Remove(filepath.Join(root, "src", "a.go")); err != nil {
		t.Fatal(err)
	}
	writeStaleLegacyReceipt(t, root, "src/a.go", line)
	previous := lstatRemoveTarget
	calls := 0
	lstatRemoveTarget = func(path string) (os.FileInfo, error) {
		calls++
		if calls >= 2 {
			// The source reappears between the plan and the locked commit.
			return os.Lstat(filepath.Join(root, "src"))
		}
		return previous(path)
	}
	t.Cleanup(func() { lstatRemoveTarget = previous })
	if _, fail := ApplyRemoveEntry(root, "src/a.go", "agent", true, false); fail == nil {
		t.Fatal("a source that reappeared must refuse the removal")
	}
	if _, err := os.Stat(removeRecoveryPath(root, "src/a.go")); err != nil {
		t.Fatalf("a refused commit must leave the stale receipt: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".aoci", "index.txt"))
	if err != nil || !strings.Contains(string(after), "a.go[") {
		t.Fatalf("the Entry must be untouched: err=%v", err)
	}
}

// Parse warnings in a Legacy index are findings, not failures: the superseded
// closure still proves the Entry absent through them, and it needs no Ledger.
