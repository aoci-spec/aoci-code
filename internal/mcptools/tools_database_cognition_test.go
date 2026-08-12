package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func TestDatabaseMaintainPresentEmptyMultiTableApplyAndRetry(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"orders", "users"})
	maintainResult := handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase)
	rawMaintain := resText(t, maintainResult)
	if strings.Contains(rawMaintain, "TEST_DSN") || strings.Contains(rawMaintain, "password") || strings.Contains(rawMaintain, "business-row-sentinel") {
		t.Fatalf("Database Maintain leaked credential or business data: %s", rawMaintain)
	}
	maintain := decodeDatabaseMaintain(t, maintainResult)
	if maintain.Status != autoStatusRepairRequired || maintain.Plan == nil || maintain.Plan.TargetCount != 2 || maintain.Assessment.Summary.Missing != 2 {
		t.Fatalf("unexpected maintain plan: %#v", maintain)
	}
	input := databaseCandidateInput(maintain.Plan)
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if first.Status != autoStatusApplied || !first.Aligned || first.Applied != 2 {
		t.Fatalf("database batch did not apply atomically: %#v", first)
	}
	assertVolumeTerminalProofAction(t, first.NextAction, false)
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes["database"].ObjectCount != 2 {
		t.Fatalf("database Volume is invalid after apply: set=%#v err=%v", set, err)
	}
	state, _, err := baseline.Load(root)
	if err != nil || state.DatabaseCognition == nil || len(state.DatabaseCognition.Entries) != 2 {
		t.Fatalf("database bindings did not move with the Volume: state=%#v err=%v", state, err)
	}
	assessment := dbcognition.Assess(root, configSources(t, root), set, state)
	if !assessment.CognitionCurrent || assessment.Summary.Current != 2 {
		t.Fatalf("applied cognition is not current: %#v", assessment)
	}

	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Status != autoStatusApplied || !retry.Aligned || retry.Applied != 0 || retry.Metrics.DuplicateApplies != 1 {
		t.Fatalf("identical retry was not idempotent: %#v", retry)
	}
	assertVolumeTerminalProofAction(t, retry.NextAction, true)
}

func assertVolumeTerminalProofAction(t *testing.T, action string, duplicate bool) {
	t.Helper()
	for _, token := range []string{"remaining=0", "aoci_maintain", "Verify", "Aggregate Check", "Guide", "next_action=none"} {
		if !strings.Contains(action, token) {
			t.Fatalf("final Volumes Apply action is missing %q: %q", token, action)
		}
	}
	if duplicate != strings.Contains(action, "正式写入为0") {
		t.Fatalf("final Volumes Apply action has the wrong duplicate-write fact: duplicate=%t action=%q", duplicate, action)
	}
	if strings.Contains(action, "无需继续调用") {
		t.Fatalf("final Volumes Apply must not stop before terminal proof: %q", action)
	}
}

