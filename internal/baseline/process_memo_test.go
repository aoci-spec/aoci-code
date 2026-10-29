package baseline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func writeBaselineFixture(t *testing.T, root string, files map[string]Fingerprint) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o700); err != nil {
		t.Fatal(err)
	}
	value := &Baseline{Version: 1, CreatedAt: "2026-10-30T00:00:00Z", UpdatedAt: "2026-10-30T00:00:00Z", Files: files,
		ManagedScope: &ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: strings.Repeat("a", 64),
			ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired}}
	data, err := MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath(root), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A memoized Load equals a decoded one, hands out its own file map, and a
// rewritten file is decoded afresh.
func TestBaselineLoadMemoReturnsEqualIndependentDocuments(t *testing.T) {
	root := t.TempDir()
	files := map[string]Fingerprint{"a.go": {SHA256: "1", Size: 1, Role: "index"}, "b.go": {SHA256: "2", Size: 2, Role: "observe"}}
	writeBaselineFixture(t, root, files)
	hits0, misses0 := processMemoHits.Load(), processMemoMisses.Load()
	first, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if hits, misses := processMemoHits.Load(), processMemoMisses.Load(); hits-hits0 != 1 || misses-misses0 != 1 {
		t.Fatalf("expected one miss then one hit, got hits=%d misses=%d", hits-hits0, misses-misses0)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("memoized Baseline differs from the decoded one")
	}
	UpdateOne(second, "c.go", Fingerprint{SHA256: "3"})
	second.ManagedScope.PolicyIdentity = "tampered"
	third, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := third.Files["c.go"]; leaked || third.ManagedScope.PolicyIdentity != strings.Repeat("a", 64) {
		t.Fatal("a caller's mutation reached the next Load")
	}
	files["d.go"] = Fingerprint{SHA256: "4"}
	writeBaselineFixture(t, root, files)
	fourth, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := fourth.Files["d.go"]; !present {
		t.Fatal("a rewritten Baseline was served from the memo")
	}
	t.Setenv(ProcessMemoEnv, "off")
	hitsBefore := processMemoHits.Load()
	if _, _, err := Load(root); err != nil {
		t.Fatal(err)
	}
	if processMemoHits.Load() != hitsBefore {
		t.Fatal("the memo answered while switched off")
	}
}

// Two opens of the same cache file get independent entry maps, and a cache
// rewritten on disk is decoded afresh.
func TestFingerprintCacheOpenMemoIsIndependentPerOpen(t *testing.T) {
	root, path := cacheFixture(t)
	ageFile(t, path, 10*time.Second)
	cold := OpenFingerprintCache(root)
	if _, err := cold.HashFile(path, "main.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	cold.Save(true)
	first := OpenFingerprintCache(root)
	second := OpenFingerprintCache(root)
	if len(first.entries) != 1 || len(second.entries) != 1 {
		t.Fatalf("opens did not load the saved entry: %d %d", len(first.entries), len(second.entries))
	}
	delete(first.entries, "main.go")
	if _, still := second.entries["main.go"]; !still {
		t.Fatal("two opens share one entry map")
	}
	third := OpenFingerprintCache(root)
	if _, still := third.entries["main.go"]; !still {
		t.Fatal("a mutation of an opened cache reached the memo")
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", fingerprintCacheFile), []byte(`{"version":"aoci-fingerprint-cache/v1","entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if fresh := OpenFingerprintCache(root); len(fresh.entries) != 0 {
		t.Fatal("a rewritten cache file was served from the memo")
	}
}
