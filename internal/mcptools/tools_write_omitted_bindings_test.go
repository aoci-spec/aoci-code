package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// A batch names its objects; the digest and candidate id it leaves out are
// the issued receipt's, checked exactly as copied ones are.
func TestCodeBatchWithoutCopiedBindingsApplies(t *testing.T) {
	tests := []struct {
		name string
		drop []string
	}{
		{name: "both omitted", drop: []string{"source_sha256", "candidate_id"}},
		{name: "source omitted", drop: []string{"source_sha256"}},
		{name: "candidate omitted", drop: []string{"candidate_id"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			if maintain.CodePlan == nil || len(maintain.Candidates) != 2 {
				t.Fatalf("expected two Code candidates: %#v", maintain.CodePlan)
			}
			arguments := codeBatchArguments(wireCodePlan(maintain))
			for _, entry := range arguments["entries"].([]map[string]any) {
				for _, field := range test.drop {
					delete(entry, field)
				}
			}
			applied := applyVolumeBatch(t, session, arguments)
			if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 2 {
				t.Fatalf("a batch naming only its objects did not apply: %#v", applied)
			}
			assertVolumeTerminalProofAction(t, applied.NextAction, false)
			// The same submission again, and the fully copied form of it, are
			// the batch already applied: neither writes.
			for _, again := range []map[string]any{arguments, codeBatchArguments(wireCodePlan(maintain))} {
				duplicate := applyVolumeBatch(t, session, again)
				if duplicate.Status != autoStatusApplied || !duplicate.Aligned || duplicate.Applied != 0 {
					t.Fatalf("a repeat of the applied batch was not idempotent: %#v", duplicate)
				}
			}
		})
	}
}