func TestMaintainAllReportsCodeAndDatabaseDebt(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	mainPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { println(\"changed\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeAll})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
		t.Fatal(err)
	}
	codeCandidates, databaseCandidates := 0, 0
	for _, candidate := range maintain.Candidates {
		switch candidate.Domain {
		case cognition.ScopeCode:
			codeCandidates++
		case cognition.ScopeDatabase:
			databaseCandidates++
		}
	}
	if maintain.RequestedScope != cognition.ScopeAll || maintain.Status != autoStatusRepairRequired || maintain.Aligned ||
		maintain.Result != volumegovernance.ResultAuthoringRequired || strings.Join(maintain.AffectedDomains, ",") != "code,database" ||
		codeCandidates != 1 || databaseCandidates != 1 || maintain.CodePlan == nil || maintain.DatabasePlan == nil ||
		maintain.SemanticGenerated || maintain.NetworkAccessed || strings.Contains(raw, errVolumeReadOnly) {
		t.Fatalf("scope=all did not return the shared actionable Volume plan: %#v raw=%s", maintain, raw)
	}
	for _, candidate := range maintain.Candidates {
		if candidate.CandidateID == "" || candidate.BatchID == "" ||
			(candidate.Domain == cognition.ScopeCode && candidate.SourceSHA256 == "") ||
			(candidate.Domain == cognition.ScopeDatabase && candidate.EvidenceSHA256 == "") {
			t.Fatalf("scope=all candidate lacks its source or Evidence binding: %#v", candidate)
		}
	}
}

func TestVolumeMaintainExplicitCodeUsesSharedGovernance(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	mainPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { println(\"changed\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeCode})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.RequestedScope != cognition.ScopeCode || maintain.Status != autoStatusRepairRequired || maintain.Aligned ||
		maintain.CodePlan == nil || maintain.DatabasePlan != nil || len(maintain.Candidates) != 1 ||
		maintain.Candidates[0].Domain != cognition.ScopeCode || maintain.Candidates[0].ObjectRef != "code:main.go" ||
		maintain.Candidates[0].SourceSHA256 == "" || maintain.Candidates[0].CandidateID == "" ||
		maintain.Candidates[0].BatchID == "" || strings.Contains(raw, errVolumeReadOnly) {
		t.Fatalf("scope=code did not return the shared actionable Code plan: %#v raw=%s", maintain, raw)
	}
}

func TestVolumeMaintainExplicitAllAlignedRemainsReadOnly(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	session := connectMCPClient(t, root)
	rootBefore := volumeFileText(t, root, "aoci.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeAll})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.RequestedScope != cognition.ScopeAll || maintain.Status != autoStatusApplied || !maintain.Aligned ||
		maintain.Result != volumegovernance.ResultAligned || len(maintain.Candidates) != 0 || strings.Contains(raw, errVolumeReadOnly) {
		t.Fatalf("aligned scope=all did not remain aligned: %#v raw=%s", maintain, raw)
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore || volumeFileText(t, root, "aoci.meta.txt") != metaBefore ||
		volumeFileText(t, root, "aoci.code.txt") != codeBefore {
		t.Fatal("aligned scope=all changed formal cognition")
	}
}

func TestDatabaseOnlyNoArgumentMaintainDeliversEvidenceBoundContract(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	session := connectMCPClient(t, root)
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	if strings.Contains(raw, "TEST_DSN") || strings.Contains(raw, "password") || strings.Contains(raw, "business-row-sentinel") {
		t.Fatalf("Database no-argument Maintain leaked credential or business data: %s", raw)
	}
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.Status != autoStatusRepairRequired || maintain.Result != volumegovernance.ResultAuthoringRequired ||
		strings.Join(maintain.AffectedDomains, ",") != cognition.ScopeDatabase || len(maintain.Candidates) != 1 ||
		maintain.DatabasePlan == nil || maintain.DatabasePlan.TargetCount != 1 || maintain.AuthoringMeta != metaBefore ||
		maintain.SemanticGenerated || maintain.NetworkAccessed {
		t.Fatalf("Database no-argument Maintain contract is incomplete: %#v", maintain)
	}
	candidate := maintain.Candidates[0]
	if candidate.Domain != cognition.ScopeDatabase || candidate.ObjectRef != "database://primary/public/users" ||
		candidate.EvidenceVersion == "" || candidate.EvidenceSHA256 == "" || candidate.CandidateID == "" || candidate.BatchID == "" {
		t.Fatalf("Database candidate lacks accepted-Evidence identity: %#v", candidate)
	}
	_ = deliveredVolumeExample(t, maintain.Instructions, maintain.AuthoringMeta, cognition.ScopeDatabase)
	if after := volumeFileText(t, root, "aoci.code.txt"); after != codeBefore {
		t.Fatal("Database no-argument Maintain changed the Code Volume")
	}
	if after := volumeFileText(t, root, "aoci.meta.txt"); after != metaBefore {
		t.Fatal("Database no-argument Maintain changed formal Meta")
	}
}

func TestDatabaseCreateCannotBypassEvidenceReceiptWithVolumeSHA(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	item := AtomicUpdateItem{
		ObjectRef:    "database://primary/public/users",
		NewEntry:     "users[DB7S]: F:store durable user state | R:- | A:id | S:-",
		SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt"),
	}
	before := volumeFileText(t, root, "aoci.database.txt")
	if _, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{item}); fail == nil ||
		!strings.Contains(fail.Msg, "database_create_requires_candidate_receipt") {
		t.Fatalf("legacy Volume binding created an unbound Database Entry: %+v", fail)
	}
	if after := volumeFileText(t, root, "aoci.database.txt"); after != before {
		t.Fatal("rejected unbound Database create modified the formal Volume")
	}
}

func TestDatabaseMaintainPagesDeterministicallyUntilCurrent(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"accounts", "audit_log", "order_items", "orders", "payments"})
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseCognitionBatchObjects = 2
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	wantTargets := []int{2, 2, 1}
	for page, want := range wantTargets {
		maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
		if maintain.Status != autoStatusRepairRequired || maintain.Plan == nil || maintain.Plan.TargetCount != want {
			t.Fatalf("page %d plan mismatch: %#v", page+1, maintain)
		}
		input := databaseCandidateInput(maintain.Plan)
		for left, right := 0, len(input)-1; left < right; left, right = left+1, right-1 {
			input[left], input[right] = input[right], input[left]
		}
		applied := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
		if applied.Status != autoStatusApplied || applied.Applied != want {
			t.Fatalf("page %d apply mismatch: %#v", page+1, applied)
		}
		if page < len(wantTargets)-1 && applied.Aligned {
			t.Fatalf("page %d claimed alignment with remaining tables", page+1)
		}
		if page < len(wantTargets)-1 {
			if !strings.Contains(applied.NextAction, "aoci_maintain") || strings.Contains(applied.NextAction, "Aggregate Check") {
				t.Fatalf("page %d must continue Maintain instead of terminal proof: %q", page+1, applied.NextAction)
			}
		} else {
			assertVolumeTerminalProofAction(t, applied.NextAction, false)
		}
	}
	final := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	if !final.Aligned || final.Status != autoStatusApplied || final.Assessment.Summary.Current != 5 {
		t.Fatalf("paged authoring did not reach current: %#v", final)
	}
}

func TestDatabaseCandidateRejectsChangedEvidenceAndVolumePreimage(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{"evidence", func(t *testing.T, root string) {
			table := databaseEvidenceTable("users")
			table.Columns = append(table.Columns, dbevidence.Column{Ordinal: 2, Name: "status", NativeType: "text", CanonicalType: "text", Nullable: false})
			writeDatabaseEvidenceFixture(t, root, []dbevidence.TableEvidence{table})
		}},
		{"volume", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(cognition.DatabaseMarker+"\n# third-party change\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := databaseCognitionWriteFixture(t, []string{"users"})
			maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
			before, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
			test.mutate(t, root)
			mutated, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
			result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", databaseCandidateInput(maintain.Plan)))
			if result.Status != autoStatusStopped || result.Applied != 0 {
				t.Fatalf("stale candidate was not rejected: %#v", result)
			}
			after, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
			if string(after) != string(mutated) {
				t.Fatal("rejected candidate modified the Database Volume")
			}
			if test.name == "evidence" && string(before) != string(after) {
				t.Fatal("Evidence-only mutation changed the cognition Volume")
			}
		})
	}
}

func TestDatabaseApplyCannotClaimAlignmentWithNewSourceBlocker(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{
		SourceID: "unavailable", Engine: dbevidence.EnginePostgreSQL, Database: "other", Namespaces: []string{"public"},
		CredentialEnv: "UNAVAILABLE_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", databaseCandidateInput(maintain.Plan)))
	if result.Aligned || !strings.Contains(joinedFindingText(result), "evidence_unavailable: source:unavailable") {
		t.Fatalf("an unconfigured-Evidence source was hidden by post-Apply alignment: %#v", result)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.database.txt"), "users[DB7S]") {
		t.Fatal("independent source blocker incorrectly rolled back the valid target Apply")
	}
}

