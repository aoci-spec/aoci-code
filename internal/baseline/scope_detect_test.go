package baseline

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestDetectManagedScopeSeparatesObservedEvidence(t *testing.T) {
	raw := "===/repo/===\nmain.go[GO.C9.RT]: F:core | R:- | A:- | S:-\n"
	doc, _ := index.Parse(raw)
	index.ResolveRelPaths(doc, "/repo")
	old := &Baseline{Version: 1, Files: map[string]Fingerprint{
		"main.go":      {Role: machinecontract.ScopeRoleIndex, SHA256: "old-main"},
		"main_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: "old-test"},
		"gone_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: "old-gone"},
	}}
	current := map[string]Fingerprint{
		"main.go":      {Role: machinecontract.ScopeRoleIndex, SHA256: "new-main"},
		"main_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: "new-test"},
		"new_test.go":  {Role: machinecontract.ScopeRoleObserve, SHA256: "new-test-2"},
	}
	result := DetectManagedScope("/repo", doc, old, current, fs.WalkOptions{}, false)
	if len(result.Stale) != 1 || result.Stale[0] != "main.go" || len(result.Missing) != 0 || len(result.Orphan) != 0 {
		t.Fatalf("formal drift was not separated: %+v", result)
	}
	if len(result.ObservedChanged) != 1 || result.ObservedChanged[0] != "main_test.go" ||
		len(result.ObservedNew) != 1 || result.ObservedNew[0] != "new_test.go" ||
		len(result.ObservedRemoved) != 1 || result.ObservedRemoved[0] != "gone_test.go" {
		t.Fatalf("observe states incomplete: %+v", result)
	}
}
