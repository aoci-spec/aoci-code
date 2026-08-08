package scoperefresh

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScopeRefreshIdentitiesIgnoreAuditTimestampCopies(t *testing.T) {
	plan := Plan{Version: machinecontract.BaselineScopePlanV1, BaselineTimestamp: "2026-08-01T00:00:00Z", BaselinePostimageSHA256: "candidate"}
	firstPlanID, err := planIdentity(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.BaselineTimestamp = "2026-08-02T00:00:00Z"
	secondPlanID, err := planIdentity(plan)
	if err != nil || firstPlanID != secondPlanID {
		t.Fatalf("Plan identity must ignore its audit timestamp copy: first=%s second=%s err=%v", firstPlanID, secondPlanID, err)
	}

	preview := Preview{Version: machinecontract.BaselineScopePreviewV1, Plan: plan, BaselinePostimageSHA256: "candidate"}
	firstPreviewID, err := previewIdentity(preview)
	if err != nil {
		t.Fatal(err)
	}
	preview.Plan.BaselineTimestamp = "2099-01-01T00:00:00Z"
	secondPreviewID, err := previewIdentity(preview)
	if err != nil || firstPreviewID != secondPreviewID {
		t.Fatalf("Preview identity must ignore the nested audit timestamp copy: first=%s second=%s err=%v", firstPreviewID, secondPreviewID, err)
	}
	preview.BaselinePostimageSHA256 = "different-candidate"
	changedPreviewID, err := previewIdentity(preview)
	if err != nil || changedPreviewID == firstPreviewID {
		t.Fatal("Preview identity must continue binding projected Baseline bytes")
	}
}

func newScopeFixture(t *testing.T, runtimeCount int, excludeLockfile bool) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureFile(t, root, "src/main.go", "package main\n")
	writeFixtureFile(t, root, "src/new.go", "package main\n")
	writeFixtureFile(t, root, ".env", "SECRET=not-read-by-inventory\n")
	writeFixtureFile(t, root, "package-lock.json", "{}\n")
	cfg := config.DefaultConfig()
	if excludeLockfile {
		cfg.ExcludeFiles = append(cfg.ExcludeFiles, "package-lock.json")
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	mainFingerprint, err := baseline.HashFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	lockFingerprint, err := baseline.HashFile(filepath.Join(root, "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]baseline.Fingerprint{
		"src/main.go":       mainFingerprint,
		".env":              {SHA256: fmt.Sprintf("%064d", 1), Size: 12},
		"package-lock.json": lockFingerprint,
	}
	for index := 0; index < runtimeCount; index++ {
		files[fmt.Sprintf(".runtime/cache/%04d.pid", index)] = baseline.Fingerprint{SHA256: fmt.Sprintf("%064x", index+2), Size: 1}
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScopeRefreshSafelyRemovesRuntimeAndPreservesDrift(t *testing.T) {
	root := newScopeFixture(t, 2853, true)
	preview, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Plan.Removed) != 2855 || preview.Plan.SafeRemovalCount != 2854 || preview.Plan.OrdinaryRemovalCount != 1 {
		t.Fatalf("scope split mismatch: removed=%d safe=%d ordinary=%d", len(preview.Plan.Removed), preview.Plan.SafeRemovalCount, preview.Plan.OrdinaryRemovalCount)
	}
	if len(preview.Plan.Added) != 1 || preview.Plan.Added[0].Path != "src/new.go" || len(preview.Plan.SourceDrift) != 0 {
		t.Fatalf("new source or drift classification mismatch: %#v", preview.Plan)
	}
	if !preview.Plan.InteractionRequired || preview.Plan.HighRiskReduction {
		t.Fatalf("one explicit lockfile exclusion requires review but is not a mass ordinary reduction: %#v", preview.Plan)
	}

	writeFixtureFile(t, root, "src/main.go", "package changed\n")
	drifted, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(drifted.Plan.SourceDrift) != 1 || drifted.Plan.SourceDrift[0].Code != "source_bytes_changed" {
		t.Fatalf("real source drift was washed by scope change: %#v", drifted.Plan.SourceDrift)
	}
	approval, err := NewApproval(drifted, "human-reviewer", "2026-07-31T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, drifted, approval); err == nil || err.Error() != "baseline_scope_source_drift" {
		t.Fatalf("source drift must block before Intent: %v", err)
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("blocked Plan wrote Intent: %#v %v", pending, err)
	}
}

func TestScopeRefreshApplyIdempotenceRecoveryAndConflict(t *testing.T) {
	root := newScopeFixture(t, 3, true)
	preview, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := NewApproval(preview, "human-reviewer", "2026-07-31T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, preview, nil); err == nil || err.Error() != "baseline_scope_approval_required" {
		t.Fatalf("ordinary removal bypassed approval: %v", err)
	}

	applyFault = func(step string) error {
		if step == "after_baseline" {
			return fmt.Errorf("injected")
		}
		return nil
	}
	if _, err := Apply(root, preview, approval); err == nil || err.Error() != "injected" {
		t.Fatalf("fault injection did not stop after Baseline CAS: %v", err)
	}
	applyFault = func(string) error { return nil }
	t.Cleanup(func() { applyFault = func(string) error { return nil } })
	transactionID := preview.PreviewID[:24]
	status, err := Inspect(root, transactionID)
	if err != nil || status.State != "postimage" || !status.RecoveryAvailable {
		t.Fatalf("postimage recovery status invalid: %#v %v", status, err)
	}
	result, err := Resume(root, transactionID)
	if err != nil || result.Status != "recovered" {
		t.Fatalf("scope Resume did not converge: %#v %v", result, err)
	}
	result, err = Apply(root, preview, approval)
	if err != nil || result.Status != "already_applied" {
		t.Fatalf("repeated Apply not idempotent: %#v %v", result, err)
	}

	conflictRoot := newScopeFixture(t, 0, false)
	conflictPreview, err := Build(conflictRoot, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, conflictRoot, ".aoci/baseline.json", "third-party\n")
	if _, err := Apply(conflictRoot, conflictPreview, nil); err == nil || err.Error() != "baseline_scope_third_party_baseline_conflict" {
		t.Fatalf("third-party Baseline replacement was not rejected: %v", err)
	}
}

func TestScopeRefreshPureSafetyRemovalNeedsNoApproval(t *testing.T) {
	root := newScopeFixture(t, 100, false)
	preview, err := Build(root, time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.InteractionRequired || preview.Plan.OrdinaryRemovalCount != 0 {
		t.Fatalf("built-in safety removal should advance without ordinary-source approval: %#v", preview.Plan)
	}
	result, err := Apply(root, preview, nil)
	if err != nil || result.Status != "applied" {
		t.Fatalf("safe scope Apply failed: %#v %v", result, err)
	}
	loaded, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if _, exists := loaded.Files[".env"]; exists {
		t.Fatal("sensitive Baseline fingerprint survived safe Scope Refresh")
	}
	if _, exists := loaded.Files["package-lock.json"]; !exists {
		t.Fatal("package-lock.json was hard-coded as unsafe")
	}
	if _, exists := loaded.Files["src/new.go"]; !exists {
		t.Fatal("nonignored new source did not enter refreshed scope")
	}
}

func TestScopeRefreshRejectsManagedScopeBaseline(t *testing.T) {
	root := newScopeFixture(t, 0, false)
	value, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load fixture Baseline: exists=%v err=%v", exists, err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{
		Version:             machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity:      fmt.Sprintf("%064d", 7),
		ObserveChangePolicy: "review_required",
	}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, "2026-07-31T00:00:00Z"); err == nil || err.Error() != "baseline_scope_managed_scope_unsupported" {
		t.Fatalf("legacy Scope Refresh accepted a Managed Scope Baseline: %v", err)
	}
}

func TestScopeRefreshCurationIdentityIsReplayGuard(t *testing.T) {
	root := newScopeFixture(t, 0, false)
	preview, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	document := &curation.Document{Version: curation.Version, Decisions: []curation.Decision{{Path: "src/main.go",
		Decision: curation.DecisionExclude, Role: "generated fixture", Reason: "reviewed project scope",
		Confidence: 100, SourceSHA256: fingerprint.SHA256, Agent: "scope-test", UpdatedAt: "2026-07-31T00:01:00Z"}}}
	if err := curation.Save(root, document); err != nil {
		t.Fatal(err)
	}
	current, err := Build(root, "2026-07-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if current.Plan.CurationIdentity == preview.Plan.CurationIdentity || current.PreviewID == preview.PreviewID {
		t.Fatal("Curation change did not supersede Scope Preview")
	}
	var curatedRemoval *ScopeObject
	for index := range current.Plan.Removed {
		if current.Plan.Removed[index].Path == "src/main.go" {
			curatedRemoval = &current.Plan.Removed[index]
		}
	}
	if curatedRemoval == nil || curatedRemoval.Reason != "curation_excluded" || current.Plan.OrdinaryRemovalCount != 1 ||
		!current.Plan.InteractionRequired || current.Plan.SafeInventory.CurationExcluded != 1 {
		t.Fatalf("Curation exclusion was not represented as an approved scope delta: %#v", current.Plan)
	}
	if _, err := Apply(root, preview, nil); err == nil || err.Error() != "baseline_scope_replay_mismatch" {
		t.Fatalf("stale Curation-bound Preview was accepted: %v", err)
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("Curation replay rejection wrote Intent: %#v err=%v", pending, err)
	}
}

var _ = afs.SafeInventoryVersion
