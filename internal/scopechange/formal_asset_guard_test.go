package scopechange

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// A Volume's Baseline record is a guard: whatever role a rule once recorded
// there, the guard carries index, so the next Apply writes the role back and
// the Plan never asks for the Volume to be reviewed as Observe evidence.
func TestFormalVolumeGuardsCarryTheIndexRole(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: guard role fixture\n#Global-Invariants: -\n"+
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
	active := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if rel == "aoci.code.txt" {
			fingerprint.Role = machinecontract.ScopeRoleObserve
		}
		active.Files[rel] = fingerprint
	}
	guards, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if guards[rel].Role != machinecontract.ScopeRoleIndex || guards[rel].SHA256 != active.Files[rel].SHA256 {
			t.Fatalf("guard for %s does not carry the index role over the stored bytes: %+v", rel, guards[rel])
		}
	}
}

// legacyScopeFixture builds a Legacy-layout Managed Scope repository whose
// Baseline was written by rc22: mutate edits the snapshot the way rc22's
// rule-governed evaluation recorded it before it is saved.
func legacyScopeFixture(t *testing.T, profile string, rules []managedscope.Rule, mutate func(files map[string]baseline.Fingerprint)) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeChangeFixture(t, root, "main.go", "package sample\n")
	writeChangeFixture(t, root, "aoci.txt", "# test index\n==="+filepath.ToSlash(root)+"/===\n"+
		"main.go[CG5T]: F:production | R:- | A:- | S:-\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(profile); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope.Rules = append(cfg.ManagedScope.Rules, rules...)
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
	mutate(files)
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	mode, err := EffectiveApplyAuthorizationMode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: cfg.EffectiveManagedScope().ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: cfg.CognitionBudget, ApplyAuthorizationMode: mode}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	return root
}

// The formal-asset pin must not reach a Legacy index file: rc22 evaluated
// aoci.txt by the rules (excluded under a custom profile with no matching
// rule, observed under a *.txt observe rule) and the Baseline recorded that.
// A Plan over that Baseline builds without demanding an Entry for aoci.txt,
// which Legacy Maintain never plans, so the repository stays aligned on
// upgrade instead of answering managed_scope_authoring_candidate_required.
func TestLegacyIndexFileNeedsNoAuthoringCandidateOnUpgrade(t *testing.T) {
	empty := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
	cases := map[string]string{
		"custom profile, no rule matches aoci.txt": legacyScopeFixture(t, machinecontract.ScopeProfileCustom, []managedscope.Rule{
			{RuleID: "go-sources", Action: machinecontract.ScopeRoleIndex, Pattern: "**/*.go", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "custom", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true},
		}, func(files map[string]baseline.Fingerprint) { delete(files, "aoci.txt") }),
		"production profile, *.txt observe rule": legacyScopeFixture(t, machinecontract.ScopeProfileProduction, []managedscope.Rule{
			{RuleID: "txt-observe", Action: machinecontract.ScopeRoleObserve, Pattern: "*.txt", PatternKind: machinecontract.ScopePatternGlob,
				Reason: "observe text", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true},
		}, func(files map[string]baseline.Fingerprint) {
			recorded := files["aoci.txt"]
			recorded.Role = machinecontract.ScopeRoleObserve
			files["aoci.txt"] = recorded
		}),
	}
	for name, root := range cases {
		preview, err := Build(root, "2026-10-29T00:00:00Z", empty)
		if err != nil {
			t.Fatalf("%s: the Plan demands what no command can author: %v", name, err)
		}
		// The Scope transaction has always re-fingerprinted the Legacy index
		// file it rewrites as index (the observe fingerprint delta is rc22
		// behaviour); what must not appear is a role change or an added
		// index object for aoci.txt, the pin's signature.
		moved, err := json.Marshal([]any{preview.Plan.IndexAdded, preview.Plan.RoleChanges, preview.Plan.BaselineAdded})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(moved), `"aoci.txt"`) {
			t.Fatalf("%s: the Plan moves the Legacy index file: %s", name, moved)
		}
	}
}
