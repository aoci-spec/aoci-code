package migrationapply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func TestMigrationEnvelopeDigestIgnoresAuditTimestampCopies(t *testing.T) {
	envelope := ApplyEnvelope{PreparedAt: "2026-08-01T00:00:00Z", Snapshot: LegacySnapshot{CapturedAt: "2026-08-01T00:00:01Z", LegacySHA256: "source-a"}, Baseline: BaselinePostimage{Content: "candidate-a"}}
	first, err := envelopeDigest(&envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.PreparedAt = "2026-08-02T00:00:00Z"
	envelope.Snapshot.CapturedAt = "2026-08-02T00:00:01Z"
	second, err := envelopeDigest(&envelope)
	if err != nil || first != second {
		t.Fatalf("Envelope digest must ignore audit timestamp copies: first=%s second=%s err=%v", first, second, err)
	}
	envelope.Snapshot.LegacySHA256 = "source-b"
	changedSource, err := envelopeDigest(&envelope)
	if err != nil || changedSource == first {
		t.Fatal("Envelope digest must continue binding formal source evidence")
	}
	envelope.Snapshot.LegacySHA256 = "source-a"
	envelope.Baseline.Content = "candidate-b"
	changedCandidate, err := envelopeDigest(&envelope)
	if err != nil || changedCandidate == first {
		t.Fatal("Envelope digest must continue binding formal candidate bytes")
	}
}

func TestLegacyMigrationCodeOnlyApplyAndStrictReversal(t *testing.T) {
	root := migrationFixture(t, false)
	legacyPath := filepath.Join(root, "aoci.txt")
	legacyWithDottedTag := strings.Replace(string(mustRead(t, legacyPath)), "main.go[CD9S]", "main.go[C.D.9.S]", 1)
	if err := os.WriteFile(legacyPath, []byte(legacyWithDottedTag), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshLegacyBaseline(t, root)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
	legacyBefore := mustRead(t, filepath.Join(root, "aoci.txt"))
	baselineBefore := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
	sourceBefore := mustRead(t, filepath.Join(root, "src", "main.go"))
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != machinecontract.CognitionMigrationStatusApplied || result.ActiveLayout != "volumes" || result.NetworkAccessed {
		t.Fatalf("Migration failed: result=%#v err=%v", result, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.LayoutMode != cognition.LayoutVolumesV1 || len(set.Volumes["code"].Objects) != 1 {
		t.Fatalf("Volumes layout invalid: set=%#v err=%v", set, err)
	}
	if !strings.Contains(string(mustRead(t, filepath.Join(root, "aoci.code.txt"))), "main.go[C.D.9.S]") {
		t.Fatal("preserved dotted Legacy Entry missing")
	}
	status, err := Status(root, result.TransactionID)
	if err != nil || status.Status != machinecontract.CognitionMigrationStatusApplied || status.RecoveryPending {
		t.Fatalf("terminal status invalid: %#v err=%v", status, err)
	}
	repeated, err := Apply(root, envelope, approval)
	if err != nil || repeated.Status != machinecontract.CognitionMigrationStatusAlreadyApplied {
		t.Fatalf("repeated migration not idempotent: %#v err=%v", repeated, err)
	}
	plan, err := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z")
	if err != nil || !plan.Eligible {
		t.Fatalf("strict Reversal not eligible: %#v err=%v", plan, err)
	}
	reversalApproval, err := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := ApplyReversal(root, plan, reversalApproval)
	if err != nil || reversed.Status != machinecontract.CognitionMigrationStatusReversed || reversed.ActiveLayout != "legacy" {
		t.Fatalf("Reversal failed: %#v err=%v", reversed, err)
	}
	if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(legacyBefore) ||
		string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineBefore) {
		t.Fatal("Reversal did not restore exact Legacy/Baseline bytes")
	}
	if string(mustRead(t, filepath.Join(root, "src", "main.go"))) != string(sourceBefore) {
		t.Fatal("business source changed")
	}
	for _, path := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("Volume remained after Reversal: %s", path)
		}
	}
}

func TestLegacySelfEntryHasExplicitDispositionAndStableTarget(t *testing.T) {
	root := migrationFixture(t, false)
	legacyPath := filepath.Join(root, "aoci.txt")
	legacy := strings.TrimSuffix(string(mustRead(t, legacyPath)), "\n") + "\n" +
		"===Source " + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CD9S]: F:Describe the formal cognition root asset | R:- | A:- | S:Keep governance identity explicit across layout migration\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshLegacyBaseline(t, root)
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	selfCount := 0
	for _, record := range plan.Mapping.Records {
		if record.LegacySelfEntry {
			selfCount++
			if record.DispositionVersion != machinecontract.CognitionLegacyEntryDispositionV1 || record.Disposition != "legacy_self_entry_root_authoring" ||
				len(record.AllowedTargets) != 1 || record.AllowedTargets[0] != cognition.OwnerRoot {
				t.Fatalf("self-entry disposition incomplete: %#v", record)
			}
		}
	}
	if selfCount != 1 || plan.Mapping.Coverage.LegacyEntryTotal != 2 ||
		plan.Mapping.Coverage.LegacyEntryDispositionComplete != plan.Mapping.Coverage.LegacyEntryTotal {
		t.Fatalf("Legacy Entry disposition coverage mismatch: %#v", plan.Mapping.Coverage)
	}
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code"})
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("self-entry D2-A Preview invalid: %#v %v", preview, err)
	}
	for _, change := range preview.LogicalDiff.Changes {
		if strings.Contains(change.TargetRef, "code:code:") {
			t.Fatalf("canonical target was double-prefixed: %#v", change)
		}
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorSelfEntryRootMapping(t, template)
	mapping, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2, Snapshot: *snapshot,
		Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, envelope, approval); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes["code"].Objects) != 1 || cognitionObjectExists(set.Volumes["code"], "code:aoci.txt") {
		t.Fatalf("Legacy self-entry remained Code-owned after Migration: %#v %v", set, err)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.CodeSourceCount != 1 || facts.CodeEntryCount != 1 {
		t.Fatalf("Migration ownership did not align shared governance: facts=%#v err=%v", facts, err)
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	codeFingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil || value.Files["aoci.code.txt"].SHA256 != codeFingerprint.SHA256 {
		t.Fatalf("Code Volume lacks stable Baseline identity: %#v %v", value.Files["aoci.code.txt"], err)
	}
	if _, duplicated := value.Files["aoci.txt"]; duplicated {
		t.Fatal("Root-owned aoci.txt was retained as a Code Baseline object")
	}
}

func TestFieldLevelEntryPreservationAndSemanticDiff(t *testing.T) {
	root := migrationFixture(t, false)
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code"})
	for index := range candidate.Assets {
		if candidate.Assets[index].AssetID == "code" {
			candidate.Assets[index].Content = strings.Replace(candidate.Assets[index].Content, "R:-", "R:code:src/main.go", 1)
		}
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("field-preservation Preview invalid: %#v %v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorRegeneratedEntryMapping(t, template, "code:src/main.go")
	targetByID := map[string]TargetRange{}
	for _, target := range authored.TargetRanges {
		targetByID[target.Identity] = target
	}
	for index := range authored.Records {
		record := &authored.Records[index]
		if record.TargetObject != "code:src/main.go" {
			continue
		}
		if record.SourceKind == "entry" {
			record.MappingMode = machinecontract.CognitionMappingFieldPreserved
			record.EntryPreservation = &EntryPreservation{Version: machinecontract.CognitionEntryPreservationV1,
				PreservedFields: []string{"tags", "F", "A", "S"}, RegeneratedFields: []string{"R"},
				IdentityCanonicalizationProposal: &IdentityCanonicalizationProposal{SourceObjectIdentity: "src/main.go", TargetObjectIdentity: "code:src/main.go",
					OneToOne: true, TargetExists: true, RepresentationOnly: true, ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"},
				ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}
			continue
		}
		target := targetByID[record.TargetSemanticRangeIdentity]
		if record.SourceSHA256 == target.SHA256 {
			record.MappingMode = machinecontract.CognitionMappingPreserved
		}
	}
	validated, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Coverage.PreservedFieldCount != 4 || validated.Coverage.RegeneratedFieldCount != 1 ||
		validated.Coverage.FieldPreservedEntryCount != 1 || validated.Coverage.IdentityCanonicalizationCount != 1 || validated.Coverage.FullRegeneratedEntryCount != 0 {
		t.Fatalf("field preservation coverage incorrect: %#v", validated.Coverage)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2, Snapshot: *snapshot,
		Plan: *plan, Mapping: *validated, Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.SemanticDiff.PreservedFields != 4 || envelope.SemanticDiff.RegeneratedFields != 1 || envelope.SemanticDiff.FullRegenerated != 0 ||
		envelope.SemanticDiff.DiffSHA256 == "" {
		t.Fatalf("field-level Semantic Diff incomplete: %#v", envelope.SemanticDiff)
	}
}

func TestLegacyMigrationCodeDatabaseAndBindings(t *testing.T) {
	root := migrationFixture(t, true)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code", "database"})
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != machinecontract.CognitionMigrationStatusApplied {
		t.Fatalf("Code+Database Migration failed: %#v err=%v", result, err)
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists || value.DatabaseCognition == nil || len(value.DatabaseCognition.Entries) != 1 {
		t.Fatalf("Database Binding missing: %#v exists=%t err=%v", value, exists, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes["code"].Objects) != 1 || len(set.Volumes["database"].Objects) != 1 {
		t.Fatalf("Code+Database scopes invalid: %#v err=%v", set, err)
	}
}

func TestRepresentativeReactNodeMySQLLegacyMigration(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join("..", "cognitionplan", "testdata", "pilot-react-node-mysql")
	for _, relative := range []string{"frontend/src/App.jsx", "backend/src/api.js", "backend/src/service.js"} {
		data, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		writeMigrationFile(t, root, relative, string(data))
	}
	installMigrationDatabaseEvidence(t, root)
	legacy := strings.Join([]string{
		"#AOCI-CLI Complete Index", "#Project: De-identified React Node MySQL fixture", "#[Tag dictionary]", "#A Layer: C Code D Database",
		"===Frontend " + filepath.ToSlash(filepath.Join(root, "frontend", "src")) + "/===",
		"App.jsx[CF8S]: F:Present model-authored item state | R:code:backend/src/api.js | A:App | S:Keep UI state independent from schema transport details",
		"===Backend " + filepath.ToSlash(filepath.Join(root, "backend", "src")) + "/===",
		"api.js[CA8S]: F:Expose the item endpoint | R:code:backend/src/service.js | A:GET /api/items | S:Do not place database credentials in route code",
		"service.js[CS9S]: F:Coordinate item retrieval | R:database://primary/fixture/items | A:listItems | S:Preserve the frontend to API to service to database boundary",
		"===Database/database://primary/fixture/===",
		"items[DB9S]: F:Persist de-identified item state | R:code:backend/src/service.js | A:id,name | S:Item writes and their audit event commit in one transaction",
	}, "\n") + "\n"
	writeMigrationFile(t, root, "aoci.txt", legacy)
	refreshPilotBaseline(t, root, []string{"aoci.txt", "frontend/src/App.jsx", "backend/src/api.js", "backend/src/service.js"})

	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code", "database"}, "2026-07-30T00:00:00Z")
	if err != nil || snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityEligible {
		t.Fatalf("Pilot Snapshot invalid: %#v err=%v", snapshot, err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code", "database"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := pilotMigrationCandidate(root, plan, legacy)
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("Pilot D2-A Preview invalid: %#v err=%v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := ValidateMapping(root, snapshot, plan, candidate, authorApplyGradeMapping(t, template))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2, Snapshot: *snapshot,
		Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	approval, _ := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	result, err := Apply(root, envelope, approval)
	if err != nil || result.NetworkAccessed {
		t.Fatalf("Pilot Migration failed: %#v err=%v", result, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes["code"].Objects) != 3 || len(set.Volumes["database"].Objects) != 1 {
		t.Fatalf("Pilot scopes invalid: %#v err=%v", set, err)
	}
}

func TestMigrationRootLastFailureResumeAndPendingRollback(t *testing.T) {
	t.Run("root_failure_resume", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		previous := migrationFault
		migrationFault = failMigrationOnce("before_publish_root")
		_, err := Apply(root, envelope, approval)
		migrationFault = previous
		if err == nil {
			t.Fatal("injected Root failure ignored")
		}
		transactionID := transactionIdentity(envelope, approval)
		status, err := Status(root, transactionID)
		if err != nil || status.ActiveLayout != "legacy" || status.Status != machinecontract.CognitionMigrationStatusRecoveryRequiredLegacy {
			t.Fatalf("Root-last state invalid: %#v err=%v", status, err)
		}
		if stateFor(status, "aoci.meta.txt") != "postimage" || stateFor(status, "aoci.code.txt") != "postimage" || stateFor(status, "aoci.txt") != "preimage" {
			t.Fatalf("dormant Volume prefix invalid: %#v", status.Targets)
		}
		resumed, err := Resume(root, transactionID)
		if err != nil || resumed.Status != machinecontract.CognitionMigrationStatusApplied {
			t.Fatalf("Resume failed: %#v err=%v", resumed, err)
		}
	})

	t.Run("baseline_failure_rollback", func(t *testing.T) {
		root := migrationFixture(t, false)
		legacy := mustRead(t, filepath.Join(root, "aoci.txt"))
		baselineRaw := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		previous := migrationFault
		migrationFault = failMigrationOnce("before_publish_baseline")
		_, err := Apply(root, envelope, approval)
		migrationFault = previous
		if err == nil {
			t.Fatal("injected Baseline failure ignored")
		}
		transactionID := transactionIdentity(envelope, approval)
		rolled, err := Rollback(root, transactionID)
		if err != nil || rolled.Status != machinecontract.CognitionMigrationStatusRolledBack {
			t.Fatalf("pending Rollback failed: %#v err=%v", rolled, err)
		}
		if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(legacy) || string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineRaw) {
			t.Fatal("pending Rollback did not restore exact preimages")
		}
	})
}

func TestMigrationThirdPartyConflictNeverOverwritten(t *testing.T) {
	root := migrationFixture(t, false)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
	previous := migrationFault
	migrationFault = failMigrationOnce("before_publish_root")
	_, _ = Apply(root, envelope, approval)
	migrationFault = previous
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte("third-party\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transactionID := transactionIdentity(envelope, approval)
	status, err := Status(root, transactionID)
	if err != nil || status.Status != machinecontract.CognitionMigrationStatusRecoveryConflict || !status.ThirdPartyConflict {
		t.Fatalf("third-party conflict not detected: %#v err=%v", status, err)
	}
	if _, err := Resume(root, transactionID); err == nil {
		t.Fatal("Resume overwrote third-party bytes")
	}
	if string(mustRead(t, filepath.Join(root, "aoci.code.txt"))) != "third-party\n" {
		t.Fatal("third-party bytes changed")
	}
}

func TestMigrationApprovalGuardsAndPrepareAreFailClosed(t *testing.T) {
	t.Run("runtime_audit_does_not_change_content_replay", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		legacy := mustRead(t, filepath.Join(root, "aoci.txt"))
		baselineRaw := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
		cfg, err := config.LoadReadOnly(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{"rules", "overview"} {
			ledger.Append(root, cfg.LedgerEnabled, ledger.Event{Op: op, Result: ledger.ResultOK})
		}
		result, err := Apply(root, envelope, approval)
		if err != nil || result.Status != machinecontract.CognitionMigrationStatusApplied {
			t.Fatalf("runtime audit metadata destabilized Migration replay: %#v err=%v", result, err)
		}
		if string(mustDecodeLegacy(t, envelope)) != string(legacy) || envelope.Snapshot.BaselineSHA256 != sha256Hex(baselineRaw) {
			t.Fatal("frozen Migration preimages changed while accepting runtime audit metadata")
		}
	})

	t.Run("prepare_zero_write", func(t *testing.T) {
		root := migrationFixture(t, false)
		legacy := mustRead(t, filepath.Join(root, "aoci.txt"))
		baselineRaw := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
		_, _ = preparedMigrationFixture(t, root, []string{"code"})
		if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(legacy) || string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineRaw) {
			t.Fatal("Prepare changed formal assets")
		}
		if entries, err := os.ReadDir(filepath.Join(root, ".aoci", "transactions")); err == nil && len(entries) != 0 {
			t.Fatalf("Prepare persisted runtime transaction assets: %v", entries)
		}
	})
	t.Run("forged_approval", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		forged := *approval
		forged.MappingSHA256 = strings.Repeat("0", 64)
		if _, err := Apply(root, envelope, &forged); err == nil {
			t.Fatal("forged Approval was accepted")
		}
		if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(mustDecodeLegacy(t, envelope)) {
			t.Fatal("forged Approval changed Legacy")
		}
	})
	t.Run("source_drift", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		writeMigrationFile(t, root, "src/main.go", "package main\n// third-party drift\n")
		if _, err := Apply(root, envelope, approval); err == nil || !strings.Contains(err.Error(), "guard_drift") {
			t.Fatalf("source drift was accepted: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(root, "aoci.meta.txt")); !os.IsNotExist(err) {
			t.Fatal("source-drifted Apply wrote a Volume")
		}
	})
	t.Run("target_created_after_approval", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		writeMigrationFile(t, root, "aoci.meta.txt", "third-party\n")
		if _, err := Apply(root, envelope, approval); err == nil {
			t.Fatal("third-party target was accepted")
		}
		if string(mustRead(t, filepath.Join(root, "aoci.meta.txt"))) != "third-party\n" {
			t.Fatal("third-party target was overwritten")
		}
	})
	t.Run("target_symlink_after_approval", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		target := filepath.Join(root, "third-party.txt")
		writeMigrationFile(t, root, "third-party.txt", "third-party\n")
		if err := os.Symlink(target, filepath.Join(root, "aoci.meta.txt")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Apply(root, envelope, approval); err == nil {
			t.Fatal("target symlink was accepted")
		}
		if string(mustRead(t, target)) != "third-party\n" {
			t.Fatal("target symlink destination was overwritten")
		}
	})
	t.Run("snapshot_tamper", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		tampered := *envelope
		tampered.Snapshot.LegacyContentBase64 = "dGFtcGVyZWQ="
		if _, err := Apply(root, &tampered, approval); err == nil {
			t.Fatal("tampered Snapshot was accepted")
		}
		if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(mustDecodeLegacy(t, envelope)) {
			t.Fatal("tampered Snapshot changed Legacy")
		}
	})
	t.Run("missing_staging", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		previous := migrationFault
		migrationFault = failMigrationOnce("after_recovery_intent")
		_, _ = Apply(root, envelope, approval)
		migrationFault = previous
		transactionID := transactionIdentity(envelope, approval)
		intent, err := loadRecoveryAt(intentPath(root, transactionID), transactionID)
		if err != nil || len(intent.Staging) == 0 {
			t.Fatalf("pending Intent unavailable: %#v err=%v", intent, err)
		}
		stagingRel := ""
		for _, staged := range intent.Staging {
			if staged.Path == "aoci.meta.txt" {
				stagingRel = staged.StagingRel
			}
		}
		if stagingRel == "" {
			t.Fatal("Meta staging record missing")
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(stagingRel))); err != nil {
			t.Fatal(err)
		}
		status, err := Status(root, transactionID)
		if err != nil || status.Status != machinecontract.CognitionMigrationStatusRecoveryConflict {
			t.Fatalf("missing staging did not fail closed: %#v err=%v", status, err)
		}
		if _, err := Resume(root, transactionID); err == nil {
			t.Fatal("Resume crossed missing staging")
		}
	})
}

// The retry above is only alive while Apply's lock failure still satisfies
// errors.Is(err, afs.ErrLockTimeout). That holds because transaction.go wraps
// with %w, which is one character and no test previously watched it: change it
// to %v and the retry silently stops matching, the flake returns, and every
// suite stays green while it happens.
//
// Costs one real DefaultLockTimeout because that constant is not injectable, so
// it follows this file's existing convention and runs in Full Confidence only —
// which is also the only tier where the flake was ever observed.
func TestApplyLockFailureKeepsTheTimeoutSentinel(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the real 10s index-lock timeout; runs in Full Confidence")
	}
	root := migrationFixture(t, false)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
	held, err := afs.AcquireIndexLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	_, applyErr := Apply(root, envelope, approval)
	if applyErr == nil {
		t.Fatal("Apply succeeded while the index lock was held by someone else")
	}
	if !errors.Is(applyErr, afs.ErrLockTimeout) {
		t.Fatalf("applyRetryingLockWait can never retry: %v does not match ErrLockTimeout", applyErr)
	}
}

// applyRetryingLockWait runs Apply and retries only a lock-wait timeout.
//
// A timeout is not a verdict about the property under test. Two concurrent
// Applies must serialize to exactly one applied and one already_applied; the
// index write lock is what enforces that, so a wait that expires means the lock
// worked and this caller ran out of patience. The production error says as much
// in as many words — it ends in "稍后重试" — and the only thing not following
// that instruction was this test.
//
// Observed once on a loaded windows-latest runner where the migrationapply
// package took 187s against roughly 0.3s per run locally. Asserting the outcome
// distribution is this test's job; asserting that the loser outlasts an
// unrelated machine's load is not, and conflating the two reported a correct
// serialization as a failure. A lock that never frees still fails the test,
// because the final attempt's error is returned.
func applyRetryingLockWait(root string, envelope *ApplyEnvelope, approval *Approval) (*ApplyResult, error) {
	var result *ApplyResult
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		result, err = Apply(root, envelope, approval)
		if err == nil || !errors.Is(err, afs.ErrLockTimeout) {
			return result, err
		}
	}
	return result, err
}

func TestConcurrentMigrationAndRecoverySerialize(t *testing.T) {
	t.Run("same_approved_apply", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		results := make(chan *ApplyResult, 2)
		failures := make(chan error, 2)
		var group sync.WaitGroup
		for index := 0; index < 2; index++ {
			group.Add(1)
			go func() {
				defer group.Done()
				result, err := applyRetryingLockWait(root, envelope, approval)
				results <- result
				failures <- err
			}()
		}
		group.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		statuses := map[string]int{}
		for result := range results {
			statuses[result.Status]++
		}
		if statuses[machinecontract.CognitionMigrationStatusApplied] != 1 || statuses[machinecontract.CognitionMigrationStatusAlreadyApplied] != 1 {
			t.Fatalf("concurrent outcomes invalid: %#v", statuses)
		}
	})
	t.Run("resume_rollback", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		previous := migrationFault
		migrationFault = failMigrationOnce("after_recovery_intent")
		_, _ = Apply(root, envelope, approval)
		migrationFault = previous
		transactionID := transactionIdentity(envelope, approval)
		var group sync.WaitGroup
		errors := make(chan error, 2)
		group.Add(2)
		go func() { defer group.Done(); _, err := Resume(root, transactionID); errors <- err }()
		go func() { defer group.Done(); _, err := Rollback(root, transactionID); errors <- err }()
		group.Wait()
		close(errors)
		successes := 0
		for err := range errors {
			if err == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("Resume/Rollback did not serialize: successes=%d", successes)
		}
		status, err := Status(root, transactionID)
		if err != nil || status.RecoveryPending || (status.Status != machinecontract.CognitionMigrationStatusApplied && status.Status != machinecontract.CognitionMigrationStatusRolledBack) {
			t.Fatalf("terminal state invalid: %#v err=%v", status, err)
		}
	})
}

