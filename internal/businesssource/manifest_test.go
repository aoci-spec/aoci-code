package businesssource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func writeManifestFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManifestDeterministicAndSafe(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	writeManifestFixture(t, root, "src/main.go", "package main\r\n")
	writeManifestFixture(t, root, "src/new.go", "package main\n")
	writeManifestFixture(t, root, ".env", "SECRET=never-hashed\n")
	writeManifestFixture(t, root, ".runtime/state.db", "runtime\n")
	writeManifestFixture(t, root, "aoci.txt", "formal\n")
	first, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(root, "2026-07-31T01:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.AggregateSHA256 != second.AggregateSHA256 || len(first.Files) != 2 || first.GeneratedAt == second.GeneratedAt {
		t.Fatalf("audit time changed content identity or source selection is wrong: %#v %#v", first, second)
	}
	for _, path := range first.OrderedPaths {
		if path == ".env" || path == ".runtime/state.db" || path == "aoci.txt" {
			t.Fatalf("unsafe or formal asset entered business manifest: %s", path)
		}
	}
	if first.NetworkAccessed {
		t.Fatal("business source manifest accessed network")
	}
}

func TestManagedManifestKeepsObserveEvidenceAndNeverReadsExclude(t *testing.T) {
	root := t.TempDir()
	writeManifestFixture(t, root, "src/main.go", "package main\n")
	writeManifestFixture(t, root, "src/main_test.go", "package main\n")
	writeManifestFixture(t, root, "src/testdata/secret.txt", "excluded fixture\n")
	writeManifestFixture(t, root, "aoci.txt", "# formal\n==="+filepath.ToSlash(root)+"/===\n")
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
	manifest, err := Build(root, "")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range manifest.Files {
		paths[file.Path] = true
	}
	if !paths["src/main.go"] || !paths["src/main_test.go"] || paths["src/testdata/secret.txt"] {
		t.Fatalf("managed Business Source role selection invalid: %+v", manifest.OrderedPaths)
	}
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Profile = machinecontract.ScopeProfileFull
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, ""); err == nil || err.Error() != "business_source_scope_change_required" {
		t.Fatalf("direct policy edit was used as active evidence scope: %v", err)
	}
}
