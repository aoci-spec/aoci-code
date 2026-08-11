package cognitionplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestManagedScopeOnboardingAuthorsOnlyIndexAndKeepsObserveAsEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	writeFile(t, root, "src/main_test.go", "package main\n")
	writeFile(t, root, "src/testdata/case.txt", "fixture\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
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
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	authoring := map[string]bool{}
	for _, task := range plan.AuthoringTasks {
		authoring[task.ObjectRef] = true
	}
	if !authoring["code:src/main.go"] || authoring["code:src/main_test.go"] || authoring["code:src/testdata/case.txt"] {
		t.Fatalf("Managed Scope authoring selection invalid: %+v", plan.AuthoringTasks)
	}
	inventory := map[string]InventoryObject{}
	for _, object := range plan.Inventory {
		inventory[object.Path] = object
	}
	if inventory["src/main_test.go"].ScopeRole != machinecontract.ScopeRoleObserve || inventory["src/main_test.go"].Eligible {
		t.Fatalf("observe test did not remain non-authoring evidence: %+v", inventory["src/main_test.go"])
	}
	if _, exists := inventory["src/testdata/case.txt"]; exists {
		t.Fatal("excluded fixture entered D2 inventory")
	}
	evidencePaths := map[string]bool{}
	for _, file := range plan.BusinessSourceManifest.Files {
		evidencePaths[file.Path] = true
	}
	if !evidencePaths["src/main_test.go"] || evidencePaths["src/testdata/case.txt"] {
		t.Fatalf("observe/exclude evidence selection invalid: %+v", plan.BusinessSourceManifest.OrderedPaths)
	}
	if _, present, err := InitialManagedScopeEvidenceFromPlan(plan); err != nil || present {
		t.Fatalf("an already-receipted persisted plan was upgraded to Fresh project evidence: present=%t err=%v", present, err)
	}
	for _, task := range plan.AuthoringTasks[:2] {
		if len(task.EvidenceRefs) != 0 {
			t.Fatalf("legacy-compatible Root/Meta task identity changed: %+v", task)
		}
	}
}

func TestFreshInitialManagedScopeBindsProjectGovernanceAndRootMetaEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	writeFile(t, root, "src/main_test.go", "package main\n")
	writeFile(t, root, "src/testdata/case.txt", "fixture\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	evidence, present, err := InitialManagedScopeEvidenceFromPlan(plan)
	if err != nil || !present {
		t.Fatalf("Fresh desired governance was not bound: evidence=%#v present=%t err=%v", evidence, present, err)
	}
	if evidence.PolicyIdentity == "" || evidence.BudgetPolicyIdentity == "" || evidence.AuthoringEvidenceIdentity == "" ||
		evidence.IndexEvidenceIdentity == "" || evidence.ObserveEvidenceIdentity == "" || evidence.HighRiskOptInIdentity == "" {
		t.Fatalf("initial Managed Scope evidence incomplete: %#v", evidence)
	}
	if evidence.ObserveChangePolicy != machinecontract.ObserveChangeReviewRequired {
		t.Fatalf("production governance was not preserved: %#v", evidence)
	}
	inventory := map[string]InventoryObject{}
	for _, object := range plan.Inventory {
		inventory[object.Path] = object
	}
	if !inventory["src/main.go"].Eligible || inventory["src/main.go"].ScopeRole != machinecontract.ScopeRoleIndex {
		t.Fatalf("Index role missing: %#v", inventory["src/main.go"])
	}
	if inventory["src/main_test.go"].Eligible || inventory["src/main_test.go"].ScopeRole != machinecontract.ScopeRoleObserve {
		t.Fatalf("Observe role missing: %#v", inventory["src/main_test.go"])
	}
	if _, exists := inventory["src/testdata/case.txt"]; exists {
		t.Fatal("Exclude role entered the authoring inventory")
	}
	for _, task := range plan.AuthoringTasks[:2] {
		joined := strings.Join(task.EvidenceRefs, "\n")
		for _, required := range []string{
			"inventory:" + plan.InventoryIdentity,
			"source-evidence:" + plan.SourceEvidenceIdentity,
			"authoring-evidence:" + evidence.AuthoringEvidenceIdentity,
			"managed-scope:" + evidence.PolicyIdentity,
			"cognition-budget:" + evidence.BudgetPolicyIdentity,
			"index-evidence:" + evidence.IndexEvidenceIdentity,
			"observe-evidence:" + evidence.ObserveEvidenceIdentity,
			"observe-change-policy:" + evidence.ObserveChangePolicy,
			"high-risk-opt-in:" + evidence.HighRiskOptInIdentity,
		} {
			if !strings.Contains(joined, required) {
				t.Fatalf("%s task lacks project evidence %q: %+v", task.TaskID, required, task.EvidenceRefs)
			}
		}
	}
	if got := plan.SemanticAuthoringRequirement.EvidenceBindingSHA256; got != SemanticAuthoringEvidenceBindingSHA256(plan) {
		t.Fatalf("semantic provenance did not bind project evidence: %s", got)
	}
}

