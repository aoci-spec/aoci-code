package mcptools

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// The post-Apply inspector reports exactly the drift governance reports, in
// its own order (stale, orphan, unbaselined, missing), and a held source
// (a binary without an Entry) appears in neither.
func TestVolumeAlignmentInspectorReadsTheGovernanceProjection(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, content []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", []byte(cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: inspector projection fixture\n#Global-Invariants: deterministic fixture bytes\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"))
	write("aoci.meta.txt", []byte(cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"))
	write("main.go", []byte("package main\n"))
	write("extra.go", []byte("package main\n\nfunc extra() {}\n"))
	write("logo.png", []byte("\x89PNG\x00\x00not text"))
	write("aoci.code.txt", []byte(cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n"+
		"gone.go[CD7T]: F:an Entry whose source left | R:- | A:- | S:-\n"))
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
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
	write("main.go", []byte("package main\n// changed after the Baseline\n"))
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{}
	for _, item := range []struct {
		prefix string
		paths  []string
	}{{"stale: ", facts.CodeDrift.Stale}, {"orphan: ", facts.CodeDrift.Orphan}, {"unbaselined: ", facts.CodeDrift.Unbaselined}, {"missing: ", facts.CodeDrift.Missing}} {
		for _, rel := range item.paths {
			expected = append(expected, item.prefix+rel)
		}
	}
	if len(expected) != 3 || len(facts.CodeDrift.Skipped) != 1 || facts.CodeDrift.Skipped[0] != "logo.png" {
		t.Fatalf("fixture misread by governance: expected=%v skipped=%v", expected, facts.CodeDrift.Skipped)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatalf("load: %+v", fail)
	}
	outcome := &AtomicBatchOutcome{Volumes: []string{"code"}, Items: []*UpdateOutcome{{Action: "update", Rel: "main.go"}}, BaselineComplete: true}
	aligned, remaining, findings, _ := inspectCognitionVolumeAlignment(root, "test", loaded, outcome)
	// The Code Volume's own fingerprint is unchanged, so the only findings are
	// the projection's drift lines.
	if aligned || remaining != len(expected) || !reflect.DeepEqual(findings, expected) {
		t.Fatalf("the inspector disagrees with governance:\n inspector aligned=%t remaining=%d findings=%v\n governance %v", aligned, remaining, findings, expected)
	}
}
