// A stale Baseline record of a deleted file is not a coverage reduction.
//
// A real experience: internal/codebatch/relations.go was deleted when the relation validation was split out, but its index-role
// record stayed in the Baseline for three weeks. Everyday verify/check do not look at it, so the repository kept reporting aligned; while any
// Scope Change re-evaluates every role, so this stale account was necessarily judged a coverage reduction (risk=high),
// automatic authorization refused, and a pure policy change was forced to require a human approval.
//
// The criterion is "can the current Safe Inventory still see this path": an excluded but existing file stays in the evaluation,
// and only a file that really vanished is absent. A vanished file has neither an Entry nor bytes to lose; retiring its record is bookkeeping,
// not a reduction.
package scopechange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestVanishedSourceIsBookkeepingNotCoverageReduction(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)

	// Build one stale account: the Baseline holds an index-role record, but the file has vanished from the working tree.
	vanished := "internal/gone/removed.go"
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	state.Files[vanished] = baseline.Fingerprint{
		Role: machinecontract.ScopeRoleIndex, SHA256: strings.Repeat("a", 64),
		NormalizedSHA256: strings.Repeat("a", 64), Size: 1,
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, vanished)); !os.IsNotExist(statErr) {
		t.Fatalf("fixture precondition broken: %s should not exist", vanished)
	}

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}

	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == vanished {
			t.Fatalf("a deleted file has no Entry and no bytes to lose, so retiring its stale "+
				"Baseline record must not count as a coverage reduction: %+v", reduction)
		}
	}
	if preview.Plan.Risk.CognitionCoverageReduction {
		t.Fatalf("stale bookkeeping must not raise the coverage-reduction risk: %+v", preview.Plan.Risk)
	}
	// The fixture itself retires one Entry, so the overall Level is not low; what is locked is that this stale account contributes
	// no coverage reduction count and no P1.
	if preview.Plan.Risk.CoverageReductionCount != 0 || preview.Plan.Risk.P1 != 0 {
		t.Fatalf("stale bookkeeping must contribute no coverage-reduction count and no P1: %+v", preview.Plan.Risk)
	}

	// The bookkeeping still happens: the stale account really is cleared, it just no longer needs a human.
	removed := false
	for _, item := range preview.Plan.BaselineRemoved {
		if item.Path == vanished {
			removed = true
		}
	}
	if !removed {
		t.Fatal("the stale Baseline record should still be retired by the transaction")
	}
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err != nil {
		t.Fatalf("policy-bound auto must accept a pure bookkeeping retirement: %v", err)
	}
}

// The opposite: the file still exists and merely has no Entry yet; that is really losing coverage and must be blocked as before.
func TestExistingUnauthoredSourceStillCountsAsCoverageReduction(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)

	// A source file that really exists but has no Entry.
	present := filepath.Join(root, "unauthored.go")
	if err := os.WriteFile(present, []byte("package main\n\nfunc Unauthored() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(present)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files["unauthored.go"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{
			RuleID: "drop-unauthored", Action: machinecontract.ScopeRoleExclude,
			Pattern: "unauthored.go", PatternKind: machinecontract.ScopePatternFile,
			Reason: "test-only fixture", Source: machinecontract.ScopeRuleUser,
			CreatedBy: "stale-baseline-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == "unauthored.go" {
			found = true
			if reduction.AuthoringState != "missing" {
				t.Fatalf("an existing unauthored source is missing authoring debt, got %q", reduction.AuthoringState)
			}
		}
	}
	if !found {
		t.Fatal("dropping an existing source that still owes an Entry is a real coverage reduction and must stay reported")
	}
	if !preview.Plan.Risk.CognitionCoverageReduction {
		t.Fatal("a real coverage reduction must still raise its risk")
	}
}