func TestMigrationFaultBoundariesConvergeDeterministically(t *testing.T) {
	if testing.Short() {
		t.Skip("full fault matrix runs in Full Confidence")
	}
	steps := []string{
		"before_snapshot_persist", "after_snapshot_persist", "before_stage_0", "after_stage_0",
		"before_recovery_intent", "after_recovery_intent", "before_publish_meta", "after_publish_meta",
		"before_publish_code", "after_publish_code", "before_publish_root", "after_publish_root",
		"before_publish_baseline", "after_publish_baseline", "before_internal_verify", "after_internal_verify",
		"before_completion_receipt", "after_completion_receipt", "before_ledger", "after_ledger",
		"before_transaction_archive", "after_transaction_archive",
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := migrationFixture(t, false)
			envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
			previous := migrationFault
			migrationFault = failMigrationOnce(step)
			_, err := Apply(root, envelope, approval)
			migrationFault = previous
			if err == nil {
				t.Fatalf("fault %s was ignored", step)
			}
			transactionID := transactionIdentity(envelope, approval)
			pending, pendingErr := Pending(root)
			if pendingErr != nil {
				t.Fatal(pendingErr)
			}
			var result *ApplyResult
			if len(pending) == 1 {
				status, statusErr := Status(root, transactionID)
				if statusErr != nil || status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
					t.Fatalf("fault produced unrecoverable state: %#v err=%v", status, statusErr)
				}
				result, err = Resume(root, transactionID)
			} else {
				result, err = Apply(root, envelope, approval)
			}
			if err != nil || (result.Status != machinecontract.CognitionMigrationStatusApplied && result.Status != machinecontract.CognitionMigrationStatusAlreadyApplied) {
				t.Fatalf("fault did not converge: result=%#v err=%v", result, err)
			}
			if migrationLedgerCount(root, transactionID) != 1 {
				t.Fatalf("terminal Ledger event duplicated after %s", step)
			}
		})
	}
}

