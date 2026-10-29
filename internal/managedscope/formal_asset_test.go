package managedscope

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A rule that matches the Root or a Volume changes nothing: the formal asset
// keeps the index role with no matched rule, the same rule still governs
// business files, and a git-hidden formal asset still reads as excluded so
// scan keeps refusing to publish a Baseline that omits it (#158).
func TestFormalAssetsAreOutsidePolicyRules(t *testing.T) {
	policy := Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: machinecontract.ScopeProfileProduction,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, Rules: []Rule{
			{RuleID: "volume-observe", Action: machinecontract.ScopeRoleObserve, Pattern: "aoci.code.txt", PatternKind: machinecontract.ScopePatternFile,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true},
			{RuleID: "text-exclude", Action: machinecontract.ScopeRoleExclude, Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 2, Enabled: true},
		}}
	for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		result := EvaluatePath(policy, path, false, false)
		if result.Role != machinecontract.ScopeRoleIndex || result.MatchedRule != nil || result.RuleSource != machinecontract.ScopeRuleBuiltin ||
			!result.EntersWholeIndex || result.EntersObserveFingerprint {
			t.Fatalf("a rule reached the formal asset %s: %+v", path, result)
		}
	}
	if result := EvaluatePath(policy, "notes.txt", false, false); result.Role != machinecontract.ScopeRoleExclude || result.MatchedRule == nil {
		t.Fatalf("the glob rule stopped governing business files: %+v", result)
	}
	if result := EvaluatePath(policy, "aoci.code.txt", true, false); result.Role != machinecontract.ScopeRoleExclude {
		t.Fatalf("a git-hidden Volume no longer reads as excluded: %+v", result)
	}
}
