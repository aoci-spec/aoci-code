package cli

import (
	"encoding/json"
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

// projectionFixture builds a Volumes repository whose Code Volume knows
// main.go only, beside two held index-role sources (a binary and an empty
// file) and, with debt, a text source without an Entry plus a stale main.go.
func projectionFixture(t *testing.T, withDebt bool) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, content []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", []byte(cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: projection fixture\n#Global-Invariants: deterministic fixture bytes\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"))
	write("aoci.meta.txt", []byte(cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"))
	write("main.go", []byte("package main\n"))
	write("logo.png", []byte("\x89PNG\x00\x00not text"))
	write("empty.txt", []byte{})
	if withDebt {
		write("extra.go", []byte("package main\n\nfunc extra() {}\n"))
	}
	write("aoci.code.txt", []byte(cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n"))
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
	if withDebt {
		write("main.go", []byte("package main\n// changed after the Baseline\n"))
	}
	return root, cfg
}

// scope status and verify read one Code drift projection: the lists agree
// path for path, held sources are skipped on both and never authoring
// targets, and a tree whose only Entry-less index files are held is aligned
// for both, where scope status alone used to say authoring_required.
func TestScopeStatusReadsTheGovernanceProjection(t *testing.T) {
	for _, withDebt := range []bool{true, false} {
		root, cfg := projectionFixture(t, withDebt)
		output, err := runScopeCLI(t, root, "status")
		if err != nil {
			t.Fatalf("scope status: %v: %s", err, output)
		}
		var status scopePolicyStatus
		if err := json.Unmarshal(output, &status); err != nil {
			t.Fatalf("decode %s: %v", output, err)
		}
		set, err := cognition.Load(root, cfg.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := volumegovernance.Assess(root, cfg, set)
		if err != nil {
			t.Fatal(err)
		}
		if status.Drift == nil {
			t.Fatalf("withDebt=%t: scope status carried no drift: %s", withDebt, output)
		}
		if !reflect.DeepEqual(*status.Drift, facts.CodeDrift) {
			t.Fatalf("withDebt=%t: scope status drift differs from governance:\n status %+v\n verify %+v", withDebt, *status.Drift, facts.CodeDrift)
		}
		if status.AuthoringTargets != len(facts.CodeDrift.Missing) {
			t.Fatalf("withDebt=%t: authoring_targets %d, governance missing %v", withDebt, status.AuthoringTargets, facts.CodeDrift.Missing)
		}
		skipped := map[string]bool{}
		for _, rel := range facts.CodeDrift.Skipped {
			skipped[rel] = true
		}
		if !skipped["logo.png"] || !skipped["empty.txt"] {
			t.Fatalf("withDebt=%t: held sources are not reported as skipped: %v", withDebt, facts.CodeDrift.Skipped)
		}
		if withDebt {
			if !reflect.DeepEqual(facts.CodeDrift.Missing, []string{"extra.go"}) || !reflect.DeepEqual(facts.CodeDrift.Stale, []string{"main.go"}) ||
				status.Stage != "authoring_required" || facts.Result == volumegovernance.ResultAligned {
				t.Fatalf("debt fixture misread: missing=%v stale=%v stage=%s result=%s", facts.CodeDrift.Missing, facts.CodeDrift.Stale, status.Stage, facts.Result)
			}
			continue
		}
		if status.Stage != "aligned" || facts.Result != volumegovernance.ResultAligned || status.AuthoringTargets != 0 {
			t.Fatalf("held-only tree: scope status stage=%s targets=%d, governance result=%s", status.Stage, status.AuthoringTargets, facts.Result)
		}
	}
}
