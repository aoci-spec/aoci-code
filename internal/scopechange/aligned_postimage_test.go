package scopechange

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// A Plan that moves nothing keeps the Baseline's bytes, updated_at included:
// an activation a second after the last Apply used to re-stamp the timestamp,
// so an aligned repository under the review posture asked for approval.
func TestAlignedPlanKeepsTheBaselineBytes(t *testing.T) {
	root, _ := buildLegacyDatabaseBootstrapScopeFixture(t)
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "no-op-aligned", Action: machinecontract.ScopeRoleExclude,
			Pattern: "future-never-present.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "test aligned postimage",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "scope-test", Order: 100, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	empty := func() CandidateSet {
		return CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1, Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
	}
	first, err := Build(root, "2026-08-12T00:00:00Z", empty())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Apply(root, first, nil); err != nil || result.Status != "applied" {
		t.Fatalf("policy change did not apply: %#v %v", result, err)
	}
	applied, _, err := baseline.Load(root)
	if err != nil || applied.UpdatedAt != "2026-08-12T00:00:00Z" {
		t.Fatalf("a real change keeps its own timestamp: %q %v", applied.UpdatedAt, err)
	}
	again, err := Build(root, "2026-08-12T01:00:00Z", empty())
	if err != nil {
		t.Fatal(err)
	}
	image := again.BaselinePostimage
	if image.PreimageSHA256 != image.PostimageSHA256 || again.Baseline.UpdatedAt != applied.UpdatedAt {
		t.Fatalf("an aligned Plan re-stamped the Baseline: updated_at %q -> %q", applied.UpdatedAt, again.Baseline.UpdatedAt)
	}
}
