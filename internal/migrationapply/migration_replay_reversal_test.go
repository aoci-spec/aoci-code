// Replay and Reversal: the Legacy Snapshot, D2-A replay, Mapping, Prepare,
// Apply, and strict byte-exact Reversal contracts. Shared fixtures and
// helpers live in migration_test.go.
package migrationapply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
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

// Rs pointing nowhere are everywhere in historical indexes. That is not corruption, and migration must pass as usual; the machine does not check
// who an index entry's relation points at.
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
