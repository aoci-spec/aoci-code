package managedscope

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Two snapshots of a settled tree are identical and the second one reads no
// source: the fingerprint cache under .aoci answers every path it hashed.
func TestSnapshotReusingServesASettledTreeFromTheCache(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Second)
	for name, body := range map[string]string{"main.go": "package main\n", "lib.go": "package main\n\nvar x = 1\n", "notes.txt": "n\n"} {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	policy := Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: machinecontract.ScopeProfileProduction,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, Rules: []Rule{
			{RuleID: "txt", Action: machinecontract.ScopeRoleObserve, Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true}}}
	evaluation, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("snapshot sizes: %d %d", len(first), len(second))
	}
	for rel, fp := range first {
		if second[rel] != fp {
			t.Fatalf("%s differs between the cold and cached snapshot: %+v vs %+v", rel, fp, second[rel])
		}
		if fp.Role == "" {
			t.Fatalf("%s lost its role", rel)
		}
	}
	cache := baseline.OpenFingerprintCache(root)
	for rel := range first {
		if _, err := cache.HashFile(filepath.Join(root, rel), rel, baseline.Fingerprint{}); err != nil {
			t.Fatal(err)
		}
	}
	if cache.Hits != 3 || cache.Misses != 0 {
		t.Fatalf("the saved cache does not cover the snapshot: hits=%d misses=%d", cache.Hits, cache.Misses)
	}
}