func TestNoConfigurationAbsentVolumeIsNoDatabaseDebt(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	result := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	if !result.Aligned || result.Status != autoStatusApplied || result.Assessment.NextAction != machinecontract.DatabaseCognitionActionNoConfiguration ||
		result.Assessment.DatabaseVolumeState != cognition.AssetAbsent || result.Findings != nil {
		t.Fatalf("Code-only project acquired Database cognition debt: %#v", result)
	}
}

func TestLegacyMaintainAllKeepsNoDatabaseConfigurationAsNoOp(t *testing.T) {
	root := buildRepo(t)
	result := handleMaintainInput(root, "test-version", maintainIn{Scope: cognition.ScopeAll}, nil)
	var combined allMaintainResult
	if err := json.Unmarshal([]byte(resText(t, result)), &combined); err != nil {
		t.Fatal(err)
	}
	if combined.Status != combined.Code.Status || combined.Aligned != combined.Code.Aligned || !combined.Database.Aligned ||
		combined.Database.Assessment.NextAction != machinecontract.DatabaseCognitionActionNoConfiguration || len(combined.Database.Findings) != 0 {
		t.Fatalf("Legacy scope=all introduced Database debt or changed Code Maintain: %#v", combined)
	}
}

func TestMissingCandidateReceiptDoesNotLeakHostPath(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	if err := os.Remove(filepath.Join(root, ".aoci", "drafts", "database-cognition", "candidate-"+maintain.Plan.BatchID+".json")); err != nil {
		t.Fatal(err)
	}
	raw := resText(t, handleMCPUpdateBatch(root, "test-version", databaseCandidateInput(maintain.Plan)))
	var result autoResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "database_candidate_receipt_missing") || len(result.Findings) != 1 || strings.Contains(result.Findings[0].Cause, root) {
		t.Fatalf("candidate receipt diagnostic leaked a host path or lost its machine code: %s", raw)
	}
}

