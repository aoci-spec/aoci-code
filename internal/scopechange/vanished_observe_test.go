// A vanished observe source must be reported as removed, even while Git still tracks it.
//
// A real experience: after deleting a tracked observe test file, scope acknowledge could never succeed.
// scope status judges by snapshot and reports observed_removed truthfully; Build judges by roles, and
// a "deleted but still tracked" file is still evaluated by the Safe Inventory as an unsafe_filesystem_object
// Exclude item: it stays in roles, so it reads like "reclassified by policy", not like "vanished".
// The two lists therefore never agree, acknowledge refuses with observed_evidence_review_required,
// and the error says neither what is extra nor what is missing. The operator has no action that clears it.
//
// The roles criterion itself is right: it separates "reclassified by policy" (still in roles, reviewed by the plan) from "vanished"
// (not in roles). What was missing was only to stop a vanished path from posing as the former.
package scopechange

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// trackAndVanish puts the fixture into the state "tracked by Git, recorded as observe in the Baseline, but deleted in the working tree";
// only when all three hold at once is it that blind spot.
func trackAndVanish(t *testing.T, root, rel string) {
	t.Helper()
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint, recorded := state.Files[rel]
	if !recorded {
		t.Fatalf("fixture precondition: %s must be in the Baseline", rel)
	}
	fingerprint.Role = machinecontract.ScopeRoleObserve
	state.Files[rel] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", root, "add", "-A")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

func vanishedFixtureFacts(t *testing.T, root string) (*managedscope.Evaluation, map[string]baseline.Fingerprint, map[string]string, *baseline.Baseline, *config.Config) {
	t.Helper()
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	active, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: cfg.CurationExclude})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	return evaluation, snapshot, evaluationRoles(evaluation), active, cfg
}

func TestVanishedButTrackedObserveSourceIsReportedRemoved(t *testing.T) {
	root, _ := buildChangeFixture(t)
	trackAndVanish(t, root, "main_test.go")
	evaluation, snapshot, roles, active, cfg := vanishedFixtureFacts(t, root)

	// Precondition: it really is still in the evaluation rather than simply gone; otherwise this case cannot reach that blind spot.
	if _, governed := roles["main_test.go"]; !governed {
		t.Fatal("fixture precondition: a tracked-but-deleted path must still be evaluated, " +
			"otherwise the blind spot this pins cannot occur")
	}
	vanished := vanishedPaths(evaluation)
	if !vanished["main_test.go"] {
		t.Fatalf("a governed path the inventory cannot find must be named vanished: %v", vanished)
	}
	changes := observedEvidenceChanges(active, snapshot, roles, vanished, true, cfg.LineEndingTolerance)
	found := false
	for _, path := range changes {
		if path == "main_test.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a vanished observe source must be reported removed, got %v", changes)
	}
}

// The property really being locked is not this one symptom but that the two computations must not diverge: acknowledge submits the review with
// status's list, Build validates with its own list, and as long as the two differ they can never match.
func TestBuildAndStatusAgreeOnObserveRemovals(t *testing.T) {
	root, _ := buildChangeFixture(t)
	trackAndVanish(t, root, "main_test.go")
	evaluation, snapshot, roles, active, cfg := vanishedFixtureFacts(t, root)

	raw, err := os.ReadFile(filepath.Join(root, cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	document, _ := index.Parse(string(raw))
	index.ResolveRelPaths(document, root)
	drift := baseline.DetectManagedScope(root, document, active, snapshot,
		afs.WalkOptions{HighRiskOptIn: cfg.SafeInventoryHighRiskOptIn}, cfg.LineEndingTolerance)
	if len(drift.ObservedRemoved) == 0 {
		t.Fatal("fixture precondition: status must see a removal for this comparison to mean anything")
	}

	changes := map[string]bool{}
	for _, path := range observedEvidenceChanges(active, snapshot, roles, vanishedPaths(evaluation), true, cfg.LineEndingTolerance) {
		changes[path] = true
	}
	for _, path := range drift.ObservedRemoved {
		if !changes[path] {
			t.Fatalf("status reports %q removed but the Scope Change plan does not; the review set "+
				"submitted by scope acknowledge can then never match and the repository is stuck",
				path)
		}
	}
}
