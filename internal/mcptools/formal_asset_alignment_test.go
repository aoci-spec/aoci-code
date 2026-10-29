package mcptools

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
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// An rc22 Baseline that carries the Root and the Meta Volume as Observe
// evidence (a *.txt observe rule at first scan). Governance calls the
// repository aligned; the post-Apply inspector that every aoci_update_entry
// result reads must say the same, or each batch reports aligned=false with
// observed_removed findings that no Maintain lists and an optimization run
// stops on (the rc23 pre-tag audit, fourth reader of #158).
func TestVolumeAlignmentInspectorIgnoresFormalObserveRecords(t *testing.T) {
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
	cfg.ManagedScope.Rules = append(cfg.ManagedScope.Rules, managedscope.Rule{RuleID: "txt-observe", Action: machinecontract.ScopeRoleObserve,
		Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob, Reason: "recorded by rc22", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "test", Order: 1, Enabled: true})
	if err := config.Save(root, cfg); err != nil {
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
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt"} {
		recorded := files[rel]
		recorded.Role = machinecontract.ScopeRoleObserve
		files[rel] = recorded
	}
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
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Result != volumegovernance.ResultAligned || len(facts.CodeDrift.ObservedRemoved) != 0 {
		t.Fatalf("governance is the reference and it is not aligned: result=%s removed=%v", facts.Result, facts.CodeDrift.ObservedRemoved)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatalf("load: %+v", fail)
	}
	outcome := &AtomicBatchOutcome{Volumes: []string{"code"}, Items: []*UpdateOutcome{{Action: "update", Rel: "main.go"}}, BaselineComplete: true}
	aligned, remaining, findings, _ := inspectCognitionVolumeAlignment(root, "test", loaded, outcome)
	if !aligned || remaining != 0 || strings.Contains(strings.Join(findings, " "), "observed_removed") {
		t.Fatalf("the post-Apply inspector disagrees with governance: aligned=%t remaining=%d findings=%v", aligned, remaining, findings)
	}
}