func TestDatabaseMaintainAbsentDoesNotCreateVolume(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		CredentialEnv: "TEST_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	if result.Status != autoStatusStopped || len(result.Findings) != 1 || result.Findings[0] != machinecontract.DatabaseCognitionActionSnapshotOrRepair {
		t.Fatalf("absent Volume was not explicit: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
		t.Fatalf("absent maintain created a Database Volume: %v", err)
	}
}

func TestDatabaseBindingFailureRecoversThroughExistingEntriesTransaction(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	input := databaseCandidateInput(maintain.Plan)
	originalSave := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected binding save failure") }
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	saveAtomicBaseline = originalSave
	t.Cleanup(func() { saveAtomicBaseline = originalSave })
	if first.Status != autoStatusStopped || first.Applied != 0 {
		t.Fatalf("binding failure was reported as success: %#v", first)
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, bound := baseline.FindDatabaseCognitionBinding(state, "database://primary/public/users"); bound {
		t.Fatal("failed Baseline save exposed a completed binding")
	}
	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Status != autoStatusApplied || !retry.Aligned || retry.Applied != 0 || retry.Metrics.DuplicateApplies != 1 {
		t.Fatalf("existing recovery did not complete the binding: %#v", retry)
	}
	state, _, err = baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, bound := baseline.FindDatabaseCognitionBinding(state, "database://primary/public/users"); !bound {
		t.Fatal("recovery did not persist the Evidence binding")
	}
}

func TestDatabaseBindingRecoverySurvivesLostCandidateReceipt(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	input := databaseCandidateInput(maintain.Plan)
	originalSave := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected binding save failure") }
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	saveAtomicBaseline = originalSave
	t.Cleanup(func() { saveAtomicBaseline = originalSave })
	if first.Status != autoStatusStopped || first.Applied != 0 {
		t.Fatalf("binding failure was reported as success: %#v", first)
	}
	if err := os.Remove(filepath.Join(root, ".aoci", "drafts", "database-cognition", "candidate-"+maintain.Plan.BatchID+".json")); err != nil {
		t.Fatal(err)
	}
	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Status != autoStatusApplied || !retry.Aligned || retry.Applied != 0 || retry.Metrics.DuplicateApplies != 1 {
		t.Fatalf("existing Entries recovery could not restore a lost candidate receipt: %#v", retry)
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	binding, bound := baseline.FindDatabaseCognitionBinding(state, "database://primary/public/users")
	if !bound || binding.TableEvidenceSHA256 != maintain.Plan.Candidates[0].TableEvidenceSHA256 {
		t.Fatalf("recovery did not restore the proven Evidence binding: %#v", binding)
	}
}

func TestDatabaseBindingRecoveryHardBlocksChangedEvidence(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	input := databaseCandidateInput(maintain.Plan)
	originalSave := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected binding save failure") }
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	saveAtomicBaseline = originalSave
	t.Cleanup(func() { saveAtomicBaseline = originalSave })
	if first.Status != autoStatusStopped || first.Applied != 0 {
		t.Fatalf("binding failure was reported as success: %#v", first)
	}
	postimage := volumeFileText(t, root, "aoci.database.txt")
	table := databaseEvidenceTable("users")
	table.Columns = append(table.Columns, dbevidence.Column{Ordinal: 2, Name: "status", NativeType: "text", CanonicalType: "text", Nullable: false})
	writeDatabaseEvidenceFixture(t, root, []dbevidence.TableEvidence{table})
	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Status != autoStatusStopped || retry.Applied != 0 || !strings.Contains(joinedFindingText(retry), "write_conflict") {
		t.Fatalf("third-party Evidence change did not hard-block recovery: %#v", retry)
	}
	if volumeFileText(t, root, "aoci.database.txt") != postimage {
		t.Fatal("blocked Evidence recovery changed the Database Volume postimage")
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, bound := baseline.FindDatabaseCognitionBinding(state, "database://primary/public/users"); bound {
		t.Fatal("blocked Evidence recovery advanced the Database Binding")
	}
}

func TestDatabaseUnbaselinedEntryAdvancesBindingOnce(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	entry := modelAuthoredDatabaseTestEntry("database://primary/public/users")
	volume := cognition.DatabaseMarker + "\n===Primary tables/database://primary/public/===\n" + entry + "\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(volume), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	if maintain.Plan == nil || maintain.Assessment.Summary.Unbaselined != 1 {
		t.Fatalf("existing unbound Entry was not offered: %#v", maintain)
	}
	input := databaseCandidateInput(maintain.Plan)
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if first.Status != autoStatusApplied || !first.Aligned || first.Applied != 1 || first.Metrics.DuplicateApplies != 0 {
		t.Fatalf("first binding-only Apply was misclassified: %#v", first)
	}
	second := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if second.Status != autoStatusApplied || !second.Aligned || second.Applied != 0 || second.Metrics.DuplicateApplies != 1 {
		t.Fatalf("binding-only retry was not idempotent: %#v", second)
	}
}

func TestDatabaseCandidateFailuresLeaveWholeBatchUnchanged(t *testing.T) {
	for _, test := range []struct {
		name       string
		mutate     func([]updateEntryItemIn) []updateEntryItemIn
		wantStatus string
	}{
		{"missing_item", func(input []updateEntryItemIn) []updateEntryItemIn { return input[:1] }, autoStatusStopped},
		{"duplicate_item", func(input []updateEntryItemIn) []updateEntryItemIn { return []updateEntryItemIn{input[0], input[0]} }, autoStatusRepairRequired},
		{"wrong_candidate", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].CandidateID = strings.Repeat("a", 64)
			return input
		}, autoStatusStopped},
		{"illegal_tag", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[ZZ7S]: F:durable purchase lifecycle | R:- | A:- | S:-"
			return input
		}, autoStatusRepairRequired},
		{"f_over_limit", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[DB7S]: F:" + strings.Repeat("x", 161) + " | R:- | A:- | S:-"
			return input
		}, autoStatusRepairRequired},
		{"r_over_limit", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[DB7S]: F:coordinate durable purchase lifecycle | R:" + strings.Repeat("code:main.go,", 8) + "code:main.go | A:- | S:-"
			return input
		}, autoStatusRepairRequired},
		{"a_over_limit", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[DB7S]: F:coordinate durable purchase lifecycle | R:- | A:a,b,c,d,e,f,g | S:-"
			return input
		}, autoStatusRepairRequired},
		{"s_over_limit", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[DB7S]: F:coordinate durable purchase lifecycle | R:- | A:- | S:" + strings.Repeat("x", 201)
			return input
		}, autoStatusRepairRequired},
		{"dangling_relation", func(input []updateEntryItemIn) []updateEntryItemIn {
			input[0].NewEntry = "orders[DB7S]: F:coordinate durable purchase lifecycle | R:database://primary/public/ghost | A:order identity | S:-"
			return input
		}, autoStatusRepairRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := databaseCognitionWriteFixture(t, []string{"orders", "users"})
			maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
			before, err := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
			if err != nil {
				t.Fatal(err)
			}
			result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", test.mutate(databaseCandidateInput(maintain.Plan))))
			if result.Status != test.wantStatus || result.Applied != 0 {
				t.Fatalf("unexpected rejection: %#v", result)
			}
			if test.wantStatus == autoStatusRepairRequired {
				if len(result.Findings) == 0 || !result.PreserveOtherCandidates || len(result.RetryScope) == 0 {
					t.Fatalf("repairable candidate rejection lacks retry evidence: %#v", result)
				}
				for _, finding := range result.Findings {
					if finding.CandidateIndex <= 0 || finding.Path == "" || finding.CanonicalObjectIdentity == "" ||
						finding.Domain == "" || finding.Field == "" || finding.RuleCode == "" ||
						finding.Expected == "" || finding.Actual == "" || finding.Cause == "" || finding.SafeRepairAction == "" {
						t.Fatalf("repairable candidate Finding has an implicit field: %+v", finding)
					}
				}
			}
			after, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
			if string(before) != string(after) {
				t.Fatal("rejected batch changed the formal Database Volume")
			}
		})
	}
}

