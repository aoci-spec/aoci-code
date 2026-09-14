package mcptools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// addNoOpExcludeRule edits the desired policy the way scope rule add does:
// a rule matching a path that never exists, so desired ≠ active and a Scope
// Change is required while no role actually moves.
func addNoOpExcludeRule(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	edited := *cfg.ManagedScope
	edited.Rules = append(append([]managedscope.Rule{}, edited.Rules...), managedscope.Rule{
		RuleID: "no-op-future", Action: machinecontract.ScopeRoleExclude, Pattern: "never-present.txt",
		PatternKind: machinecontract.ScopePatternFile, Reason: "test policy edit", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "scope-test", Order: 100, Enabled: true})
	normalized, err := managedscope.Normalize(edited)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &normalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func baselineDigest(t *testing.T, root, rel string) string {
	t.Helper()
	value, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint, ok := value.Files[rel]
	if !ok {
		t.Fatalf("%s is not in the Baseline", rel)
	}
	return fingerprint.SHA256
}

// Issue #47, end to end through the MCP surface: a Volumes repository with one
// stale index source and one pending policy edit had no legal move. Maintain
// and update_entry stopped with scope_change_required; the policy-only Scope
// Change was refused with managed_scope_index_source_stale; the candidate that
// would have cleared it could only be projected into the Root manifest. Now
// the policy-only change applies and retains the old fingerprint, Maintain
// plans the stale source afterwards, and one ordinary batch aligns it.
func TestVolumesPolicyChangeOverStaleSourceAppliesThenMaintainAuthors(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	enableProductionManagedScope(t, root)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	if aligned := maintainVolumeBatch(t, session); aligned.Status != autoStatusApplied || !aligned.Aligned {
		t.Fatalf("fixture must start aligned: %#v", aligned)
	}
	old := baselineDigest(t, root, "main.go")

	writeVolumeTestFile(t, root, "main.go", "package main\n\nfunc Added() {}\n")
	addNoOpExcludeRule(t, root)

	blockedText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	if !strings.Contains(blockedText, `"status":"stopped"`) || !strings.Contains(blockedText, "scope_change_required") {
		t.Fatalf("a pending policy edit must stop Maintain with scope_change_required: %s", blockedText)
	}

	// The candidate that used to be the only way past the stale guard is
	// refused outright: it would have been inserted into the Root manifest.
	live, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	withEntry := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{{CandidateID: "c1", Path: "main.go", SourceSHA256: live.SHA256,
			NewEntry: "main.go[CD9S]: F:run the fixture | R:- | A:main | S:-", ReviewStatus: scopechange.ReviewStatusReviewed}},
		Dispositions: []scopechange.EntryDisposition{}}
	if _, err := scopechange.Build(root, "2026-09-14T00:00:00Z", withEntry); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_volumes_entry_candidates_unsupported") {
		t.Fatalf("Volumes must refuse an Entry candidate: %v", err)
	}

	policyOnly := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}}
	preview, err := scopechange.Build(root, "2026-09-14T00:00:00Z", policyOnly)
	if err != nil {
		t.Fatalf("the policy-only change is the one legal move and must plan: %v", err)
	}
	if len(preview.Plan.SourceStaleRetained) != 1 || preview.Plan.SourceStaleRetained[0].Path != "main.go" {
		t.Fatalf("the stale source must be reported as retained: %+v", preview.Plan.SourceStaleRetained)
	}
	if preview.Plan.InteractionRequired {
		t.Fatalf("a no-op policy edit is auto-authorizable; retention must not turn it into a human gate: %+v", preview.Plan)
	}
	manifestBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	codeBefore, _ := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	result, err := scopechange.Apply(root, preview, nil)
	if err != nil {
		t.Fatalf("auto apply: %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, manifestBefore) {
		t.Fatal("Apply rewrote the Root manifest")
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.code.txt")); !bytes.Equal(after, codeBefore) {
		t.Fatal("Apply touched the Code Volume")
	}
	if got := baselineDigest(t, root, "main.go"); got != old {
		t.Fatalf("active Baseline must keep the digest the Entry was bound to: got %s want %s", got, old)
	}

	// The policy is active, so Maintain runs again and plans exactly the
	// source Apply retained as stale; one ordinary batch aligns it.
	planned := maintainVolumeBatch(t, session)
	if planned.Status != autoStatusRepairRequired || planned.CodePlan == nil || len(planned.Candidates) != 1 ||
		planned.Candidates[0].Path != "main.go" {
		t.Fatalf("Maintain must plan the retained stale source after Apply: %#v", planned)
	}
	applied := applyVolumeBatch(t, session, codeBatchArguments(planned.CodePlan))
	if applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("authoring the retained source must align the repository: %#v", applied)
	}
	if got := baselineDigest(t, root, "main.go"); got != live.SHA256 {
		t.Fatalf("after authoring, the Baseline binds the live bytes: got %s want %s", got, live.SHA256)
	}
}
