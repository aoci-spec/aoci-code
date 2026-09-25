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
func TestLegacyStaleRemoveReceiptClosesSupersededOrDiscarded(t *testing.T) {
	const line = "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读"
	t.Run("superseded", func(t *testing.T) {
		root := legacyReceiptRepo(t)
		indexPath := filepath.Join(root, ".aoci", "index.txt")
		if err := os.Remove(filepath.Join(root, "src", "a.go")); err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatal(err)
		}
		moved := strings.Replace(string(text), line+"\n", "", 1)
		if err := os.WriteFile(indexPath, []byte(moved), 0o644); err != nil {
			t.Fatal(err)
		}
		writeStaleLegacyReceipt(t, root, "src/a.go", line)
		outcome, fail := ApplyRemoveEntry(root, "src/a.go", "agent", true, false)
		if fail != nil || outcome == nil || !outcome.Superseded {
			t.Fatalf("the receipt must close as superseded: fail=%+v outcome=%+v", fail, outcome)
		}
		after, err := os.ReadFile(indexPath)
		if err != nil || string(after) != moved {
			t.Fatalf("a superseded closure must not touch the index: err=%v", err)
		}
		if _, err := os.Stat(removeRecoveryPath(root, "src/a.go")); !os.IsNotExist(err) {
			t.Fatalf("receipt must be gone: %v", err)
		}
	})
	t.Run("discarded", func(t *testing.T) {
		root := legacyReceiptRepo(t)
		if err := os.Remove(filepath.Join(root, "src", "a.go")); err != nil {
			t.Fatal(err)
		}
		writeStaleLegacyReceipt(t, root, "src/a.go", line)
		outcome, fail := ApplyRemoveEntry(root, "src/a.go", "agent", true, false)
		if fail != nil || outcome == nil || !outcome.StaleReceiptDiscarded || outcome.RemovedLine != line {
			t.Fatalf("the stale receipt must be discarded and the removal applied: fail=%+v outcome=%+v", fail, outcome)
		}
		after, err := os.ReadFile(filepath.Join(root, ".aoci", "index.txt"))
		if err != nil || strings.Contains(string(after), "a.go[") {
			t.Fatalf("the orphan must be gone: err=%v\n%s", err, after)
		}
		if _, err := os.Stat(removeRecoveryPath(root, "src/a.go")); !os.IsNotExist(err) {
			t.Fatalf("no receipt may remain: %v", err)
		}
	})
}

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
func TestLegacySupersededClosureToleratesWarningsAndADisabledLedger(t *testing.T) {
	const line = "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读"
	root := buildRepo(t)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	if err := os.Remove(filepath.Join(root, "src", "a.go")); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(string(text), line+"\n", "suspicious line that only looks like an Entry [X]: F:x\n", 1)
	if err := os.WriteFile(indexPath, []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}
	writeStaleLegacyReceipt(t, root, "src/a.go", line)
	outcome, fail := ApplyRemoveEntry(root, "src/a.go", "agent", true, false)
	if fail != nil || outcome == nil || !outcome.Superseded {
		t.Fatalf("the receipt must close as superseded over warnings without a Ledger: fail=%+v outcome=%+v", fail, outcome)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "src/a.go")); !os.IsNotExist(err) {
		t.Fatalf("receipt must be gone: %v", err)
	}
}
