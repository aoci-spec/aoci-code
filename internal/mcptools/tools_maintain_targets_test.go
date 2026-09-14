package mcptools

import (
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// enableProductionManagedScope re-baselines a Volumes fixture under the
// production Managed Scope profile, the governance every aoci init repository
// runs with, so drift classification goes through the role-aware detector.
func enableProductionManagedScope(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &policy
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		fingerprint.Role = machinecontract.ScopeRoleIndex
		snapshot[rel] = fingerprint
	}
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value := baseline.NewBaseline(snapshot)
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: policy.ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
}

// A file created after the last scan has no Entry and no Baseline fingerprint,
// so the detector files it under both Missing and Unbaselined. Maintain's
// authoring batch used to add the two lists and promise twice the work its
// own candidate list carried: two fresh files answered total_targets 4,
// remaining 2, continuation_required true, while the plan held exactly two
// candidates and the next Maintain would have found nothing left.
func TestVolumeMaintainCountsAFreshUnscannedFileOnce(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	enableProductionManagedScope(t, root)
	session := connectMCPClient(t, root)
	if aligned := maintainVolumeBatch(t, session); aligned.Status != autoStatusApplied || !aligned.Aligned {
		t.Fatalf("fixture must start aligned: %#v", aligned)
	}
	writeVolumeTestFile(t, root, "extra_a.go", "package main\n\nconst ExtraA = 1\n")
	writeVolumeTestFile(t, root, "extra_b.go", "package main\n\nconst ExtraB = 2\n")

	maintain := maintainVolumeBatch(t, session)
	if maintain.Status != autoStatusRepairRequired || len(maintain.Candidates) != 2 {
		t.Fatalf("expected two fresh candidates: %#v", maintain)
	}
	if maintain.Batch.TotalTargets != 2 || maintain.Batch.Included != 2 || maintain.Batch.Remaining != 0 ||
		maintain.Batch.ContinuationRequired {
		t.Fatalf("a file that is both Missing and Unbaselined must count once: %#v", maintain.Batch)
	}
	if maintain.CodePlan == nil || maintain.CodePlan.TotalTargets != maintain.Batch.TotalTargets {
		t.Fatalf("authoring batch and code plan disagree on the total: %#v vs %#v", maintain.Batch, maintain.CodePlan)
	}
}