func TestDatabaseCandidateRejectsTamperedEvidenceAndDisabledSource(t *testing.T) {
	t.Run("tampered_evidence", func(t *testing.T) {
		root := databaseCognitionWriteFixture(t, []string{"users"})
		maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
		candidate := maintain.Plan.Candidates[0]
		path := filepath.Join(dbevidence.RuntimeEvidenceRoot(root), filepath.FromSlash(candidate.EvidenceRef))
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", databaseCandidateInput(maintain.Plan)))
		if result.Status != autoStatusStopped || result.Applied != 0 {
			t.Fatalf("tampered Evidence was accepted: %#v", result)
		}
	})
	t.Run("source_disabled", func(t *testing.T) {
		root := databaseCognitionWriteFixture(t, []string{"users"})
		cfg, err := config.LoadBase(root)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DatabaseSources[0].Enabled = false
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
		if maintain.Status != autoStatusStopped || maintain.Assessment.BlockingSourceCount != 1 || maintain.Plan != nil {
			t.Fatalf("disabled source was not fail-closed: %#v", maintain)
		}
	})
}

func TestDatabaseAtomicWriteFailurePreservesVolumeAndBindings(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	before, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
	originalWrite := writeAtomicIndex
	writeAtomicIndex = func(string, []byte, string) error { return errors.New("injected AtomicWrite failure") }
	t.Cleanup(func() { writeAtomicIndex = originalWrite })
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", databaseCandidateInput(maintain.Plan)))
	writeAtomicIndex = originalWrite
	if result.Status != autoStatusStopped || result.Applied != 0 {
		t.Fatalf("AtomicWrite failure was reported as success: %#v", result)
	}
	after, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
	if string(before) != string(after) {
		t.Fatal("AtomicWrite failure changed the Database Volume")
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := baseline.FindDatabaseCognitionBinding(state, "database://primary/public/users"); exists {
		t.Fatal("AtomicWrite failure advanced the Evidence binding")
	}
}

func databaseCognitionWriteFixture(t *testing.T, tableNames []string) string {
	t.Helper()
	root := buildVolumeRepo(t, true, true)
	if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(cognition.DatabaseMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "TEST_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	tables := make([]dbevidence.TableEvidence, 0, len(tableNames))
	for _, name := range tableNames {
		tables = append(tables, databaseEvidenceTable(name))
	}
	writeDatabaseEvidenceFixture(t, root, tables)
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeDatabaseEvidenceFixture(t *testing.T, root string, tables []dbevidence.TableEvidence) {
	t.Helper()
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{}, CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, tables)
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
}

func databaseEvidenceTable(name string) dbevidence.TableEvidence {
	return dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/" + name, Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table", Columns: []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}}, PrimaryKey: &dbevidence.KeyConstraint{Name: name + "_pkey", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
}

func databaseCandidateInput(plan *dbcognition.Plan) []updateEntryItemIn {
	result := make([]updateEntryItemIn, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		result = append(result, updateEntryItemIn{ObjectRef: candidate.ObjectRef, CandidateID: candidate.CandidateID, BatchID: plan.BatchID, NewEntry: modelAuthoredDatabaseTestEntry(candidate.ObjectRef)})
	}
	return result
}

func modelAuthoredDatabaseTestEntry(objectRef string) string {
	name := objectRef[strings.LastIndexByte(objectRef, '/')+1:]
	entries := map[string]string{
		"accounts":    "accounts[DB7S]: F:hold the durable customer account boundary | R:- | A:account identity | S:account closure is a lifecycle decision and must not be inferred from schema nullability",
		"audit_log":   "audit_log[DB7S]: F:preserve append-only security audit events | R:- | A:audit event identity | S:records are append-only and retention is governed outside ordinary CRUD",
		"order_items": "order_items[DB7S]: F:bind ordered products to the commercial order snapshot | R:- | A:order-item identity | S:price facts are captured for the order and must not be recomputed from a later catalog value",
		"orders":      "orders[DB7S]: F:coordinate the durable purchase lifecycle | R:- | A:order identity | S:state transitions are committed with their dependent financial effects",
		"payments":    "payments[DB7S]: F:record payment attempts and their durable outcomes | R:- | A:payment identity | S:provider callbacks are idempotent at the payment boundary",
		"users":       "users[DB7S]: F:hold the durable application user identity | R:- | A:user identity | S:identity removal follows the application retention workflow rather than direct row deletion",
	}
	entry, exists := entries[name]
	if !exists {
		panic(fmt.Sprintf("missing explicit model-authored test Entry for %s", objectRef))
	}
	return entry
}

func configSources(t *testing.T, root string) []dbevidence.SourceConfig {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.DatabaseSources
}

func decodeDatabaseMaintain(t *testing.T, result *mcp.CallToolResult) databaseMaintainResult {
	t.Helper()
	var decoded databaseMaintainResult
	if err := json.Unmarshal([]byte(resText(t, result)), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