// The completed digest is the one issued, so a source that moved after
// Maintain is refused exactly as it is for a copied digest.
func TestCodeBatchWithoutCopiedBindingsRefusesMovedSource(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	arguments := codeBatchArguments(wireCodePlan(maintain))
	for _, entry := range arguments["entries"].([]map[string]any) {
		delete(entry, "source_sha256")
		delete(entry, "candidate_id")
	}
	driftPath := filepath.Join(root, filepath.FromSlash(maintain.Candidates[0].Path))
	data, err := os.ReadFile(driftPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driftPath, append(data, []byte("// drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
	rejected := applyVolumeBatch(t, session, arguments)
	if rejected.Status == autoStatusApplied || rejected.Applied != 0 || rejected.FormalWritesStarted {
		t.Fatalf("a moved source was written under the issued digest: %#v", rejected)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
}

// Nothing can be completed for an object the batch never issued or for a
// batch that is not on record: both stop with zero writes and name the
// reason instead of asking for a binding the model cannot have.
func TestCodeBatchWithoutCopiedBindingsStopsOutsideTheIssuedBatch(t *testing.T) {
	tests := []struct {
		name  string
		alter func(map[string]any)
		want  string
	}{
		{name: "path not issued", want: "code_candidate_batch_mismatch", alter: func(arguments map[string]any) {
			entry := arguments["entries"].([]map[string]any)[1]
			entry["path"] = entry["path"].(string) + "x"
		}},
		{name: "batch not on record", want: "code_candidate", alter: func(arguments map[string]any) {
			arguments["code_batch_id"] = oneHexTypo(arguments["code_batch_id"].(string))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			arguments := codeBatchArguments(wireCodePlan(maintain))
			for _, entry := range arguments["entries"].([]map[string]any) {
				delete(entry, "source_sha256")
				delete(entry, "candidate_id")
			}
			test.alter(arguments)
			codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
			baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
			text := callVolumeTool(t, session, "aoci_update_entry", arguments)
			if !strings.Contains(text, test.want) || strings.Contains(text, "source_sha256 evidence binding") {
				t.Fatalf("the refusal does not name why nothing could be completed: %s", text)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
			assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
			// The untouched batch still applies afterwards.
			applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(maintain)))
			if applied.Status != autoStatusApplied || applied.Applied != 2 {
				t.Fatalf("the issued batch no longer applies after the refusal: %#v", applied)
			}
		})
	}
}

// A copied value is never replaced: a wrong one still answers its finding
// even when the other binding of the same item was left out.
func TestCodeBatchKeepsACopiedBindingBesideAnOmittedOne(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	arguments := codeBatchArguments(wireCodePlan(maintain))
	entry := arguments["entries"].([]map[string]any)[1]
	delete(entry, "candidate_id")
	entry["source_sha256"] = oneHexTypo(entry["source_sha256"].(string))
	rejected := applyVolumeBatch(t, session, arguments)
	if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted ||
		len(rejected.Findings) != 1 || rejected.Findings[0].Field != "source_sha256" {
		t.Fatalf("a wrong copied digest was replaced or misreported: %#v", rejected)
	}
}

func TestDatabaseBatchWithoutCopiedCandidateIDApplies(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	input := databaseCandidateInput(maintain.Plan)
	for index := range input {
		input[index].CandidateID = ""
	}
	applied := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != len(input) {
		t.Fatalf("a Database batch naming only its objects did not apply: %#v", applied)
	}
	// An object the batch never issued has nothing to be completed from.
	other := databaseCognitionWriteFixture(t, []string{"users"})
	otherMaintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(other, "test-version", cognition.ScopeDatabase))
	stray := databaseCandidateInput(otherMaintain.Plan)
	stray[0].CandidateID = ""
	stray[0].ObjectRef = "database://primary/public/absent"
	refused := decodeAutoResult(t, handleMCPUpdateBatch(other, "test-version", stray))
	if refused.Status == autoStatusApplied || refused.Applied != 0 || refused.FormalWritesStarted {
		t.Fatalf("an object outside the Database batch was written: %#v", refused)
	}
}

// Only a field left out is completed. An empty or blank value that was sent
// is the refusal it always was: nothing is written under it.
func TestExplicitEmptyBindingIsNotCompleted(t *testing.T) {
	for _, test := range []struct{ field, value string }{
		{"source_sha256", ""}, {"candidate_id", ""}, {"source_sha256", "  "}, {"candidate_id", " "},
	} {
		t.Run(test.field+"="+strings.ReplaceAll(test.value, " ", "_"), func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			arguments := codeBatchArguments(wireCodePlan(maintain))
			arguments["entries"].([]map[string]any)[1][test.field] = test.value
			codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
			rejected := applyVolumeBatch(t, session, arguments)
			if rejected.Status == autoStatusApplied || rejected.Applied != 0 || rejected.FormalWritesStarted {
				t.Fatalf("an explicitly empty %s was completed and written: %#v", test.field, rejected)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
		})
	}
	root := databaseCognitionWriteFixture(t, []string{"users"})
	session := connectMCPClient(t, root)
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	entries := []map[string]any{}
	for _, item := range databaseCandidateInput(maintain.Plan) {
		entries = append(entries, map[string]any{"object_ref": item.ObjectRef, "candidate_id": "", "new_entry": item.NewEntry})
	}
	refused := applyVolumeBatch(t, session, map[string]any{"batch_id": maintain.Plan.BatchID, "entries": entries})
	if refused.Status == autoStatusApplied || refused.Applied != 0 || refused.FormalWritesStarted {
		t.Fatalf("an explicitly empty Database candidate_id was completed and written: %#v", refused)
	}
}

// A batch nothing can be completed for records no Observe review either:
// the refusal that reports zero formal writes leaves the Baseline as it was.
func TestUncompletableBatchRecordsNoObserveReview(t *testing.T) {
	root := pendingObserveRepoWithWork(t, config.AutomationModeAuto)
	maintain := maintainResultOf(t, root)
	if maintain.ObserveReview == nil || len(maintain.Candidates) != 1 {
		t.Fatalf("the fixture issued no review beside a batch: %#v", maintain)
	}
	session := connectMCPClient(t, root)
	arguments := codeBatchArguments(wireCodePlan(maintain))
	for _, entry := range arguments["entries"].([]map[string]any) {
		delete(entry, "source_sha256")
		delete(entry, "candidate_id")
	}
	arguments["code_batch_id"] = oneHexTypo(arguments["code_batch_id"].(string))
	arguments["observe_review"] = map[string]any{"review_digest": maintain.ObserveReview.ReviewDigest, "reviewed_by": "codex-agent"}
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
	refused := applyVolumeBatch(t, session, arguments)
	if refused.Status == autoStatusApplied || refused.FormalWritesStarted || refused.ObserveReview != nil {
		t.Fatalf("a batch on no record still recorded its review: %#v review=%#v", refused, refused.ObserveReview)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
	if after := maintainResultOf(t, root); after.ObserveReview == nil {
		t.Fatal("the review no longer waits although nothing was confirmed")
	}
}

// A Database batch whose receipt is gone names the receipt and the two ways
// on, never a source_sha256 that Database items may not carry.
func TestDatabaseBatchWithoutReceiptNamesTheReceipt(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	maintain := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase))
	input := databaseCandidateInput(maintain.Plan)
	for index := range input {
		input[index].CandidateID = ""
	}
	matches, err := filepath.Glob(filepath.Join(root, ".aoci", "drafts", "*", "*"+maintain.Plan.BatchID+"*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no Database receipt to remove: %v %v", matches, err)
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			t.Fatal(err)
		}
	}
	text := resText(t, handleMCPUpdateBatch(root, "test-version", input))
	if !strings.Contains(text, "database_candidate_receipt_missing") || !strings.Contains(text, "candidate_id") ||
		strings.Contains(text, "source_sha256") {
		t.Fatalf("the refusal does not name the missing receipt and the way on: %s", text)
	}
}

