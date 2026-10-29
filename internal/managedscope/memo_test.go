package managedscope

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func memoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	for _, rel := range []string{"src/main.go", "src/util.go", "docs/readme.md", "vendor/dep/lib.go", ".env"} {
		writeScopeFixture(t, root, rel, "// "+rel+"\n")
	}
	writeScopeFixture(t, root, ".gitignore", "build/\n")
	writeScopeFixture(t, root, "build/out.bin", "x")
	gitScopeFixture(t, root, "add", ".")
	// Untracked and not ignored: the one kind of path a .gitignore edit can
	// still move, since git never ignores what it tracks.
	writeScopeFixture(t, root, "scratch/notes.txt", "untracked\n")
	return root
}

func memoCounts() (int64, int64) { return evaluationMemoHits.Load(), evaluationMemoMisses.Load() }

// The second Build over an unchanged tree is a hit and equals, field for
// field, a Build with the memo switched off.
func TestEvaluationMemoHitEqualsAFreshBuild(t *testing.T) {
	root := memoFixture(t)
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	hits0, misses0 := memoCounts()
	first, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hits1, misses1 := memoCounts()
	if misses1-misses0 != 1 || hits1-hits0 != 1 {
		t.Fatalf("expected one miss then one hit, got misses=%d hits=%d", misses1-misses0, hits1-hits0)
	}
	t.Setenv(baseline.ProcessMemoEnv, "off")
	fresh, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, fresh) || !reflect.DeepEqual(second, fresh) {
		t.Fatal("memoized evaluation differs from a fresh build")
	}
	if hits2, _ := memoCounts(); hits2 != hits1 {
		t.Fatal("the memo answered while switched off")
	}
}

// Every input of an evaluation enters the key: each mutation below must miss,
// and the verdict after it must equal a fresh build.
func TestEvaluationMemoMissesOnEveryInput(t *testing.T) {
	root := memoFixture(t)
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	options := BuildOptions{}
	build := func() *Evaluation {
		t.Helper()
		result, err := Build(root, policy, options)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	build()
	cases := []struct {
		name   string
		mutate func()
	}{
		{"file added", func() { writeScopeFixture(t, root, "src/new.go", "package src\n") }},
		{"file removed", func() {
			if err := os.Remove(filepath.Join(root, "src", "util.go")); err != nil {
				t.Fatal(err)
			}
		}},
		{"untracked file becomes git-ignored", func() { writeScopeFixture(t, root, ".gitignore", "build/\nscratch/\n") }},
		{"rule edited", func() {
			policy.Rules = append(policy.Rules, Rule{RuleID: "memo-observe-docs", Action: machinecontract.ScopeRoleObserve,
				Pattern: "docs/**", PatternKind: machinecontract.ScopePatternGlob, Reason: "memo", Source: machinecontract.ScopeRuleUser,
				CreatedBy: "test", Order: 900, Enabled: true})
		}},
		{"curation exclusion added", func() { options.CurationExclude = []string{"src/main.go"} }},
		{"walk option changed", func() { options.WalkOptions.ExcludeDirs = []string{"vendor"} }},
		{"high-risk opt-in changed", func() { options.HighRiskOptIn = []string{".env"} }},
		{"root layout moves to Volumes", func() { writeScopeFixture(t, root, "aoci.txt", cognition.RootManifestMarker+"\n") }},
	}
	for _, tc := range cases {
		_, missesBefore := memoCounts()
		tc.mutate()
		got := build()
		if _, missesAfter := memoCounts(); missesAfter != missesBefore+1 {
			t.Fatalf("%s: the memo answered from before the change (misses %d -> %d)", tc.name, missesBefore, missesAfter)
		}
		t.Setenv(baseline.ProcessMemoEnv, "off")
		want, err := Build(root, policy, options)
		t.Setenv(baseline.ProcessMemoEnv, "")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: verdict after the change differs from a fresh build", tc.name)
		}
		hitsBefore, _ := memoCounts()
		build()
		if hitsAfter, _ := memoCounts(); hitsAfter != hitsBefore+1 {
			t.Fatalf("%s: the rebuilt verdict was not remembered", tc.name)
		}
	}
}

// What a caller gets is its own: trimming or reordering it leaves the next
// hit intact, and an empty list stays [] rather than null.
func TestEvaluationMemoHandsOutIndependentCopies(t *testing.T) {
	root := memoFixture(t)
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	first, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Observe == nil || first.Exclude == nil {
		t.Fatal("Build no longer initialises empty lists")
	}
	index := len(first.Index)
	first.Index = first.Index[:0]
	first.Index = append(first.Index, PathEvaluation{Path: "tampered"})
	second, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Index) != index || second.Index[0].Path == "tampered" || second.Observe == nil || second.Exclude == nil {
		t.Fatalf("the memo handed out shared storage: %d entries, first %q", len(second.Index), second.Index[0].Path)
	}
}

// Concurrent builds over one root all answer the same verdict.
func TestEvaluationMemoIsRaceFree(t *testing.T) {
	root := memoFixture(t)
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	want, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 5; round++ {
				got, buildErr := Build(root, policy, BuildOptions{})
				if buildErr != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("concurrent build differs (err=%v)", buildErr)
					return
				}
			}
		}()
	}
	group.Wait()
}

// The key itself: two inventories that differ only in a single path, or in a
// path moving from tracked to ignored, never share a digest.
func TestEvaluationMemoKeySeparatesInventories(t *testing.T) {
	policy, err := Normalize(DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	base := &fs.SafeInventory{ManagedCandidates: []string{"a.go", "b.go"}, TrackedPaths: []string{"a.go", "b.go"}}
	variants := []*fs.SafeInventory{
		{ManagedCandidates: []string{"a.go", "b.go", "c.go"}, TrackedPaths: []string{"a.go", "b.go", "c.go"}},
		{ManagedCandidates: []string{"a.go", "b.go"}, TrackedPaths: []string{"a.go"}, IgnoredPaths: []string{"b.go"}},
		{ManagedCandidates: []string{"a.go", "b.go"}, TrackedPaths: []string{"a.go", "b.go"}, Exclusions: []fs.SafeInventoryExclusion{{PathSummary: "x", Category: "sensitive"}}},
	}
	want, err := evaluationMemoKey(policy, fs.WalkOptions{}, "", nil, base)
	if err != nil {
		t.Fatal(err)
	}
	for index, variant := range variants {
		got, keyErr := evaluationMemoKey(policy, fs.WalkOptions{}, "", nil, variant)
		if keyErr != nil || got == want {
			t.Fatalf("variant %d shares the key with the base inventory (err=%v)", index, keyErr)
		}
	}
	if layout, _ := evaluationMemoKey(policy, fs.WalkOptions{}, cognition.LayoutVolumesV1, nil, base); layout == want {
		t.Fatal("the root layout does not enter the key")
	}
}
