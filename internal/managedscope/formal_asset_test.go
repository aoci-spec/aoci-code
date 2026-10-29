package managedscope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func formalAssetPolicy() Policy {
	return Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: machinecontract.ScopeProfileProduction,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, Rules: []Rule{
			{RuleID: "volume-observe", Action: machinecontract.ScopeRoleObserve, Pattern: "aoci.code.txt", PatternKind: machinecontract.ScopePatternFile,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true},
			{RuleID: "text-exclude", Action: machinecontract.ScopeRoleExclude, Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 2, Enabled: true},
		}}
}

// A rule that matches the Root or a Volume changes nothing: the formal asset
// keeps the index role with no matched rule, the same rule still governs
// business files, and a git-hidden formal asset still reads as excluded so
// scan keeps refusing to publish a Baseline that omits it (#158).
func TestFormalAssetsAreOutsidePolicyRules(t *testing.T) {
	policy := formalAssetPolicy()
	for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		result := EvaluatePath(policy, cognition.LayoutVolumesV1, path, false, false)
		if result.Role != machinecontract.ScopeRoleIndex || result.MatchedRule != nil || result.RuleSource != machinecontract.ScopeRuleBuiltin ||
			!result.EntersWholeIndex || result.EntersObserveFingerprint {
			t.Fatalf("a rule reached the formal asset %s: %+v", path, result)
		}
	}
	if result := EvaluatePath(policy, cognition.LayoutVolumesV1, "notes.txt", false, false); result.Role != machinecontract.ScopeRoleExclude || result.MatchedRule == nil {
		t.Fatalf("the glob rule stopped governing business files: %+v", result)
	}
	if result := EvaluatePath(policy, cognition.LayoutVolumesV1, "aoci.code.txt", true, false); result.Role != machinecontract.ScopeRoleExclude {
		t.Fatalf("a git-hidden Volume no longer reads as excluded: %+v", result)
	}
}

// The pin is a Volumes v1 fact. Under the Legacy layout aoci.txt is the index
// itself and stays the rule-governed candidate rc22 evaluated: pinning it
// would demand an Entry that Legacy Maintain never plans, and an rc22-aligned
// repository would answer managed_scope_authoring_candidate_required with no
// command that clears it (the rc23 pre-tag audit).
func TestLegacyIndexFileStaysRuleGoverned(t *testing.T) {
	policy := formalAssetPolicy()
	for _, layout := range []string{cognition.LayoutLegacyMonolithic, ""} {
		result := EvaluatePath(policy, layout, "aoci.txt", false, false)
		if result.Role != machinecontract.ScopeRoleExclude || result.MatchedRule == nil || result.RuleSource == machinecontract.ScopeRuleBuiltin {
			t.Fatalf("layout %q: the glob rule no longer governs the Legacy index file: %+v", layout, result)
		}
	}
}

// Build takes the layout from the Root file at the repository root, so the
// same policy evaluates aoci.txt as index under a Volumes Root and by its
// rules under a Legacy index.
func TestBuildPinsFormalAssetsOnlyUnderVolumes(t *testing.T) {
	policy := Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: machinecontract.ScopeProfileProduction,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, Rules: []Rule{
			{RuleID: "text-observe", Action: machinecontract.ScopeRoleObserve, Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "test", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true}}}
	roleOf := func(t *testing.T, root, rel string) (string, string) {
		t.Helper()
		evaluation, err := Build(root, policy, BuildOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, items := range [][]PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
			for _, item := range items {
				if item.Path == rel {
					return item.Role, item.RuleSource
				}
			}
		}
		t.Fatalf("%s is not in the evaluation of %s", rel, root)
		return "", ""
	}
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "aoci.txt"), []byte("#AOCI-INDEX: 1\n==="+filepath.ToSlash(legacy)+"/===\nmain.go[CG5T]: F:main | R:- | A:- | S:-\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if role, source := roleOf(t, legacy, "aoci.txt"); role != machinecontract.ScopeRoleObserve || source == machinecontract.ScopeRuleBuiltin {
		t.Fatalf("Legacy index file: role=%s source=%s", role, source)
	}
	volumes := t.TempDir()
	if err := os.WriteFile(filepath.Join(volumes, "aoci.txt"), []byte(cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volumes, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if role, source := roleOf(t, volumes, "aoci.txt"); role != machinecontract.ScopeRoleIndex || source != machinecontract.ScopeRuleBuiltin {
		t.Fatalf("Volumes Root: role=%s source=%s", role, source)
	}
	if cognition.RootLayout(t.TempDir()) != "" {
		t.Fatal("a root without aoci.txt reports a layout")
	}
}