func TestMigrationReversalFaultBoundariesConverge(t *testing.T) {
	if testing.Short() {
		t.Skip("full reversal fault matrix runs in Full Confidence")
	}
	steps := []string{"before_stage_0", "after_stage_0", "before_reversal_root", "after_reversal_root",
		"before_reversal_baseline", "after_reversal_baseline", "before_reversal_code", "after_reversal_code",
		"before_reversal_meta", "after_reversal_meta", "before_reversal_receipt", "after_reversal_receipt",
		"before_reversal_ledger", "after_reversal_ledger", "before_reversal_archive", "after_reversal_archive"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := migrationFixture(t, false)
			envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
			migrated, err := Apply(root, envelope, approval)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareReversal(root, migrated.TransactionID, "2026-07-30T00:02:00Z")
			if err != nil {
				t.Fatal(err)
			}
			approved, err := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
			if err != nil {
				t.Fatal(err)
			}
			previous := migrationFault
			migrationFault = failMigrationOnce(step)
			_, err = ApplyReversal(root, plan, approved)
			migrationFault = previous
			if err == nil {
				t.Fatalf("fault %s was ignored", step)
			}
			reversalID := reversalTransactionIdentity(plan, approved)
			pending, pendingErr := cognitionPendingReversal(root)
			if pendingErr != nil {
				t.Fatal(pendingErr)
			}
			var result *ApplyResult
			if len(pending) == 1 {
				status, statusErr := ReversalStatus(root, reversalID)
				if statusErr != nil || status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
					t.Fatalf("Reversal fault produced conflict: %#v err=%v", status, statusErr)
				}
				result, err = ResumeReversal(root, reversalID)
			} else {
				result, err = ApplyReversal(root, plan, approved)
			}
			if err != nil || (result.Status != machinecontract.CognitionMigrationStatusReversed && result.Status != machinecontract.CognitionMigrationStatusAlreadyReversed) {
				t.Fatalf("Reversal fault did not converge: %#v err=%v", result, err)
			}
		})
	}
}