// The single-Entry compatibility mode keeps its contract: nothing is
// completed there, so an empty binding is the refusal it was. A Database
// batch item never carries a source digest, an empty one included.
func TestCompatibilityFormsAreNotCompleted(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 1)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	entry := codeBatchArguments(wireCodePlan(maintain))["entries"].([]map[string]any)[0]
	for _, field := range []string{"source_sha256", "candidate_id"} {
		single := map[string]any{"code_batch_id": maintain.CodePlan.BatchID, "path": entry["path"],
			"new_entry": entry["new_entry"], "source_sha256": entry["source_sha256"], "candidate_id": entry["candidate_id"]}
		single[field] = ""
		codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
		text := callVolumeTool(t, session, "aoci_update_entry", single)
		if strings.Contains(text, `"status":"applied"`) {
			t.Fatalf("the single-Entry mode completed an empty %s: %s", field, text)
		}
		assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
	}
	database := databaseCognitionWriteFixture(t, []string{"users"})
	databaseSession := connectMCPClient(t, database)
	plan := decodeDatabaseMaintain(t, handleDatabaseMaintain(database, "test-version", cognition.ScopeDatabase)).Plan
	entries := []map[string]any{}
	for _, item := range databaseCandidateInput(plan) {
		entries = append(entries, map[string]any{"object_ref": item.ObjectRef, "candidate_id": item.CandidateID,
			"source_sha256": "", "new_entry": item.NewEntry})
	}
	refused := callVolumeTool(t, databaseSession, "aoci_update_entry", map[string]any{"batch_id": plan.BatchID, "entries": entries})
	if strings.Contains(refused, `"status":"applied"`) || !strings.Contains(refused, "database_candidate_binding_fields_conflict") {
		t.Fatalf("a Database item carrying an empty source digest was written: %s", refused)
	}
}

// Every batch refused for a missing binding leaves the Observe review
// unrecorded, whichever way the binding went missing.
func TestBatchMissingABindingRecordsNoObserveReview(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(map[string]any)
	}{
		{"no batch id", func(arguments map[string]any) { delete(arguments, "code_batch_id") }},
		{"unusable path", func(arguments map[string]any) { arguments["entries"].([]map[string]any)[0]["path"] = "/" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := pendingObserveRepoWithWork(t, config.AutomationModeAuto)
			maintain := maintainResultOf(t, root)
			session := connectMCPClient(t, root)
			arguments := codeBatchArguments(wireCodePlan(maintain))
			for _, entry := range arguments["entries"].([]map[string]any) {
				delete(entry, "source_sha256")
				delete(entry, "candidate_id")
			}
			test.alter(arguments)
			arguments["observe_review"] = map[string]any{"review_digest": maintain.ObserveReview.ReviewDigest, "reviewed_by": "codex-agent"}
			baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
			text := callVolumeTool(t, session, "aoci_update_entry", arguments)
			if strings.Contains(text, `"status":"applied"`) || strings.Contains(text, "acknowledged") {
				t.Fatalf("a batch missing a binding still recorded its review: %s", text)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
		})
	}
}

// The single-Entry Database form (object_ref, batch_id, candidate_id, no
// source digest) is a legal request and still applies.
func TestSingleDatabaseEntryWithoutSourceDigestStillApplies(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	session := connectMCPClient(t, root)
	plan := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "test-version", cognition.ScopeDatabase)).Plan
	item := databaseCandidateInput(plan)[0]
	text := callVolumeTool(t, session, "aoci_update_entry", map[string]any{"object_ref": item.ObjectRef,
		"batch_id": plan.BatchID, "candidate_id": item.CandidateID, "new_entry": item.NewEntry})
	if !strings.Contains(text, `"status":"applied"`) || strings.Contains(text, "database_candidate_binding_fields_conflict") {
		t.Fatalf("the legal single-Entry Database form was refused: %s", text)
	}
}

func TestDatabaseDigestConflictIsKnownBeforeTheObserveReview(t *testing.T) {
	conflict := []updateEntryItemIn{{ObjectRef: "database://primary/public/users", BatchID: strings.Repeat("a", 64),
		CandidateID: strings.Repeat("b", 64), NewEntry: "users[DB7S]: F:x | R:- | A:- | S:-", sourceGiven: true}}
	if !bindingsIncomplete(conflict) {
		t.Fatal("a Database batch item carrying an empty digest is not refused before the review")
	}
	conflict[0].sourceGiven = false
	if bindingsIncomplete(conflict) {
		t.Fatal("a legal Database batch item is refused before the review")
	}
}
