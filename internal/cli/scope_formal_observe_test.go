package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// The shape #158 reported: a persisted rule gave aoci.code.txt the observe
// role and a Scope Change recorded it so in the Baseline. Status, governance,
// and a Plan must agree that nothing is pending, the persisted rule must no
// longer reach the Volume, and an exact rule naming one is refused.
func TestFormalVolumeRecordedAsObserveDoesNotDeadlockManagedScope(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: formal observe fixture\n#Global-Invariants: deterministic fixture bytes\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope.ObserveChangePolicy = machinecontract.ObserveChangeReviewRequired
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	// The rule rc22 let a team persist, and apply.
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "volume-observe", Action: machinecontract.ScopeRoleObserve,
			Pattern: "aoci.code.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "recorded by rc22",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range evaluation.Observe {
		if item.Path == "aoci.code.txt" {
			t.Fatalf("the persisted rule still reaches the Volume: %+v", item)
		}
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	recorded := files["aoci.code.txt"]
	recorded.Role = machinecontract.ScopeRoleObserve
	files["aoci.code.txt"] = recorded
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: cfg.CognitionBudget}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}

	status, err := buildScopePolicyStatus(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if status.Stage != "aligned" || status.ObservedPendingReview != 0 || status.Drift == nil || len(status.Drift.ObservedRemoved) != 0 {
		t.Fatalf("status still sees the Volume as Observe evidence: %+v", status)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Result != volumegovernance.ResultAligned || facts.ManagedScope.ObservedPendingReview != 0 || len(facts.CodeDrift.ObservedRemoved) != 0 {
		t.Fatalf("governance still reports the Volume as observed_removed: result=%s pending=%d removed=%v findings=%+v",
			facts.Result, facts.ManagedScope.ObservedPendingReview, facts.CodeDrift.ObservedRemoved, facts.Findings)
	}
	empty := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}}
	if _, err := scopechange.Build(root, "2026-10-29T00:00:00Z", empty); err != nil {
		t.Fatalf("a Plan still demands review of the Volume: %v", err)
	}
	output, err := runScopeCLI(t, root, "rule", "add", "pin-volume", "--action", "observe", "--pattern", "aoci.code.txt",
		"--pattern-kind", "file", "--reason", "test")
	if err == nil || !strings.Contains(err.Error(), "managed_scope_rule_targets_formal_asset: aoci.code.txt") {
		t.Fatalf("an exact rule naming a Volume was accepted: err=%v output=%s", err, output)
	}
	// The way out for a repository already in this state: activation applies
	// with nothing to approve and writes the Volume's role back.
	output, err = runScopeCLI(t, root, "activate")
	if err != nil || !strings.Contains(string(output), `"status": "applied"`) {
		t.Fatalf("activation did not repair the recorded role: err=%v output=%s", err, output)
	}
	after, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline after activation: exists=%t err=%v", exists, err)
	}
	if after.Files["aoci.code.txt"].Role != machinecontract.ScopeRoleIndex {
		t.Fatalf("the Volume's Baseline record still carries %q", after.Files["aoci.code.txt"].Role)
	}
	if status, err := buildScopePolicyStatus(root, true); err != nil || status.Stage != "aligned" || status.ObservedPendingReview != 0 {
		t.Fatalf("status after activation: %+v err=%v", status, err)
	}
}