func TestMigrationPendingRollbackFaultBoundariesConverge(t *testing.T) {
	if testing.Short() {
		t.Skip("full rollback fault matrix runs in Full Confidence")
	}
	steps := []string{"after_rollback_root", "after_rollback_baseline", "before_rollback_code", "after_rollback_code",
		"before_rollback_meta", "after_rollback_meta", "before_rollback_receipt", "after_rollback_receipt",
		"before_rollback_archive", "after_rollback_archive"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := migrationFixture(t, false)
			envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
			previous := migrationFault
			migrationFault = failMigrationOnce("before_internal_verify")
			_, err := Apply(root, envelope, approval)
			migrationFault = previous
			if err == nil {
				t.Fatal("failed to create active pending Migration")
			}
			transactionID := transactionIdentity(envelope, approval)
			migrationFault = failMigrationOnce(step)
			_, err = Rollback(root, transactionID)
			migrationFault = previous
			if err == nil {
				t.Fatalf("Rollback fault %s was ignored", step)
			}
			result, err := Rollback(root, transactionID)
			if err != nil || result.Status != machinecontract.CognitionMigrationStatusRolledBack {
				t.Fatalf("Rollback fault did not converge: %#v err=%v", result, err)
			}
			status, err := Status(root, transactionID)
			if err != nil || status.Status != machinecontract.CognitionMigrationStatusRolledBack || status.RecoveryPending {
				t.Fatalf("Rollback terminal status invalid: %#v err=%v", status, err)
			}
		})
	}
}

func TestCompletedMigrationReversalRejectsDriftAndLaterWrites(t *testing.T) {
	t.Run("formal_postimage_drift", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		result, err := Apply(root, envelope, approval)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte("third-party\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		plan, err := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z")
		if err != nil || plan.Eligible || len(plan.Risks) == 0 {
			t.Fatalf("drifted postimage remained eligible: %#v err=%v", plan, err)
		}
	})
	t.Run("later_cognition_write", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		result, err := Apply(root, envelope, approval)
		if err != nil {
			t.Fatal(err)
		}
		cfg, _ := config.LoadReadOnly(root)
		ledger.Append(root, cfg.LedgerEnabled, ledger.Event{Op: "aoci_update_entry", Result: ledger.ResultOK, AppliedCount: 1})
		if _, err := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z"); err == nil || !strings.Contains(err.Error(), "later_cognition_write") {
			t.Fatalf("later cognition write did not permanently reject Reversal: %v", err)
		}
	})
	t.Run("pending_resume_revalidates_guards", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		result, err := Apply(root, envelope, approval)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z")
		if err != nil {
			t.Fatal(err)
		}
		reversalApproval, err := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
		if err != nil {
			t.Fatal(err)
		}
		previous := migrationFault
		migrationFault = failMigrationOnce("after_reversal_root")
		_, err = ApplyReversal(root, plan, reversalApproval)
		migrationFault = previous
		if err == nil {
			t.Fatal("failed to create pending Reversal")
		}
		writeMigrationFile(t, root, "src/main.go", "package main\n// third-party drift\n")
		reversalID := reversalTransactionIdentity(plan, reversalApproval)
		if _, err := ResumeReversal(root, reversalID); err == nil || !strings.Contains(err.Error(), "guard_drift") {
			t.Fatalf("pending Reversal crossed Source drift: %v", err)
		}
	})
	t.Run("pending_resume_rejects_other_transaction", func(t *testing.T) {
		root := migrationFixture(t, false)
		envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
		result, err := Apply(root, envelope, approval)
		if err != nil {
			t.Fatal(err)
		}
		plan, _ := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z")
		reversalApproval, _ := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
		previous := migrationFault
		migrationFault = failMigrationOnce("after_reversal_root")
		_, _ = ApplyReversal(root, plan, reversalApproval)
		migrationFault = previous
		writeMigrationFile(t, root, ".aoci/transactions/bootstrap-third-party.json", "{}\n")
		reversalID := reversalTransactionIdentity(plan, reversalApproval)
		if _, err := ResumeReversal(root, reversalID); err == nil || !strings.Contains(err.Error(), "other_pending") {
			t.Fatalf("pending Reversal crossed another transaction: %v", err)
		}
	})
}

