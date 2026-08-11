package cognitionbaseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestBuildVolumePostimageClosesInitialManagedScopeInOneBaseline(t *testing.T) {
	root := t.TempDir()
	writeBaselineSource(t, root, "main.go", "package main\n")
	writeBaselineSource(t, root, "main_test.go", "package main\n")
	writeBaselineSource(t, root, "testdata/case.txt", "excluded fixture\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.BootstrapPlan(cognitionplan.Options{
		RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, present, err := cognitionplan.InitialManagedScopeEvidenceFromPlan(plan)
	if err != nil || !present {
		t.Fatalf("project governance evidence missing: evidence=%#v present=%t err=%v", evidence, present, err)
	}
	assets := map[string]cognitionplan.CandidateAsset{
		"code": {AssetID: "code", Path: "aoci.code.txt", Content: "#AOCI-CODE-VOLUME: 1\n"},
	}
	value, _, err := BuildVolumePostimage(root, plan, nil, assets, "2026-08-11T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if value.ManagedScope == nil || value.ManagedScope.PolicyIdentity != evidence.PolicyIdentity ||
		value.ManagedScope.BudgetPolicyIdentity != evidence.BudgetPolicyIdentity ||
		value.ManagedScope.BudgetPolicy == nil || value.ManagedScope.BudgetPolicy.Mode != machinecontract.BudgetModeEnforce {
		t.Fatalf("initial Managed Scope receipt missing: %#v", value.ManagedScope)
	}
	if got := value.Files["main.go"].Role; got != machinecontract.ScopeRoleIndex {
		t.Fatalf("Index fingerprint role = %q", got)
	}
	if got := value.Files["main_test.go"].Role; got != machinecontract.ScopeRoleObserve {
		t.Fatalf("Observe fingerprint role = %q", got)
	}
	if _, exists := value.Files["testdata/case.txt"]; exists {
		t.Fatal("Exclude path entered the Bootstrap Baseline")
	}
	if _, exists := value.Files["aoci.code.txt"]; !exists {
		t.Fatal("formal Code Volume fingerprint missing")
	}

	writeBaselineSource(t, root, "main_test.go", "package main\n// drift\n")
	if _, _, err := BuildVolumePostimage(root, plan, nil, assets, "2026-08-11T00:00:00Z"); err == nil ||
		!strings.Contains(err.Error(), "inventory_guard_drift") {
		t.Fatalf("Observe source drift was not rejected: %v", err)
	}
}

func TestBuildVolumePostimageRejectsInitialManagedScopeWithoutCodeTarget(t *testing.T) {
	root := t.TempDir()
	writeBaselineSource(t, root, "main.go", "package main\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildVolumePostimage(root, plan, nil, map[string]cognitionplan.CandidateAsset{}, "2026-08-11T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "initial_managed_scope_code_target_required") {
		t.Fatalf("Baseline accepted unauthored Index-role files: %v", err)
	}
}

func writeBaselineSource(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