func TestFreshInitialManagedScopeCannotActivateWithoutCodeTargets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCandidate(root, plan, validCandidate(t, root, plan)); err == nil ||
		!strings.Contains(err.Error(), "initial_managed_scope_code_target_required") {
		t.Fatalf("Root/Meta-only Candidate could fingerprint unauthored Code objects: %v", err)
	}
}

func TestFreshInitialManagedScopeDriftSupersedesPlanAndHighRiskReadFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "main_test.go", "package main\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := validCandidate(t, root, plan)

	writeFile(t, root, "main_test.go", "package main\n// changed observed evidence\n")
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerSuperseded {
		t.Fatalf("Observe drift did not supersede the Plan: preview=%#v err=%v", preview, err)
	}

	writeFile(t, root, "main_test.go", "package main\n")
	cfg.CognitionBudget.WholeIndex.MaxTokens++
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err = ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerSuperseded {
		t.Fatalf("Budget drift did not supersede the Plan: preview=%#v err=%v", preview, err)
	}

	cfg.CognitionBudget.WholeIndex.MaxTokens--
	cfg.SafeInventoryHighRiskOptIn = []string{".env"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	withoutSecret, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	originalEvidence, _, _ := InitialManagedScopeEvidenceFromPlan(plan)
	highRiskEvidence, present, err := InitialManagedScopeEvidenceFromPlan(withoutSecret)
	if err != nil || !present || highRiskEvidence.HighRiskOptInIdentity == originalEvidence.HighRiskOptInIdentity {
		t.Fatalf("high-risk exception identity was not bound: evidence=%#v present=%t err=%v", highRiskEvidence, present, err)
	}
	writeFile(t, root, ".env", "TEST_ONLY_SECRET=must-not-be-read\n")
	if _, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}}); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_high_risk_read_approval_required: .env") {
		t.Fatalf("selected high-risk content did not fail closed before authoring: %v", err)
	}
}

func TestPlanIdentityBindsBaselineCurationTargetAndCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	base, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	writeBaseline(t, root)
	withBaseline, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil || withBaseline.PlanID == base.PlanID {
		t.Fatalf("Baseline drift did not invalidate Plan: err=%v", err)
	}
	writeFile(t, root, ".aoci/curation.json", "{\"version\":1,\"decisions\":[]}\n")
	withCuration, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil || withCuration.PlanID == withBaseline.PlanID {
		t.Fatalf("Curation drift did not invalidate Plan: err=%v", err)
	}
	rootMetaOnly, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
	if err != nil || rootMetaOnly.PlanID == withCuration.PlanID {
		t.Fatalf("target set drift did not invalidate Plan: err=%v", err)
	}
	candidate := validCandidate(t, root, withCuration)
	first, err := ValidateCandidate(root, withCuration, candidate)
	if err != nil || first.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("first candidate invalid: preview=%#v err=%v", first, err)
	}
	candidate.Assets[0].Content = strings.Replace(candidate.Assets[0].Content, "fixture project", "changed fixture project", 1)
	attachTestHostModelProvenance(withCuration, candidate)
	second, err := ValidateCandidate(root, withCuration, candidate)
	if err != nil || second.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("second candidate invalid: preview=%#v err=%v", second, err)
	}
	if first.PlanID == second.PlanID || first.ApprovalDigest.Digest == second.ApprovalDigest.Digest || first.PhysicalDiff.PhysicalDiffSHA256 == second.PhysicalDiff.PhysicalDiffSHA256 {
		t.Fatal("draft bytes did not supersede the candidate-bound Plan and Approval Digest")
	}
}

func TestRepresentativeReactNodeMySQLPilotFixture(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join("testdata", "pilot-react-node-mysql")
	err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(sourceRoot, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		writeFile(t, root, filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	installDatabaseEvidence(t, root)
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code", "database"}})
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{"frontend/src/App.jsx", "backend/src/api.js", "backend/src/service.js"}
	for _, path := range wanted {
		found := false
		for _, task := range plan.AuthoringTasks {
			if task.ObjectRef == "code:"+path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("representative Pilot target missing: %s", path)
		}
	}
	if plan.NetworkAccessed || len(plan.Evidence) != 1 || !containsString(plan.TargetKinds, "database") {
		t.Fatalf("Pilot database boundary changed: %#v", plan)
	}
}