func TestMigrationContractsStrictAndIdentityStable(t *testing.T) {
	root := migrationFixture(t, false)
	snapshotA, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	snapshotB, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T01:00:00Z")
	if err != nil || snapshotA.SnapshotIdentity != snapshotB.SnapshotIdentity {
		t.Fatalf("audit time destabilized Snapshot identity: %s %s err=%v", snapshotA.SnapshotIdentity, snapshotB.SnapshotIdentity, err)
	}
	data, _ := json.Marshal(snapshotA)
	if _, err := DecodeLegacySnapshot(append(data, []byte("{}")...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := DecodeLegacySnapshot([]byte(strings.Replace(string(data), "{", "{\"unknown\":true,", 1))); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := DecodeLegacySnapshot([]byte(strings.Replace(string(data), "{", "{\"version\":\"cognition-legacy-snapshot/v1\",", 1))); err == nil {
		t.Fatal("duplicate field accepted")
	}
}

func TestMigrationEligibilityAndMixedLayoutFailClosed(t *testing.T) {
	t.Run("duplicate_entry_snapshot_only", func(t *testing.T) {
		root := migrationFixture(t, false)
		path := filepath.Join(root, "aoci.txt")
		raw := mustRead(t, path)
		line := findLine(string(raw), "main.go[")
		if err := os.WriteFile(path, append(raw, []byte(line+"\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		refreshLegacyBaseline(t, root)
		snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
		if err != nil || snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityIneligible || len(snapshot.Findings) == 0 {
			t.Fatalf("damaged Legacy did not produce reviewable ineligible Snapshot: %#v err=%v", snapshot, err)
		}
	})
	t.Run("mixed_layout", func(t *testing.T) {
		root := migrationFixture(t, false)
		writeMigrationFile(t, root, "aoci.meta.txt", cognition.MetaVolumeMarker+"\n")
		if _, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "mixed_layout") {
			t.Fatalf("mixed layout was accepted: %v", err)
		}
	})
	t.Run("damaged_root_marker", func(t *testing.T) {
		root := migrationFixture(t, false)
		writeMigrationFile(t, root, "aoci.txt", "#AOCI-ROOT-MANIFEST: 2\n")
		if _, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "root_marker_invalid") {
			t.Fatalf("damaged Root marker fell back to Legacy: %v", err)
		}
	})
	t.Run("missing_baseline", func(t *testing.T) {
		root := migrationFixture(t, false)
		if err := os.Remove(filepath.Join(root, ".aoci", "baseline.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "baseline_required") {
			t.Fatalf("missing Baseline was accepted: %v", err)
		}
	})
	t.Run("pending_transaction", func(t *testing.T) {
		root := migrationFixture(t, false)
		writeMigrationFile(t, root, ".aoci/transactions/bootstrap-deadbeef.json", "{}\n")
		if _, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "pending_recovery") {
			t.Fatalf("pending transaction was crossed: %v", err)
		}
	})
	t.Run("legacy_symlink", func(t *testing.T) {
		root := migrationFixture(t, false)
		path := filepath.Join(root, "aoci.txt")
		target := filepath.Join(root, "legacy-real.txt")
		if err := os.Rename(path, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "not_regular") {
			t.Fatalf("Legacy symlink was accepted: %v", err)
		}
	})
	for _, damaged := range []struct {
		name string
		edit func(string) string
	}{
		{name: "illegal_tag", edit: func(raw string) string { return strings.Replace(raw, "main.go[CD9S]", "main.go[ZZZZ]", 1) }},
		{name: "empty_relation_item", edit: func(raw string) string { return strings.Replace(raw, "R:-", "R:a.go,,b.go", 1) }},
	} {
		t.Run(damaged.name, func(t *testing.T) {
			root := migrationFixture(t, false)
			path := filepath.Join(root, "aoci.txt")
			if err := os.WriteFile(path, []byte(damaged.edit(string(mustRead(t, path)))), 0o644); err != nil {
				t.Fatal(err)
			}
			refreshLegacyBaseline(t, root)
			snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
			if err != nil || snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityIneligible || len(snapshot.Findings) == 0 {
				t.Fatalf("damaged Legacy became Apply eligible: %#v err=%v", snapshot, err)
			}
		})
	}
}

// 历史索引里指不到的 R 遍地都是。那不是损坏, 迁移必须照常放行 —— 机器不核对
// 一条索引的关系指向谁。
func TestMigrationEligibleWhenLegacyRelationsDangle(t *testing.T) {
	root := migrationFixture(t, false)
	path := filepath.Join(root, "aoci.txt")
	raw := strings.Replace(string(mustRead(t, path)), "R:-", "R:./missing.go", 1)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshLegacyBaseline(t, root)
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Eligibility == machinecontract.CognitionMigrationEligibilityIneligible {
		t.Fatalf("悬空关系不应阻止迁移: %#v", snapshot.Findings)
	}
}

func TestMigrationModelRegeneratedEntryRequiresReviewedMappingGroup(t *testing.T) {
	root := migrationFixture(t, false)
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code"})
	for index := range candidate.Assets {
		if candidate.Assets[index].AssetID == "code" {
			candidate.Assets[index].Content = strings.Replace(candidate.Assets[index].Content,
				"F:Preserve the legacy code responsibility", "F:Reviewed regenerated code responsibility", 1)
		}
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("regenerated D2-A Preview invalid: %#v err=%v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorRegeneratedEntryMapping(t, template, "code:src/main.go")
	validated, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Coverage.SemanticEquivalence != machinecontract.CognitionMigrationSemanticReviewed || len(validated.MappingGroups) != 1 {
		t.Fatalf("reviewed Mapping Group not accepted: %#v", validated.Coverage)
	}
	broken := *validated
	broken.MappingGroups = []MappingGroup{}
	if _, err := ValidateMapping(root, snapshot, plan, candidate, &broken); err == nil {
		t.Fatal("regenerated Entry without Mapping Group was accepted")
	}
}

func TestMigrationModelOwnedMixedEntrySplitAcrossCodeAndDatabase(t *testing.T) {
	root := migrationFixture(t, true)
	legacyPath := filepath.Join(root, "aoci.txt")
	legacy := string(mustRead(t, legacyPath))
	legacy = strings.Replace(legacy, findLine(legacy, "items[")+"\n", "", 1)
	legacy = strings.Replace(legacy, "F:Preserve the legacy code responsibility | R:- | A:- | S:Keep source behavior byte-stable during cognition migration",
		"F:Describe mixed application and durable item behavior | R:- | A:- | S:Keep the application and durable identity rules together", 1)
	writeMigrationFile(t, root, "aoci.txt", legacy)
	refreshLegacyBaseline(t, root)

	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code", "database"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code", "database"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code", "database"})
	codeLine := "main.go[CD9S]: F:Run the application entrypoint | R:database://primary/fixture/items | A:- | S:Keep source behavior byte-stable during cognition migration"
	databaseLine := "items[DB9S]: F:Persist durable item identity | R:code:src/main.go | A:id | S:Writes and their audit record commit together"
	for index := range candidate.Assets {
		switch candidate.Assets[index].AssetID {
		case "code":
			candidate.Assets[index].Content = cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + codeLine + "\n"
		case "database":
			candidate.Assets[index].Content = cognition.DatabaseMarker + "\n===Database/database://primary/fixture/===\n" + databaseLine + "\n"
		}
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("mixed Entry D2-A Preview invalid: %#v err=%v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorSplitEntryMapping(t, template)
	validated, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Coverage.SemanticEquivalence != machinecontract.CognitionMigrationSemanticReviewed || len(validated.MappingGroups) != 1 {
		t.Fatalf("mixed Entry split was not fully reviewed: %#v", validated.Coverage)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2,
		Snapshot: *snapshot, Plan: *plan, Mapping: *validated, Candidate: *candidate, Preview: *preview,
		BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != machinecontract.CognitionMigrationStatusApplied {
		t.Fatalf("mixed Entry Migration failed: %#v err=%v", result, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes["code"].Objects) != 1 || len(set.Volumes["database"].Objects) != 1 {
		t.Fatalf("split target layout invalid: %#v err=%v", set, err)
	}
}

func TestMigrationMappingTemplateGeneratesNoSemantics(t *testing.T) {
	root := migrationFixture(t, true)
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code", "database"}, "2026-07-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code", "database"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code", "database"})
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range template.Records {
		if record.SourceKind == "structure" || record.SourceKind == "section" {
			if record.MappingMode != machinecontract.CognitionMappingStructuralOnly || record.TargetAsset != "none" {
				t.Fatalf("structural governance record changed: %#v", record)
			}
			continue
		}
		if record.SemanticRole != "" || record.TargetAsset != "" || record.TargetObject != "" ||
			record.TargetSemanticRangeIdentity != "" || record.MappingMode != "" ||
			record.ReviewStatus != machinecontract.CognitionMigrationSemanticPending || record.Reviewer != "" {
			t.Fatalf("program generated a semantic decision: %#v", record)
		}
	}
	for _, task := range template.AuthoringTasks {
		if task.TargetAsset != "" || task.TargetObject != "" || len(task.CandidateRangeIdentities) != 0 ||
			task.Status != machinecontract.CognitionMigrationSemanticPending || task.Reviewer != "" {
			t.Fatalf("program prefilled a model authoring decision: %#v", task)
		}
	}
}

func TestMigrationV1EnvelopeAndApprovalAreHistoricalOnly(t *testing.T) {
	root := migrationFixture(t, false)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code"})

	oldEnvelope := *envelope
	oldEnvelope.Version = machinecontract.CognitionMigrationApplyEnvelopeV1
	oldEnvelope.RequestVersion = machinecontract.CognitionMigrationApplyRequestV1
	if err := validateEnvelope(&oldEnvelope); err == nil {
		t.Fatal("rc16 v1 Envelope was accepted by the rc17 content contract")
	}
	oldApproval := *approval
	oldApproval.Version = machinecontract.CognitionMigrationApprovalV1
	if _, err := Apply(root, envelope, &oldApproval); err == nil {
		t.Fatal("rc16 v1 Approval authorized an rc17 v2 Envelope")
	}
	for _, path := range []string{"aoci.meta.txt", "aoci.code.txt"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("historical approval caused a formal write: %s", path)
		}
	}
}

func TestMigrationCRLFAndBOMAreByteReversible(t *testing.T) {
	for _, current := range []struct {
		name string
		edit func([]byte) []byte
		bom  string
		line string
	}{
		{name: "crlf", edit: func(raw []byte) []byte { return []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")) }, bom: "absent", line: "crlf"},
		{name: "utf8_bom", edit: func(raw []byte) []byte { return append([]byte{0xef, 0xbb, 0xbf}, raw...) }, bom: "utf8_bom", line: "lf"},
	} {
		t.Run(current.name, func(t *testing.T) {
			root := migrationFixture(t, false)
			legacyPath := filepath.Join(root, "aoci.txt")
			raw := current.edit(mustRead(t, legacyPath))
			if err := os.WriteFile(legacyPath, raw, 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(legacyPath, 0o640); err != nil {
				t.Fatal(err)
			}
			legacyInfo, err := os.Stat(legacyPath)
			if err != nil {
				t.Fatal(err)
			}
			expectedMode := legacyInfo.Mode().Perm()
			refreshLegacyBaseline(t, root)
			envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
			if envelope.Snapshot.BOM != current.bom || envelope.Snapshot.LineEndings != current.line {
				t.Fatalf("format evidence lost: bom=%s lines=%s", envelope.Snapshot.BOM, envelope.Snapshot.LineEndings)
			}
			migrated, err := Apply(root, envelope, approval)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareReversal(root, migrated.TransactionID, "2026-07-30T00:02:00Z")
			if err != nil {
				t.Fatal(err)
			}
			approved, _ := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
			if _, err := ApplyReversal(root, plan, approved); err != nil {
				t.Fatal(err)
			}
			if string(mustRead(t, legacyPath)) != string(raw) {
				t.Fatal("Legacy bytes were not restored exactly")
			}
			info, err := os.Stat(legacyPath)
			if err != nil || info.Mode().Perm() != expectedMode {
				t.Fatalf("Legacy mode was not restored: %v err=%v", info.Mode(), err)
			}
		})
	}
}

func preparedMigrationFixture(t testing.TB, root string, kinds []string) (*ApplyEnvelope, *Approval) {
	t.Helper()
	snapshot, err := CaptureSnapshot(root, "en-US", kinds, "2026-07-30T00:00:00Z")
	if err != nil || snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityEligible {
		t.Fatalf("Snapshot invalid: %#v err=%v", snapshot, err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: kinds})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, kinds)
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("D2-A Preview invalid: %#v err=%v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorApplyGradeMapping(t, template)
	mapping, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2,
		Snapshot: *snapshot, Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview,
		BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, approval
}

func migrationFixture(t testing.TB, withDatabase bool) string {
	t.Helper()
	root := t.TempDir()
	writeMigrationFile(t, root, "src/main.go", "package main\n\nfunc main() {}\n")
	if withDatabase {
		installMigrationDatabaseEvidence(t, root)
	} else {
		cfg := config.DefaultConfig()
		cfg.LedgerEnabled = true
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{
		"#AOCI-CLI Complete Index",
		"#Project: Model-authored legacy fixture",
		"#[Tag dictionary]",
		"#A Layer: C Code",
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===",
		"main.go[CD9S]: F:Preserve the legacy code responsibility | R:- | A:- | S:Keep source behavior byte-stable during cognition migration",
	}
	if withDatabase {
		lines = append(lines,
			"===Database/database://primary/fixture/===",
			"items[DB9S]: F:Preserve durable item identity | R:code:src/main.go | A:id | S:Writes and their audit record commit together",
		)
	}
	writeMigrationFile(t, root, "aoci.txt", strings.Join(lines, "\n")+"\n")
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, root, ".aoci/baseline.json", string(data))
	return root
}

func migrationCandidate(root string, plan *cognitionplan.Plan, kinds []string) *cognitionplan.LayoutCandidate {
	rootLines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + plan.Locale,
		"#Project: Reviewed migrated fixture", "#Global-Invariants: Preserve approved semantic ownership",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled"}
	for _, kind := range kinds {
		format := "object-fras-v2"
		path := "aoci.code.txt"
		if kind == "database" {
			format, path = "table-fras-v2", "aoci.database.txt"
		}
		rootLines = append(rootLines, fmt.Sprintf("#Volume: id=%s kind=%s path=%s format=%s depends=meta state=enabled", kind, kind, path, format))
	}
	metaLines := []string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table", "#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain F Frontend A API S Service", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
		"#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T"}
	for index := 0; index < 20; index++ {
		metaLines = append(metaLines, fmt.Sprintf("#Reviewed-Rule-%02d: model-owned semantic calibration", index))
	}
	assets := []cognitionplan.CandidateAsset{{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
		{AssetID: "meta", Path: "aoci.meta.txt", Content: strings.Join(metaLines, "\n") + "\n"}}
	legacy, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	for _, kind := range kinds {
		if kind == "code" {
			line := findLine(string(legacy), "main.go[")
			content := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + line + "\n"
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "code", Path: "aoci.code.txt",
				Content: content})
		} else {
			line := findLine(string(legacy), "items[")
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "database", Path: "aoci.database.txt",
				Content: cognition.DatabaseMarker + "\n===Database/database://primary/fixture/===\n" + line + "\n"})
		}
	}
	candidate := &cognitionplan.LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID, Assets: assets}
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution := cognitionplan.MappingResolution{UnitID: record.UnitID, TargetAsset: record.TargetAsset, TargetRef: record.TargetRef,
			Reviewer: "fixture-model", SemanticReviewed: true}
		if record.UnitKind == "entry" {
			if record.LegacySelfEntry {
				resolution.TargetAsset, resolution.TargetRef = cognition.OwnerRoot, ""
			} else if strings.HasPrefix(record.SourceText, "items[") {
				resolution.TargetAsset, resolution.TargetRef = "database", "database://primary/fixture/items"
			} else {
				resolution.TargetAsset, resolution.TargetRef = "code", "code:src/main.go"
			}
		} else if record.UnitKind == "header_semantic_atom" {
			resolution.TargetAsset = "root"
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	return candidate
}

func cognitionObjectExists(asset *cognition.Asset, objectRef string) bool {
	if asset == nil {
		return false
	}
	for _, object := range asset.Objects {
		if object.CanonicalRef == objectRef {
			return true
		}
	}
	return false
}

func pilotMigrationCandidate(root string, plan *cognitionplan.Plan, legacy string) *cognitionplan.LayoutCandidate {
	candidate := migrationCandidate(root, plan, []string{"code", "database"})
	entries := map[string]string{"App.jsx": findLine(legacy, "App.jsx["), "api.js": findLine(legacy, "api.js["),
		"service.js": findLine(legacy, "service.js["), "items": findLine(legacy, "items[")}
	for index := range candidate.Assets {
		switch candidate.Assets[index].AssetID {
		case "code":
			candidate.Assets[index].Content = cognition.CodeVolumeMarker + "\n" +
				"===Code " + filepath.ToSlash(filepath.Join(root, "frontend", "src")) + "/===\n" + entries["App.jsx"] + "\n" +
				"===Code " + filepath.ToSlash(filepath.Join(root, "backend", "src")) + "/===\n" + entries["api.js"] + "\n" + entries["service.js"] + "\n"
		case "database":
			candidate.Assets[index].Content = cognition.DatabaseMarker + "\n===Database/database://primary/fixture/===\n" + entries["items"] + "\n"
		}
	}
	refs := map[string]struct{ asset, ref string }{
		"App.jsx[": {"code", "code:frontend/src/App.jsx"}, "api.js[": {"code", "code:backend/src/api.js"},
		"service.js[": {"code", "code:backend/src/service.js"}, "items[": {"database", "database://primary/fixture/items"},
	}
	for index := range candidate.MappingResolutions {
		for prefix, target := range refs {
			for _, record := range plan.Mapping.Records {
				if record.UnitID == candidate.MappingResolutions[index].UnitID && strings.HasPrefix(record.SourceText, prefix) {
					candidate.MappingResolutions[index].TargetAsset = target.asset
					candidate.MappingResolutions[index].TargetRef = target.ref
				}
			}
		}
	}
	return candidate
}

func authorApplyGradeMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	used := map[string]bool{}
	targetByKindSHA := map[string][]TargetRange{}
	targetByKindSHAObject := map[string][]TargetRange{}
	semanticTargets := []TargetRange{}
	for _, target := range result.TargetRanges {
		key := target.Kind + "\x00" + target.SHA256
		targetByKindSHA[key] = append(targetByKindSHA[key], target)
		targetByKindSHAObject[key+"\x00"+target.Object] = append(targetByKindSHAObject[key+"\x00"+target.Object], target)
		if target.Kind != "entry" && target.Object == "" {
			semanticTargets = append(semanticTargets, target)
		}
	}
	popUnused := func(values []TargetRange) (TargetRange, bool) {
		for _, value := range values {
			if !used[value.Identity] {
				return value, true
			}
		}
		return TargetRange{}, false
	}
	taskByID := map[string]MappingAuthoringTask{}
	for _, task := range template.AuthoringTasks {
		taskByID[task.TaskID] = task
	}
	recordObject := map[string]string{}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		var target TargetRange
		found := false
		parentObject := ""
		if record.ParentSourceIdentity != "" {
			parentObject = recordObject[record.ParentSourceIdentity]
		}
		if record.SourceKind == "entry" || record.ParentSourceIdentity != "" {
			key := record.SourceKind + "\x00" + record.SourceSHA256
			if parentObject != "" {
				target, found = popUnused(targetByKindSHAObject[key+"\x00"+parentObject])
			} else {
				target, found = popUnused(targetByKindSHA[key])
			}
		}
		if found {
			record.SemanticRole = "preserved_model_owned_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMappingPreserved
			record.AuthoringTaskID = ""
		} else {
			target, found = popUnused(semanticTargets)
			if !found {
				t.Fatalf("no target range for source %s kind=%s", record.SourceIdentity, record.SourceKind)
			}
			record.SemanticRole = "model_reviewed_migration_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			task := taskByID[record.AuthoringTaskID]
			task.TargetAsset, task.TargetObject = target.Asset, target.Object
			task.CandidateRangeIdentities = []string{target.Identity}
			task.Status, task.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			taskByID[task.TaskID] = task
		}
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		used[target.Identity] = true
		recordObject[record.SourceIdentity] = record.TargetObject
	}
	result.AuthoringTasks = []MappingAuthoringTask{}
	for _, task := range taskByID {
		if task.Status == machinecontract.CognitionMigrationSemanticReviewed {
			result.AuthoringTasks = append(result.AuthoringTasks, task)
		}
	}
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	return &result
}

func authorSelfEntryRootMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	result := authorApplyGradeMapping(t, template)
	targetEntrySHA := map[string]bool{}
	for _, target := range result.TargetRanges {
		if target.Kind == "entry" {
			targetEntrySHA[target.SHA256] = true
		}
	}
	selfParent := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" && !targetEntrySHA[record.SourceSHA256] {
			selfParent = record.SourceIdentity
			break
		}
	}
	if selfParent == "" {
		t.Fatal("Legacy self-entry source missing")
	}
	groupSources := map[string]bool{selfParent: true}
	changed := true
	for changed {
		changed = false
		for _, record := range result.Records {
			if groupSources[record.ParentSourceIdentity] && !groupSources[record.SourceIdentity] {
				groupSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	usedByOther := map[string]bool{}
	for _, record := range result.Records {
		if !groupSources[record.SourceIdentity] {
			usedByOther[record.TargetSemanticRangeIdentity] = true
		}
	}
	var rootTarget TargetRange
	for _, target := range result.TargetRanges {
		if target.Asset == cognition.OwnerRoot && target.Kind == "root_semantic" {
			rootTarget = target
			break
		}
	}
	if rootTarget.Identity == "" {
		t.Fatal("Root semantic target missing")
	}
	if usedByOther[rootTarget.Identity] {
		targetByID := map[string]TargetRange{}
		for _, target := range result.TargetRanges {
			targetByID[target.Identity] = target
		}
		var replacement TargetRange
		for _, record := range result.Records {
			if groupSources[record.SourceIdentity] && record.TargetSemanticRangeIdentity != rootTarget.Identity {
				replacement = targetByID[record.TargetSemanticRangeIdentity]
				break
			}
		}
		if replacement.Identity == "" {
			t.Fatal("self-entry replacement target missing")
		}
		for index := range result.Records {
			record := &result.Records[index]
			if groupSources[record.SourceIdentity] || record.TargetSemanticRangeIdentity != rootTarget.Identity {
				continue
			}
			record.TargetAsset, record.TargetObject = replacement.Asset, replacement.Object
			record.TargetSemanticRangeIdentity = replacement.Identity
			for taskIndex := range result.AuthoringTasks {
				task := &result.AuthoringTasks[taskIndex]
				if task.TaskID == record.AuthoringTaskID {
					task.TargetAsset, task.TargetObject = replacement.Asset, replacement.Object
					task.CandidateRangeIdentities = []string{replacement.Identity}
				}
			}
			break
		}
	}
	removedTasks := map[string]bool{}
	sourceIDs, evidenceRefs := []string{}, []string{}
	for index := range result.Records {
		record := &result.Records[index]
		if !groupSources[record.SourceIdentity] {
			continue
		}
		removedTasks[record.AuthoringTaskID] = true
		sourceIDs = append(sourceIDs, record.SourceIdentity)
		evidenceRefs = append(evidenceRefs, "legacy:"+record.SourceSHA256)
		record.SemanticRole = "model_reviewed_root_ownership"
		record.TargetAsset, record.TargetObject = cognition.OwnerRoot, ""
		record.TargetSemanticRangeIdentity = rootTarget.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.AuthoringTaskID, record.MappingGroupID = "author:self-entry-root", "group:self-entry-root"
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
	}
	sort.Strings(sourceIDs)
	sort.Strings(evidenceRefs)
	evidenceRefs = uniqueStrings(evidenceRefs)
	tasks := result.AuthoringTasks[:0]
	sourceEvidenceIdentity := ""
	for _, task := range result.AuthoringTasks {
		if sourceEvidenceIdentity == "" {
			sourceEvidenceIdentity = task.SourceEvidenceIdentity
		}
		if !removedTasks[task.TaskID] {
			tasks = append(tasks, task)
		}
	}
	tasks = append(tasks, MappingAuthoringTask{TaskID: "author:self-entry-root", SourceIdentities: sourceIDs,
		SourceEvidenceRefs: evidenceRefs, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: cognition.OwnerRoot,
		CandidateRangeIdentities: []string{rootTarget.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed,
		Reviewer: "fixture-reviewer"})
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	result.AuthoringTasks = tasks
	result.MappingGroups = append(result.MappingGroups, MappingGroup{MappingGroupID: "group:self-entry-root",
		SourceIdentities: sourceIDs, TargetRangeIdentities: []string{rootTarget.Identity}, AuthoringTaskID: "author:self-entry-root",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"})
	sort.Slice(result.MappingGroups, func(i, j int) bool {
		return result.MappingGroups[i].MappingGroupID < result.MappingGroups[j].MappingGroupID
	})
	return result
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func authorRegeneratedEntryMapping(t testing.TB, template *MigrationMapping, object string) *MigrationMapping {
	t.Helper()
	sourceEvidenceIdentity := template.AuthoringTasks[0].SourceEvidenceIdentity
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	entrySource := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" {
			entrySource = record.SourceIdentity
			break
		}
	}
	groupSources := map[string]bool{entrySource: true}
	changed := true
	for changed {
		changed = false
		for _, record := range result.Records {
			if groupSources[record.ParentSourceIdentity] && !groupSources[record.SourceIdentity] {
				groupSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	used := map[string]bool{}
	groupSourceIDs, groupTargetIDs := []string{}, []string{}
	for index := range result.Records {
		record := &result.Records[index]
		if !groupSources[record.SourceIdentity] {
			continue
		}
		var target TargetRange
		found := false
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Object == object && candidate.Kind == record.SourceKind {
				target, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("regenerated group target missing for %s", record.SourceKind)
		}
		record.SemanticRole = "model_regenerated_object_semantics"
		record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.AuthoringTaskID = "author:group-code-main"
		record.MappingGroupID = "group:code-main"
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		groupSourceIDs = append(groupSourceIDs, record.SourceIdentity)
		groupTargetIDs = append(groupTargetIDs, target.Identity)
		used[target.Identity] = true
	}
	sort.Strings(groupSourceIDs)
	sort.Strings(groupTargetIDs)
	tasks := []MappingAuthoringTask{{TaskID: "author:group-code-main", SourceIdentities: groupSourceIDs,
		SourceEvidenceRefs: []string{"source:code:src/main.go"}, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: "code", TargetObject: object,
		CandidateRangeIdentities: groupTargetIDs, Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly || groupSources[record.SourceIdentity] {
			continue
		}
		var target TargetRange
		found := false
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Kind != "entry" && candidate.Object == "" {
				target, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("header target missing")
		}
		record.SemanticRole = "model_reviewed_migration_semantics"
		record.TargetAsset, record.TargetSemanticRangeIdentity = target.Asset, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		task := MappingAuthoringTask{TaskID: record.AuthoringTaskID, SourceIdentities: []string{record.SourceIdentity},
			SourceEvidenceRefs: []string{"legacy:" + record.SourceSHA256}, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: target.Asset,
			CandidateRangeIdentities: []string{target.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}
		tasks = append(tasks, task)
		used[target.Identity] = true
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	result.AuthoringTasks = tasks
	result.MappingGroups = []MappingGroup{{MappingGroupID: "group:code-main", SourceIdentities: groupSourceIDs,
		TargetRangeIdentities: groupTargetIDs, AuthoringTaskID: "author:group-code-main",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	return &result
}

func authorSplitEntryMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	sourceEvidenceIdentity := template.AuthoringTasks[0].SourceEvidenceIdentity
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	entrySources := map[string]bool{}
	entryParent := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" {
			entryParent = record.SourceIdentity
			entrySources[record.SourceIdentity] = true
		}
	}
	for _, record := range result.Records {
		if record.ParentSourceIdentity == entryParent {
			entrySources[record.SourceIdentity] = true
		}
	}
	codeTargets, databaseTargets := map[string]TargetRange{}, map[string]TargetRange{}
	groupTargetIDs := []string{}
	for _, target := range result.TargetRanges {
		if target.Object == "code:src/main.go" {
			codeTargets[target.Kind] = target
			groupTargetIDs = append(groupTargetIDs, target.Identity)
		}
		if target.Object == "database://primary/fixture/items" {
			databaseTargets[target.Kind] = target
			groupTargetIDs = append(groupTargetIDs, target.Identity)
		}
	}
	sort.Strings(groupTargetIDs)
	groupSourceIDs := []string{}
	used := map[string]bool{}
	headerTasks := []MappingAuthoringTask{}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		if entrySources[record.SourceIdentity] {
			target := codeTargets[record.SourceKind]
			if record.SourceKind == "R" || record.SourceKind == "S" {
				target = databaseTargets[record.SourceKind]
			}
			if target.Identity == "" {
				t.Fatalf("split target missing for %s", record.SourceKind)
			}
			record.SemanticRole = "model_owned_split_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			record.AuthoringTaskID, record.MappingGroupID = "author:mixed-split", "group:mixed-split"
			record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			groupSourceIDs = append(groupSourceIDs, record.SourceIdentity)
			used[target.Identity] = true
			continue
		}
		var target TargetRange
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Kind != "entry" && candidate.Object == "" {
				target = candidate
				break
			}
		}
		if target.Identity == "" {
			t.Fatalf("header target missing for %s", record.SourceIdentity)
		}
		record.SemanticRole = "model_reviewed_migration_semantics"
		record.TargetAsset, record.TargetSemanticRangeIdentity = target.Asset, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		headerTasks = append(headerTasks, MappingAuthoringTask{TaskID: record.AuthoringTaskID,
			SourceIdentities: []string{record.SourceIdentity}, SourceEvidenceRefs: []string{"legacy:" + record.SourceSHA256},
			SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: target.Asset, CandidateRangeIdentities: []string{target.Identity},
			Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"})
		used[target.Identity] = true
	}
	sort.Strings(groupSourceIDs)
	splitTask := MappingAuthoringTask{TaskID: "author:mixed-split", SourceIdentities: groupSourceIDs,
		SourceEvidenceRefs:     []string{"evidence:database://primary/fixture/items", "source:code:src/main.go"},
		SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: "multiple", CandidateRangeIdentities: groupTargetIDs,
		Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}
	result.AuthoringTasks = append(headerTasks, splitTask)
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	result.MappingGroups = []MappingGroup{{MappingGroupID: "group:mixed-split", SourceIdentities: groupSourceIDs,
		TargetRangeIdentities: groupTargetIDs, AuthoringTaskID: splitTask.TaskID,
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	return &result
}

func installMigrationDatabaseEvidence(t testing.TB, root string) {
	t.Helper()
	zero := 0
	source := dbevidence.SourceConfig{SourceID: "primary", Engine: dbevidence.EngineMySQL, Database: "fixture", Namespaces: []string{"fixture"},
		CredentialEnv: "FIXTURE_DB_PASSWORD", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}
	cfg := config.DefaultConfig()
	cfg.LedgerEnabled = true
	cfg.DatabaseSources = []dbevidence.SourceConfig{source}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: source.SourceID, Engine: source.Engine,
		Database: source.Database, Namespaces: source.Namespaces, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{},
		IncludeTables: []string{}, ExcludeTables: []string{}, CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve", LowerCaseTableNames: &zero}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/fixture/items", Engine: source.Engine,
		SourceID: source.SourceID, Database: source.Database, Namespace: "fixture", Name: "items", Kind: "base_table",
		Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
}

func refreshLegacyBaseline(t testing.TB, root string) {
	t.Helper()
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func refreshPilotBaseline(t testing.TB, root string, paths []string) {
	t.Helper()
	files := map[string]baseline.Fingerprint{}
	for _, relative := range paths {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, root, ".aoci/baseline.json", string(data))
}

func failMigrationOnce(want string) func(string) error {
	fired := false
	return func(step string) error {
		if !fired && step == want {
			fired = true
			return os.ErrPermission
		}
		return nil
	}
}

func stateFor(status *TransactionStatus, path string) string {
	for _, target := range status.Targets {
		if target.Path == path {
			return target.DiskState
		}
	}
	return "missing"
}

func writeMigrationFile(t testing.TB, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustDecodeLegacy(t testing.TB, envelope *ApplyEnvelope) []byte {
	t.Helper()
	data, err := decodeSnapshotContent(envelope.Snapshot.LegacyContentBase64, envelope.Snapshot.LegacySHA256)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func findLine(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func migrationLedgerCount(root, transactionID string) int {
	events, _ := ledger.Recent(root, 0)
	count := 0
	for _, event := range events {
		if event.Op == "cognition_migration_apply" && event.RecoveryTransactionID == transactionID {
			count++
		}
	}
	return count
}

func cognitionPendingReversal(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".aoci", "transactions"))
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "reversal-") && strings.HasSuffix(entry.Name(), ".json") {
			result = append(result, entry.Name())
		}
	}
	return result, nil
}
