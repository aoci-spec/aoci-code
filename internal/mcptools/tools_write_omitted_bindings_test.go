package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
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
