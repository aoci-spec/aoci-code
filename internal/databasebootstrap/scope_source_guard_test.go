package databasebootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

func buildManagedScopeDatabaseBootstrapFixture(t *testing.T) string {
	t.Helper()
	root := codeOnlyFixture(t)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main_test.go", "package main\n")
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	budget, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope, cfg.CognitionBudget = &policy, &budget
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	budgetIdentity, err := cognitionbudget.Identity(budget)
	if err != nil {
		t.Fatal(err)
	}
	state := baseline.NewBaseline(files)
	state.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budget}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestObserveAcknowledgeAcceptsDatabaseBootstrapAndEvidenceBeforeScopePlan(t *testing.T) {
	root := buildManagedScopeDatabaseBootstrapFixture(t)
	beforeBootstrap, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load pre-Bootstrap Baseline: exists=%t err=%v", exists, err)
	}
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, preview); err != nil {
		t.Fatal(err)
	}
	rootAfterBootstrap, err := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	activeAfterBootstrap, exists, err := baseline.Load(root)
	if err != nil || !exists || activeAfterBootstrap.Files["aoci.txt"].SHA256 != rootAfterBootstrap.SHA256 {
		t.Fatalf("new Bootstrap did not advance the Root Baseline binding: exists=%t err=%v baseline=%#v", exists, err, activeAfterBootstrap)
	}
	// Recreate the exact on-disk state left by Database Bootstrap versions that
	// predated the Root/Baseline binding fix. Scope remains responsible for
	// recognizing this historical postimage without accepting arbitrary drift.
	activeAfterBootstrap.Files["aoci.txt"] = beforeBootstrap.Files["aoci.txt"]
	if err := baseline.UpdateDatabaseCognitionBinding(activeAfterBootstrap, baseline.DatabaseCognitionBinding{
		ObjectRef: "database://primary/public/users", SourceID: "primary", EvidenceVersion: dbevidence.EvidenceVersion,
		TableEvidenceSHA256: strings.Repeat("a", 64), EntrySHA256: strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, activeAfterBootstrap); err != nil {
		t.Fatal(err)
	}

	manifest, evidenceSnapshot, exists, err := dbevidence.LoadSnapshot(root, "primary")
	if err != nil || !exists || len(evidenceSnapshot.Tables) != 1 {
		t.Fatalf("load Evidence: exists=%t err=%v snapshot=%#v", exists, err, evidenceSnapshot)
	}
	table, err := dbevidence.LoadTableEvidence(root, evidenceSnapshot.Tables[0])
	if err != nil {
		t.Fatal(err)
	}
	table.Columns = append(table.Columns, dbevidence.Column{Ordinal: 2, Name: "email", NativeType: "varchar(255)", CanonicalType: "varchar", Nullable: false})
	changedSnapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, changedSnapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, changedSnapshot, changedSnapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}

	testPath := filepath.Join(root, "main_test.go")
	if err := os.WriteFile(testPath, []byte("package main\n// reviewed after Database lifecycle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	formalBefore := map[string][]byte{}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt", ".aoci/database-baseline.json"} {
		formalBefore[rel] = mustRead(t, filepath.Join(root, filepath.FromSlash(rel)))
	}
	candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: []string{"main_test.go"},
			ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: "database-bootstrap-source-guard-test"}}
	scopePreview, err := scopechange.Build(root, "2026-08-05T00:10:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if scopePreview.SourceGuard["aoci.txt"].SHA256 != rootAfterBootstrap.SHA256 {
		t.Fatal("Scope Plan did not bind the current post-Bootstrap Root")
	}
	for _, rel := range []string{"aoci.code.txt", "aoci.database.txt"} {
		if scopePreview.SourceGuard[rel].SHA256 == "" ||
			scopePreview.Baseline.Files[rel] != activeAfterBootstrap.Files[rel] {
			t.Fatalf("Scope Plan did not preserve and guard formal Volume %s", rel)
		}
	}
	result, err := scopechange.Apply(root, scopePreview, nil)
	if err != nil || result.Status != "applied" {
		t.Fatalf("legal Database lifecycle blocked Observe acknowledgement: result=%#v err=%v", result, err)
	}
	for rel, before := range formalBefore {
		if after := mustRead(t, filepath.Join(root, filepath.FromSlash(rel))); !bytes.Equal(after, before) {
			t.Fatalf("Observe acknowledgement unexpectedly changed %s", rel)
		}
	}
	after, exists, err := baseline.Load(root)
	if err != nil || !exists || after.Files["aoci.txt"].SHA256 != rootAfterBootstrap.SHA256 ||
		after.Files["aoci.code.txt"] != activeAfterBootstrap.Files["aoci.code.txt"] ||
		after.Files["aoci.database.txt"] != activeAfterBootstrap.Files["aoci.database.txt"] ||
		!reflect.DeepEqual(after.DatabaseCognition, activeAfterBootstrap.DatabaseCognition) ||
		after.Files["main_test.go"].SHA256 == activeAfterBootstrap.Files["main_test.go"].SHA256 {
		t.Fatalf("Scope Baseline did not advance only current Root/Observe governance: exists=%t err=%v baseline=%#v", exists, err, after)
	}
}
