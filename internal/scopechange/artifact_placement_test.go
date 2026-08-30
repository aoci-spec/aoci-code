package scopechange

import (
	"os"
	"path/filepath"
	"testing"
)

// The published remediation tells an operator where to write the preview and
// approval artifacts, and that location decides whether the sequence completes.
// A plan binds the repository state, so an artifact written into the governed
// worktree between preview and apply changes the state the plan was minted
// against and the approval no longer matches it: apply refuses with
// managed_scope_replay_mismatch. The first attempt at a runnable remediation
// prescribed the repository root and therefore still did not run.
//
// .aoci is excluded from the Safe Inventory unconditionally, so artifacts kept
// there are invisible to the plan.
func TestPrescribedArtifactLocationDoesNotInvalidateThePlan(t *testing.T) {
	root, candidates, first := buildAutoPreview(t)
	baseline := first.Plan.PlanID
	if baseline == "" {
		t.Fatal("fixture precondition: the preview must carry a plan identity")
	}

	// Returns the plan identity, or the empty string when Build refuses: a new
	// file in the governed worktree can become an authoring candidate and stop
	// the plan outright, which is the same failure from the operator's side.
	rebuild := func() string {
		t.Helper()
		preview, err := Build(root, authorizationTestTime, candidates)
		if err != nil {
			return ""
		}
		return preview.Plan.PlanID
	}

	// The prescribed location: inside .aoci, which the inventory never sees.
	prescribed := filepath.Join(root, ".aoci", "scope-change")
	if err := os.MkdirAll(prescribed, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"candidates.json", "preview.json", "approval.json"} {
		if err := os.WriteFile(filepath.Join(prescribed, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := rebuild(); got != baseline {
		t.Fatalf("an artifact at the prescribed location moved the plan identity: %s -> %s\n"+
			"The remediation would then mint an approval for a plan that no longer matches, "+
			"and apply refuses with managed_scope_replay_mismatch.", baseline, got)
	}

	// The control: the repository root is exactly what must not be prescribed.
	// If this ever stops moving the identity, the guarantee above is vacuous.
	stray := filepath.Join(root, "preview.json")
	if err := os.WriteFile(stray, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(stray)
	if got := rebuild(); got == baseline {
		t.Fatal("an artifact in the governed worktree no longer disturbs the plan; " +
			"this test can no longer tell a safe location from an unsafe one")
	}
}
