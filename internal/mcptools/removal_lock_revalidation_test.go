package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// A removal validated before the lock is judged again under it: a file
// re-created at the orphan's path while the batch waited keeps its Entry and
// the batch writes nothing (rv37).
func TestRemovalRevalidatedUnderTheWriteLock(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 12)
	session := connectMCPClient(t, root)
	buildToAligned(t, session)
	orphan := filepath.Join(root, "pkg", "mod001", "file011.go")
	original, err := os.ReadFile(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(orphan); err != nil {
		t.Fatal(err)
	}
	maintain := maintainVolumeBatch(t, session)
	if len(maintain.OrphanRemovals) != 1 {
		t.Fatalf("fixture: one orphan expected, got %v", maintain.OrphanRemovals)
	}
	previous := beforeVolumeBatchCommitLocked
	t.Cleanup(func() { beforeVolumeBatchCommitLocked = previous })
	beforeVolumeBatchCommitLocked = func(string) {
		_ = os.WriteFile(orphan, original, 0o644)
	}
	applied := applyVolumeBatch(t, session, map[string]any{"removals": []map[string]any{{"path": maintain.OrphanRemovals[0]}}})
	beforeVolumeBatchCommitLocked = previous
	if applied.Status == autoStatusApplied {
		t.Fatalf("a removal of a file re-created under the lock applied: %#v", applied)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if cognitionObjectByRef(set.Volumes[cognition.ScopeCode], "code:pkg/mod001/file011.go") == nil {
		t.Fatal("the re-created file's Entry was removed")
	}
}
