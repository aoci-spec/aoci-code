package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// A batch of removals alone is keyed by its paths, so the receipt archived by
// an earlier removal of the same orphan must not answer a later one (#176).
func TestRemovalOnlyBatchWalksReceiptGenerations(t *testing.T) {
	root := t.TempDir()
	items := []normalizedAtomicItem{{rel: "pkg/a.go", remove: true}}
	base := atomicBatchKey(items)
	sha := strings.Repeat("a", 64)
	present := &cognition.Set{Volumes: map[string]*cognition.Asset{
		cognition.ScopeCode: {Objects: []cognition.Object{{CanonicalRef: "code:pkg/a.go"}}},
	}}
	absent := &cognition.Set{Volumes: map[string]*cognition.Asset{cognition.ScopeCode: {}}}
	archive := func(key string) {
		t.Helper()
		if err := saveAtomicBatchRecovery(root, atomicBatchRecovery{Version: 1, BatchKey: key, PreIndexSHA256: sha, PostIndexSHA256: sha}); err != nil {
			t.Fatal(err)
		}
		target := archivedAtomicBatchRecoveryPath(root, key)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(atomicBatchRecoveryPath(root, key), target); err != nil {
			t.Fatal(err)
		}
	}

	key, recovery, err := resolveBatchRecovery(root, present, items)
	if key != base || recovery != nil || !os.IsNotExist(err) {
		t.Fatalf("a first removal must be fresh under the base key: %s %v %v", key, recovery, err)
	}

	archive(base)
	second, recovery, err := resolveBatchRecovery(root, present, items)
	if second == base || recovery != nil || !os.IsNotExist(err) {
		t.Fatalf("a recreated orphan must get the next generation, fresh: %s %v %v", second, recovery, err)
	}
	replayKey, recovery, err := resolveBatchRecovery(root, absent, items)
	if replayKey != base || recovery == nil || err != nil {
		t.Fatalf("with the Entry gone the archived receipt is this batch's replay: %s %v %v", replayKey, recovery, err)
	}

	archive(second)
	third, recovery, err := resolveBatchRecovery(root, present, items)
	if third == base || third == second || recovery != nil || !os.IsNotExist(err) {
		t.Fatalf("a third removal must get its own generation: %s %v %v", third, recovery, err)
	}
	replayKey, _, err = resolveBatchRecovery(root, absent, items)
	if replayKey != second || err != nil {
		t.Fatalf("the replay answer is the newest completed removal: %s %v", replayKey, err)
	}

	// An active receipt at any generation is this batch in flight, whether or
	// not the Volume still carries the Entry.
	if err := saveAtomicBatchRecovery(root, atomicBatchRecovery{Version: 1, BatchKey: third, PreIndexSHA256: sha, PostIndexSHA256: sha}); err != nil {
		t.Fatal(err)
	}
	for _, set := range []*cognition.Set{present, absent} {
		key, recovery, err = resolveBatchRecovery(root, set, items)
		if key != third || recovery == nil || err != nil {
			t.Fatalf("an in-flight receipt must be found: %s %v %v", key, recovery, err)
		}
	}

	// An authored item is bound to its preimage by its candidate, so its key
	// never walks generations.
	authored := []normalizedAtomicItem{{rel: "pkg/a.go", newEntry: "a.go[CG5T]: F:x | R:- | A:- | S:-", candidateID: "c", batchID: "b"}}
	authoredBase := atomicBatchKey(authored)
	archive(authoredBase)
	key, recovery, err = resolveBatchRecovery(root, present, authored)
	if key != authoredBase || recovery == nil || err != nil {
		t.Fatalf("an authored batch keeps its one key: %s %v %v", key, recovery, err)
	}
}
