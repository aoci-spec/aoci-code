package mcptools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

const optimizationCheckpointRelativePath = ".aoci/drafts/code-cognition/optimization-active.json"

type optimizationProgressPayload struct {
	Version              string `json:"version"`
	OptimizationID       string `json:"optimization_id"`
	State                string `json:"state"`
	CurrentBatchID       string `json:"current_batch_id"`
	TotalTargets         int    `json:"total_targets"`
	Included             int    `json:"included"`
	Reviewed             int    `json:"reviewed"`
	NoChange             int    `json:"no_change"`
	Replaced             int    `json:"replaced"`
	Remaining            int    `json:"remaining"`
	ContinuationRequired bool   `json:"continuation_required"`
}

type optimizationCandidatePayload struct {
	ObjectRef           string                     `json:"object_ref"`
	Path                string                     `json:"path"`
	ExistingEntry       string                     `json:"existing_entry"`
	ExistingEntrySHA256 string                     `json:"existing_entry_sha256"`
	SourceSHA256        string                     `json:"source_sha256"`
	CandidateID         string                     `json:"candidate_id"`
	BatchID             string                     `json:"batch_id"`
	Importance          int                        `json:"importance"`
	Cost                map[string]json.RawMessage `json:"cost"`
	SelectionReason     string                     `json:"selection_reason"`
}

type optimizationMaintainPayload struct {
	Version      int                            `json:"version"`
	Status       string                         `json:"status"`
	Aligned      bool                           `json:"aligned"`
	Candidates   []optimizationCandidatePayload `json:"candidates"`
	CodePlan     *codebatch.Plan                `json:"code_plan"`
	Batch        volumeAuthoringBatch           `json:"authoring_batch"`
	Governance   *volumegovernance.Facts        `json:"governance"`
	Optimization *optimizationProgressPayload   `json:"optimization"`
}

type optimizationUpdatePayload struct {
	Version             int                          `json:"version"`
	Status              string                       `json:"status"`
	Aligned             bool                         `json:"aligned"`
	Attempted           int                          `json:"attempted"`
	Applied             int                          `json:"applied"`
	FormalWritesStarted bool                         `json:"formal_writes_started"`
	Optimization        *optimizationProgressPayload `json:"optimization"`
	NextAction          string                       `json:"next_action"`
}

func TestCognitionOptimizationOrdinaryMaintainDoesNotCreateCheckpoint(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	checkpoint := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))

	ordinary := maintainVolumeBatch(t, connectMCPClient(t, root))
	if ordinary.Status != autoStatusApplied || !ordinary.Aligned || len(ordinary.Candidates) != 0 || ordinary.CodePlan != nil {
		t.Fatalf("ordinary aligned Maintain changed behavior: %#v", ordinary)
	}
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("ordinary Maintain created an optimization checkpoint: %v", err)
	}
}

func TestCognitionOptimizationMaintainSelectsAlignedCandidatesDeterministicallyAndBoundsBatch(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 203)
	session := connectMCPClient(t, root)

	first := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, first, 203, 200, 3)
	firstRefs := optimizationCandidateRefs(first.Candidates)
	if !optimizationContainsString(firstRefs, "code:optimized/0201.go") || !optimizationContainsString(firstRefs, "code:optimized/0202.go") {
		t.Fatalf("C/token-prioritized candidates were lost at the 200 item boundary: %v", firstRefs[len(firstRefs)-5:])
	}
	for _, candidate := range first.Candidates {
		assertOptimizationCostContract(t, candidate)
	}

	second := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, second, 203, 200, 3)
	if got := optimizationCandidateRefs(second.Candidates); !reflect.DeepEqual(got, firstRefs) {
		t.Fatalf("active optimization batch selection is not deterministic:\nfirst=%v\nsecond=%v", firstRefs, got)
	}
	if second.Optimization.OptimizationID != first.Optimization.OptimizationID ||
		second.Optimization.CurrentBatchID != first.Optimization.CurrentBatchID {
		t.Fatalf("repeated Maintain changed active identities: first=%#v second=%#v", first.Optimization, second.Optimization)
	}
}

func TestCognitionOptimizationAllNoChangeAdvancesCheckpointAndResumes201(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 201)
	session := connectMCPClient(t, root)
	refs := cognitionOptimizationObjectRefs(201)

	first := callCognitionOptimizationMaintain(t, session, reversedStrings(refs))
	assertOptimizationReviewBatch(t, first, 201, 200, 1)
	firstRefs := optimizationCandidateRefs(first.Candidates)
	firstUpdate := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(first, func(position int, candidate optimizationCandidatePayload) string {
		if position != 0 {
			return candidate.ExistingEntry
		}
		filenameEnd := strings.Index(candidate.ExistingEntry, "[")
		if filenameEnd < 1 {
			t.Fatalf("invalid current optimization Entry: %q", candidate.ExistingEntry)
		}
		// Exercise the existing Update normalizer: fenced input with a
		// repository-relative filename is the same complete Entry postimage.
		return "```text\n" + candidate.Path + candidate.ExistingEntry[filenameEnd:] + "\n```"
	}))
	assertOptimizationProgress(t, firstUpdate.Optimization, "in_progress", 201, 200, 200, 0, 1, true)
	if firstUpdate.Status != autoStatusApplied || firstUpdate.Applied != 0 || firstUpdate.FormalWritesStarted {
		t.Fatalf("identical complete Entries were not recorded as no_change: %#v", firstUpdate)
	}
	if firstUpdate.NextAction != "call_aoci_maintain_with_cognition_optimization" {
		t.Fatalf("in-progress optimization lost its dedicated continuation action: %#v", firstUpdate)
	}

	second := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, second, 201, 1, 0)
	secondRefs := optimizationCandidateRefs(second.Candidates)
	if len(secondRefs) != 1 || optimizationContainsString(firstRefs, secondRefs[0]) {
		t.Fatalf("optimization repeated an already reviewed object: first=%v second=%v", firstRefs, secondRefs)
	}
	secondUpdate := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(second, nil))
	assertOptimizationProgress(t, secondUpdate.Optimization, "complete", 201, 201, 201, 0, 0, false)
	if secondUpdate.Status != autoStatusApplied || secondUpdate.Applied != 0 || secondUpdate.FormalWritesStarted {
		t.Fatalf("final no_change batch did not close cleanly: %#v", secondUpdate)
	}
	if secondUpdate.NextAction != "none" {
		t.Fatalf("completed optimization changed its dedicated terminal action: %#v", secondUpdate)
	}

	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationManagedScopeAllNoChangeWritesOnlyCheckpoint(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(2))
	assertOptimizationReviewBatch(t, maintain, 2, 2, 0)
	assertManagedOptimizationRoleBehavior(t, root, maintain.Governance, 2)

	formalPaths := []string{"aoci.code.txt", ".aoci/baseline.json"}
	formalBefore := cognitionOptimizationFormalPreimages(t, root, formalPaths)
	checkpointPath := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	checkpointBefore := readOptimizationTestFile(t, checkpointPath)
	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, nil))

	assertOptimizationProgress(t, update.Optimization, "complete", 2, 2, 2, 0, 0, false)
	if update.Status != autoStatusApplied || !update.Aligned || update.Applied != 0 || update.FormalWritesStarted {
		t.Fatalf("managed all-no-change batch reported a formal write: %#v", update)
	}
	assertCognitionOptimizationFormalPreimages(t, root, formalBefore)
	if checkpointAfter := readOptimizationTestFile(t, checkpointPath); reflect.DeepEqual(checkpointAfter, checkpointBefore) {
		t.Fatal("managed all-no-change batch did not advance its draft checkpoint")
	}
	assertOptimizationCheckpointCounts(t, root, 2, 2, 0, true)
	assertNoActiveOptimizationRecovery(t, root)
	assertManagedOptimizationRoleBehavior(t, root, nil, 2)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationCompletedNoChangeBatchRejectsAlteredReplay(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(2))
	original := optimizationUpdateArguments(maintain, nil)
	completed := callCognitionOptimizationUpdate(t, session, original)
	assertOptimizationProgress(t, completed.Optimization, "complete", 2, 2, 2, 0, 0, false)

	formalPaths := []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json", optimizationCheckpointRelativePath}
	before := make(map[string][]byte, len(formalPaths))
	for _, rel := range formalPaths {
		before[rel] = readOptimizationTestFile(t, filepath.Join(root, filepath.FromSlash(rel)))
	}
	altered := optimizationUpdateArguments(maintain, func(position int, candidate optimizationCandidatePayload) string {
		if position == 0 {
			return refinedOptimizationEntry(t, candidate.ExistingEntry)
		}
		return candidate.ExistingEntry
	})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_update_entry", Arguments: altered})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if !strings.Contains(text, "cognition_optimization_completed_batch_payload_mismatch") ||
		!strings.Contains(text, `"applied":0`) || !strings.Contains(text, `"formal_writes_started":false`) {
		t.Fatalf("altered completed-batch replay did not fail before formal writes:\n%s", text)
	}
	for _, rel := range formalPaths {
		if after := readOptimizationTestFile(t, filepath.Join(root, filepath.FromSlash(rel))); !reflect.DeepEqual(after, before[rel]) {
			t.Fatalf("altered completed-batch replay changed %s", rel)
		}
	}

	replay := callCognitionOptimizationUpdate(t, session, original)
	assertOptimizationProgress(t, replay.Optimization, "complete", 2, 2, 2, 0, 0, false)
	if replay.Applied != 0 || replay.FormalWritesStarted {
		t.Fatalf("exact completed-batch replay was not idempotent: %#v", replay)
	}
}

func TestCognitionOptimizationMixedNoChangeAndReplacementReportsCounts(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	assertOptimizationReviewBatch(t, maintain, 3, 3, 0)

	replacement := refinedOptimizationEntry(t, maintain.Candidates[0].ExistingEntry)
	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, func(index int, candidate optimizationCandidatePayload) string {
		if index == 0 {
			return replacement
		}
		return candidate.ExistingEntry
	}))
	assertOptimizationProgress(t, update.Optimization, "complete", 3, 3, 2, 1, 0, false)
	// The reused atomic Update transaction may count every complete submitted
	// Entry as applied when one item changes the shared Code Volume. The
	// optimization projection is the authoritative semantic classification.
	if update.Status != autoStatusApplied || update.Applied < 1 || !update.FormalWritesStarted {
		t.Fatalf("mixed optimization batch did not apply through the existing transaction: %#v", update)
	}
	if actual := volumeFileText(t, root, "aoci.code.txt"); !strings.Contains(actual, replacement) {
		t.Fatalf("model-authored complete replacement was not written\nreplacement=%q\nactual=%s", replacement, actual)
	}
	replay := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, func(index int, candidate optimizationCandidatePayload) string {
		if index == 0 {
			return replacement
		}
		return candidate.ExistingEntry
	}))
	assertOptimizationProgress(t, replay.Optimization, "complete", 3, 3, 2, 1, 0, false)
	if replay.Applied != 0 || replay.FormalWritesStarted {
		t.Fatalf("lost-response replay repeated a formal optimization write: %#v", replay)
	}
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationManagedScopeMixedBatchCompletesGovernedTransaction(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(2))
	assertOptimizationReviewBatch(t, maintain, 2, 2, 0)
	assertManagedOptimizationRoleBehavior(t, root, maintain.Governance, 2)

	formalPaths := []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"}
	formalBefore := cognitionOptimizationFormalPreimages(t, root, formalPaths)
	checkpointPath := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	checkpointBefore := readOptimizationTestFile(t, checkpointPath)
	replacement := refinedOptimizationEntry(t, maintain.Candidates[0].ExistingEntry)
	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, func(position int, candidate optimizationCandidatePayload) string {
		if position == 0 {
			return replacement
		}
		return candidate.ExistingEntry
	}))

	assertOptimizationProgress(t, update.Optimization, "complete", 2, 2, 1, 1, 0, false)
	if update.Status != autoStatusApplied || !update.Aligned || update.Applied != 1 || !update.FormalWritesStarted {
		t.Fatalf("managed mixed batch did not report its one formal replacement: %#v", update)
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, "aoci.txt")); !reflect.DeepEqual(actual, formalBefore["aoci.txt"]) {
		t.Fatal("managed mixed batch rewrote the Root manifest")
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, "aoci.meta.txt")); !reflect.DeepEqual(actual, formalBefore["aoci.meta.txt"]) {
		t.Fatal("managed mixed batch rewrote the Meta Volume")
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, "aoci.code.txt")); reflect.DeepEqual(actual, formalBefore["aoci.code.txt"]) || !strings.Contains(string(actual), replacement) {
		t.Fatal("managed mixed batch did not write the complete replacement to the Code Volume")
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, ".aoci", "baseline.json")); reflect.DeepEqual(actual, formalBefore[".aoci/baseline.json"]) {
		t.Fatal("managed mixed batch changed the Index without advancing its Baseline")
	}
	if checkpointAfter := readOptimizationTestFile(t, checkpointPath); reflect.DeepEqual(checkpointAfter, checkpointBefore) {
		t.Fatal("managed mixed batch did not advance its checkpoint after governed Apply")
	}
	assertOptimizationCheckpointCounts(t, root, 2, 1, 1, true)
	assertNoActiveOptimizationRecovery(t, root)
	assertManagedOptimizationRoleBehavior(t, root, nil, 2)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationManagedScopeBudgetPressureReducesTokensWithoutGeneratingSemantics(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 3)
	const lowEntropyS = "重复重复重复重复重复重复重复重复重复重复重复重复重复重复重复重复"
	const highEntropyS = "Preserve the exact external value and ordering because downstream recovery compares the complete postimage before advancing the draft checkpoint"
	codeBefore := cognition.CodeVolumeMarker + "\n===Optimization fixtures" + filepath.ToSlash(filepath.Join(root, "optimized")) + "/===\n" +
		"0000.go[CD3S]: F:represent the low-importance budget-pressure fixture | R:- | A:- | S:" + lowEntropyS + "\n" +
		"0001.go[CD7S]: F:represent the recovery-sensitive budget-pressure fixture | R:- | A:- | S:" + highEntropyS + "\n" +
		"0002.go[CD9S]: F:represent the high-importance fixture without hidden constraints | R:- | A:- | S:-\n"
	writeVolumeTestFile(t, root, "aoci.code.txt", codeBefore)
	refreshManagedCognitionOptimizationBaseline(t, root, 3)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	beforeReport, err := cognitionbudget.Build(root, []byte(codeBefore), cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	if beforeReport.WholeIndexTokens >= beforeReport.MaxTokens || len(beforeReport.Violations) != 0 {
		t.Fatalf("budget-pressure fixture is not hard-valid: %#v", beforeReport)
	}

	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, maintain, 3, 3, 0)
	assertManagedOptimizationRoleBehavior(t, root, maintain.Governance, 3)
	if maintain.Candidates[0].ObjectRef != "code:optimized/0000.go" ||
		maintain.Candidates[0].SelectionReason != "c_band_target_overage" ||
		optimizationCostValue(t, maintain.Candidates[0], "s_tokens") <= optimizationCostValue(t, maintain.Candidates[0], "s_target_tokens") {
		t.Fatalf("low-C target-overage object was not prioritized: %#v", maintain.Candidates)
	}
	byRef := optimizationCandidatesByRef(maintain.Candidates)
	if byRef["code:optimized/0000.go"].ExistingEntry != strings.Split(codeBefore, "\n")[2] ||
		byRef["code:optimized/0001.go"].ExistingEntry != strings.Split(codeBefore, "\n")[3] ||
		byRef["code:optimized/0002.go"].ExistingEntry != strings.Split(codeBefore, "\n")[4] {
		t.Fatal("selector generated, truncated, or retagged existing Entry semantics")
	}
	if !strings.HasSuffix(byRef["code:optimized/0002.go"].ExistingEntry, " | S:-") {
		t.Fatal("high C automatically gained an S field")
	}

	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, func(_ int, candidate optimizationCandidatePayload) string {
		if candidate.ObjectRef != "code:optimized/0000.go" {
			return candidate.ExistingEntry
		}
		parsed, ok := index.ParseEntryLine(candidate.ExistingEntry, 1)
		if !ok {
			t.Fatalf("parse budget-pressure Entry: %q", candidate.ExistingEntry)
		}
		return fmt.Sprintf("%s[%s]: F:%s | R:%s | A:%s | S:-", parsed.Filename, parsed.TagsRaw, parsed.F, parsed.R, parsed.Api)
	}))
	assertOptimizationProgress(t, update.Optimization, "complete", 3, 3, 2, 1, 0, false)
	if update.Status != autoStatusApplied || !update.Aligned || update.Applied != 1 || !update.FormalWritesStarted {
		t.Fatalf("budget-pressure mixed batch did not reuse the formal Update transaction: %#v", update)
	}
	codeAfter := volumeFileText(t, root, "aoci.code.txt")
	afterReport, err := cognitionbudget.Build(root, []byte(codeAfter), cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	if afterReport.WholeIndexTokens >= beforeReport.WholeIndexTokens {
		t.Fatalf("model-authored budget-pressure update did not reduce Whole-Index tokens: before=%d after=%d", beforeReport.WholeIndexTokens, afterReport.WholeIndexTokens)
	}
	if strings.Contains(codeAfter, lowEntropyS) || !strings.Contains(codeAfter, "0000.go[CD3S]: F:represent the low-importance budget-pressure fixture | R:- | A:- | S:-") {
		t.Fatal("model-authored replacement did not remove only the repetitive low-entropy S")
	}
	if !strings.Contains(codeAfter, "0001.go[CD7S]: F:represent the recovery-sensitive budget-pressure fixture | R:- | A:- | S:"+highEntropyS) ||
		!strings.Contains(codeAfter, "0002.go[CD9S]: F:represent the high-importance fixture without hidden constraints | R:- | A:- | S:-") {
		t.Fatal("no_change review failed to preserve high-entropy S or high-C S omission")
	}
	assertOptimizationCheckpointCounts(t, root, 3, 2, 1, true)
	assertNoActiveOptimizationRecovery(t, root)
	assertManagedOptimizationRoleBehavior(t, root, nil, 3)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationSourceDriftBlocksWithoutAdvancingCheckpoint(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	checkpoint := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	checkpointBefore := readOptimizationTestFile(t, checkpoint)
	indexBefore := volumeFileText(t, root, "aoci.code.txt")

	driftPath := filepath.Join(root, filepath.FromSlash(maintain.Candidates[0].Path))
	sourceBefore := readOptimizationTestFile(t, driftPath)
	if err := os.WriteFile(driftPath, append(append([]byte{}, sourceBefore...), []byte("// source drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain, nil))
	if drift.Status != autoStatusStopped || drift.Applied != 0 || drift.FormalWritesStarted {
		t.Fatalf("source drift did not fail before formal writes: %#v", drift)
	}
	if after := readOptimizationTestFile(t, checkpoint); !reflect.DeepEqual(after, checkpointBefore) {
		t.Fatal("failed source-drift update advanced the optimization checkpoint")
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("failed source-drift update changed the Code Volume")
	}

	if err := os.WriteFile(driftPath, sourceBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	resumed := callCognitionOptimizationMaintain(t, session, nil)
	if !reflect.DeepEqual(optimizationCandidateRefs(resumed.Candidates), optimizationCandidateRefs(maintain.Candidates)) ||
		resumed.Optimization.CurrentBatchID != maintain.Optimization.CurrentBatchID {
		t.Fatalf("restored source did not resume the unchanged current batch: before=%#v after=%#v", maintain.Optimization, resumed.Optimization)
	}
}

func TestCognitionOptimizationRebindsStaleCurrentBatchAfterOrdinaryRealignment(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	initial := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	target := initial.Candidates[0]

	sourcePath := filepath.Join(root, filepath.FromSlash(target.Path))
	source := readOptimizationTestFile(t, sourcePath)
	if err := os.WriteFile(sourcePath, append(source, []byte("// maintained source change\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(root, "aoci.code.txt")
	code := string(readOptimizationTestFile(t, codePath))
	maintainedEntry := refinedOptimizationEntry(t, target.ExistingEntry)
	code = strings.Replace(code, target.ExistingEntry, maintainedEntry, 1)
	if err := os.WriteFile(codePath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshVolumeBaseline(t, root)

	rebound := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, rebound, 3, 3, 0)
	if rebound.CodePlan.BatchID == initial.CodePlan.BatchID || rebound.Optimization.OptimizationID != initial.Optimization.OptimizationID {
		t.Fatalf("ordinary realignment did not safely rebind only the current batch: initial=%#v rebound=%#v", initial.Optimization, rebound.Optimization)
	}
	completed := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(rebound, nil))
	assertOptimizationProgress(t, completed.Optimization, "complete", 3, 3, 3, 0, 0, false)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationCheckpointAdvanceFailureResumesExistingTransaction(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(2))
	arguments := optimizationUpdateArguments(maintain, func(position int, candidate optimizationCandidatePayload) string {
		if position == 0 {
			return refinedOptimizationEntry(t, candidate.ExistingEntry)
		}
		return candidate.ExistingEntry
	})
	checkpoint := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	checkpointBefore := readOptimizationTestFile(t, checkpoint)
	formalPaths := []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"}
	formalBefore := cognitionOptimizationFormalPreimages(t, root, formalPaths)

	originalAdvance := advanceCognitionOptimizationCheckpoint
	failOnce := true
	advanceCognitionOptimizationCheckpoint = func(root, expectedSHA256 string, input cognitionoptimization.AdvanceInput) (cognitionoptimization.StoredCheckpoint, error) {
		if failOnce {
			failOnce = false
			return cognitionoptimization.StoredCheckpoint{}, errors.New("injected checkpoint CAS failure")
		}
		return originalAdvance(root, expectedSHA256, input)
	}
	defer func() { advanceCognitionOptimizationCheckpoint = originalAdvance }()

	first := callCognitionOptimizationUpdate(t, session, arguments)
	if first.Status != autoStatusStopped || first.Aligned || first.Optimization == nil ||
		first.Optimization.State != "checkpoint_recovery_required" || first.Applied != 1 || !first.FormalWritesStarted {
		t.Fatalf("checkpoint failure did not preserve an explicit retry boundary: %#v", first)
	}
	if after := readOptimizationTestFile(t, checkpoint); !reflect.DeepEqual(after, checkpointBefore) {
		t.Fatal("failed checkpoint CAS advanced draft progress")
	}
	assertOptimizationCheckpointCounts(t, root, 0, 0, 0, false)
	if actual := readOptimizationTestFile(t, filepath.Join(root, "aoci.code.txt")); reflect.DeepEqual(actual, formalBefore["aoci.code.txt"]) {
		t.Fatal("checkpoint failure was reported as a formal write without a durable Code postimage")
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, ".aoci", "baseline.json")); reflect.DeepEqual(actual, formalBefore[".aoci/baseline.json"]) {
		t.Fatal("checkpoint failure was reported after Apply without a durable Baseline postimage")
	}
	formalAfterFailure := cognitionOptimizationFormalPreimages(t, root, formalPaths)

	retry := callCognitionOptimizationUpdate(t, session, arguments)
	assertOptimizationProgress(t, retry.Optimization, "complete", 2, 2, 1, 1, 0, false)
	if retry.Status != autoStatusApplied || !retry.Aligned || retry.Applied != 0 || retry.FormalWritesStarted {
		t.Fatalf("same-batch postimage retry was not idempotent: %#v", retry)
	}
	assertCognitionOptimizationFormalPreimages(t, root, formalAfterFailure)
	assertOptimizationCheckpointCounts(t, root, 2, 1, 1, true)
	assertNoActiveOptimizationRecovery(t, root)
	assertManagedOptimizationRoleBehavior(t, root, nil, 2)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationBaselineFailureUsesExistingRecoveryBeforeCheckpoint(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(2))
	arguments := optimizationUpdateArguments(maintain, func(position int, candidate optimizationCandidatePayload) string {
		if position == 0 {
			return refinedOptimizationEntry(t, candidate.ExistingEntry)
		}
		return candidate.ExistingEntry
	})
	codeBefore := readOptimizationTestFile(t, filepath.Join(root, "aoci.code.txt"))
	baselineBefore := readOptimizationTestFile(t, filepath.Join(root, ".aoci", "baseline.json"))

	originalSave := saveAtomicBaseline
	failOnce := true
	saveAtomicBaseline = func(root string, value *baseline.Baseline) error {
		if failOnce {
			failOnce = false
			return errors.New("injected optimization Baseline failure")
		}
		return originalSave(root, value)
	}
	t.Cleanup(func() { saveAtomicBaseline = originalSave })
	first := callCognitionOptimizationUpdate(t, session, arguments)
	saveAtomicBaseline = originalSave
	if first.Status != autoStatusStopped || first.Aligned || first.Applied != 0 || !first.FormalWritesStarted {
		t.Fatalf("post-Index Baseline failure did not expose the formal recovery boundary: %#v", first)
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, "aoci.code.txt")); reflect.DeepEqual(actual, codeBefore) {
		t.Fatal("injected Baseline failure did not leave the proven Code postimage")
	}
	if actual := readOptimizationTestFile(t, filepath.Join(root, ".aoci", "baseline.json")); !reflect.DeepEqual(actual, baselineBefore) {
		t.Fatal("failed Baseline save changed durable Baseline bytes")
	}
	assertOptimizationCheckpointCounts(t, root, 0, 0, 0, false)
	assertActiveOptimizationRecovery(t, root)

	retry := callCognitionOptimizationUpdate(t, session, arguments)
	assertOptimizationProgress(t, retry.Optimization, "complete", 2, 2, 1, 1, 0, false)
	if retry.Status != autoStatusApplied || !retry.Aligned || retry.Applied != 0 || !retry.FormalWritesStarted {
		t.Fatalf("existing Recovery did not complete Baseline before checkpoint advance: %#v", retry)
	}
	assertOptimizationCheckpointCounts(t, root, 2, 1, 1, true)
	assertNoActiveOptimizationRecovery(t, root)
	assertManagedOptimizationRoleBehavior(t, root, nil, 2)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

func TestCognitionOptimizationActiveRunRejectsSelectorReplacement(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	_ = callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	checkpoint := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	before := readOptimizationTestFile(t, checkpoint)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_maintain", Arguments: map[string]any{
		"intent": maintainIntentCognitionOptimization, "scope": cognition.ScopeCode,
		"object_refs": []string{"code:optimized/0000.go"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(resText(t, result), "only_allowed_on_initial_request") {
		t.Fatalf("active optimization accepted a replacement selector:\n%s", resText(t, result))
	}
	if after := readOptimizationTestFile(t, checkpoint); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected selector replacement changed the active checkpoint")
	}
}

func TestCognitionOptimizationCorruptCheckpointFailsClosed(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	checkpoint := filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))
	indexBefore := volumeFileText(t, root, "aoci.code.txt")
	if err := os.WriteFile(checkpoint, []byte("{not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_maintain", Arguments: map[string]any{
			"intent": maintainIntentCognitionOptimization,
			"scope":  cognition.ScopeCode,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if !result.IsError || !strings.Contains(strings.ToLower(text), "checkpoint") {
		t.Fatalf("corrupt checkpoint did not fail closed: is_error=%v\n%s", result.IsError, text)
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("corrupt checkpoint handling changed the Code Volume")
	}
	update, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_update_entry", Arguments: optimizationUpdateArguments(maintain, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	updateText := resText(t, update)
	if !strings.Contains(updateText, "cognition_optimization_checkpoint_invalid") ||
		!strings.Contains(updateText, `"formal_writes_started":false`) {
		t.Fatalf("corrupt checkpoint allowed Optimization Update to fall through:\n%s", updateText)
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("corrupt checkpoint Update changed the Code Volume")
	}
}

func TestCognitionOptimizationIntentBoundariesFailClosedWithoutFormalWrites(t *testing.T) {
	tests := []struct {
		name      string
		buildRoot func(*testing.T) string
		arguments map[string]any
		formal    []string
	}{
		{
			name:      "legacy_layout",
			buildRoot: buildRepo,
			arguments: map[string]any{"intent": maintainIntentCognitionOptimization},
			formal:    []string{".aoci/index.txt", ".aoci/baseline.json"},
		},
		{
			name:      "database_scope",
			buildRoot: func(t *testing.T) string { return buildCognitionOptimizationRepo(t, 3) },
			arguments: map[string]any{"intent": maintainIntentCognitionOptimization, "scope": cognition.ScopeDatabase},
			formal:    []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"},
		},
		{
			name:      "all_scope",
			buildRoot: func(t *testing.T) string { return buildCognitionOptimizationRepo(t, 3) },
			arguments: map[string]any{"intent": maintainIntentCognitionOptimization, "scope": cognition.ScopeAll},
			formal:    []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"},
		},
		{
			name:      "unknown_intent",
			buildRoot: func(t *testing.T) string { return buildCognitionOptimizationRepo(t, 3) },
			arguments: map[string]any{"intent": "semantic_refresh"},
			formal:    []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"},
		},
		{
			name:      "object_refs_without_intent",
			buildRoot: func(t *testing.T) string { return buildCognitionOptimizationRepo(t, 3) },
			arguments: map[string]any{"object_refs": []string{"code:optimized/0000.go"}},
			formal:    []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := test.buildRoot(t)
			before := cognitionOptimizationFormalPreimages(t, root, test.formal)
			result, err := connectMCPClient(t, root).CallTool(context.Background(), &mcp.CallToolParams{
				Name: "aoci_maintain", Arguments: test.arguments,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("out-of-bound optimization request was accepted:\n%s", resText(t, result))
			}
			assertCognitionOptimizationFormalPreimages(t, root, before)
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath))); !os.IsNotExist(err) {
				t.Fatalf("rejected request created an optimization checkpoint: %v", err)
			}
		})
	}
}

func buildCognitionOptimizationRepo(t *testing.T, count int) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "optimized")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(cognition.CodeVolumeMarker + "\n===Optimization fixtures" + filepath.ToSlash(directory) + "/===\n")
	for position := 0; position < count; position++ {
		name := fmt.Sprintf("%04d.go", position)
		writeVolumeTestFile(t, root, filepath.ToSlash(filepath.Join("optimized", name)),
			fmt.Sprintf("package optimized\n\nconst Value%d = %d\n", position, position))
		importance := 5
		constraint := "Preserve this fixture's deterministic externally visible behavior"
		switch {
		case count > 201 && position == count-2:
			importance = 9
		case count > 201 && position == count-1:
			importance = 1
			// Multibyte evidence stays within the C1 50-rune authoring quota
			// while exceeding its 20-token S target under the shared byte/3
			// estimator. That makes this lexicographically late object a
			// deterministic priority candidate without making the fixture
			// formally invalid.
			constraint = strings.Repeat("约", 35)
		}
		fmt.Fprintf(&body, "%s[CD%dS]: F:represent optimization fixture %04d | R:- | A:- | S:%s\n",
			name, importance, position, constraint)
	}
	writeVolumeTestFile(t, root, "aoci.code.txt", body.String())
	refreshVolumeBaseline(t, root)
	return root
}

func buildManagedScopeCognitionOptimizationRepo(t *testing.T, count int) string {
	t.Helper()
	root := buildCognitionOptimizationRepo(t, count)
	refreshManagedCognitionOptimizationBaseline(t, root, count)
	return root
}

func refreshManagedCognitionOptimizationBaseline(t *testing.T, root string, count int) {
	t.Helper()
	const observePath = "optimized/optimization_test.go"
	const excludePath = "optimized/testdata/excluded-source.txt"
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(observePath))); os.IsNotExist(err) {
		writeVolumeTestFile(t, root, observePath, "package optimized\n\n// Observe-only optimization evidence.\n")
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(excludePath))); os.IsNotExist(err) {
		writeVolumeTestFile(t, root, excludePath, "excluded optimization evidence\n")
	} else if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	policy, err = managedscope.Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &policy
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	for rel, role := range map[string]string{
		"optimized/0000.go": machinecontract.ScopeRoleIndex,
		observePath:         machinecontract.ScopeRoleObserve,
		excludePath:         machinecontract.ScopeRoleExclude,
	} {
		if actual := managedOptimizationEvaluationRole(evaluation, rel); actual != role {
			t.Fatalf("production-like Managed Scope role for %s=%q, want %q", rel, actual, role)
		}
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	// Formal Volumes are outside business Safe Inventory, but remain explicit
	// Index-role fingerprints in the production Baseline contract.
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		fingerprint.Role = machinecontract.ScopeRoleIndex
		snapshot[rel] = fingerprint
	}
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value := baseline.NewBaseline(snapshot)
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: policy.ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	assertManagedOptimizationRoleBehavior(t, root, nil, count)
}

func managedOptimizationEvaluationRole(evaluation *managedscope.Evaluation, rel string) string {
	for _, group := range [][]managedscope.PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
		for _, item := range group {
			if item.Path == rel {
				return item.Role
			}
		}
	}
	return ""
}

func assertManagedOptimizationRoleBehavior(t *testing.T, root string, supplied *volumegovernance.Facts, indexedTargets int) {
	t.Helper()
	facts := supplied
	if facts == nil {
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		set, err := cognition.Load(root, cfg.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		facts, err = volumegovernance.Assess(root, cfg, set)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !facts.GovernanceAligned || facts.Result != volumegovernance.ResultAligned ||
		facts.CodeSourceCount != indexedTargets || facts.CodeEntryCount != indexedTargets ||
		facts.ManagedScope.IndexCount < indexedTargets || facts.ManagedScope.ObserveCount != 1 || facts.ManagedScope.ExcludeCount < 1 ||
		len(facts.CodeDrift.Missing) != 0 || len(facts.CodeDrift.Stale) != 0 ||
		len(facts.CodeDrift.Orphan) != 0 || len(facts.CodeDrift.Unbaselined) != 0 ||
		len(facts.CodeDrift.ObservedNew) != 0 || len(facts.CodeDrift.ObservedChanged) != 0 || len(facts.CodeDrift.ObservedRemoved) != 0 {
		t.Fatalf("production-like Managed Scope is not aligned: %#v", facts)
	}
	code := volumeFileText(t, root, "aoci.code.txt")
	if !strings.Contains(code, "0000.go[") || strings.Contains(code, "optimization_test.go[") || strings.Contains(code, "excluded-source.txt[") {
		t.Fatalf("Index/Observe/Exclude roles leaked into the Code Volume: %s", code)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state == nil {
		t.Fatalf("managed Baseline unavailable: exists=%v err=%v", exists, err)
	}
	if baseline.EffectiveRole(state.Files["optimized/0000.go"]) != machinecontract.ScopeRoleIndex ||
		baseline.EffectiveRole(state.Files["optimized/optimization_test.go"]) != machinecontract.ScopeRoleObserve {
		t.Fatalf("managed Baseline lost Index/Observe roles: %#v", state.Files)
	}
	if _, exists := state.Files["optimized/testdata/excluded-source.txt"]; exists {
		t.Fatal("Exclude object entered the formal Baseline")
	}
}

func assertNoActiveOptimizationRecovery(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "entries-*.json"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("optimization left an active retained transaction: matches=%v err=%v", matches, err)
	}
}

func assertActiveOptimizationRecovery(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "entries-*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("optimization did not retain exactly one active transaction: matches=%v err=%v", matches, err)
	}
}

func assertOptimizationCheckpointCounts(t *testing.T, root string, reviewed, noChange, replaced int, completed bool) {
	t.Helper()
	stored, err := cognitionoptimization.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := stored.Checkpoint
	if checkpoint.ReviewedCount != reviewed || checkpoint.NoChangeCount != noChange ||
		checkpoint.ReplacedCount != replaced || checkpoint.Completed != completed {
		t.Fatalf("unexpected durable optimization checkpoint: %#v", checkpoint)
	}
}

func cognitionOptimizationObjectRefs(count int) []string {
	refs := make([]string, 0, count)
	for position := 0; position < count; position++ {
		refs = append(refs, fmt.Sprintf("code:optimized/%04d.go", position))
	}
	return refs
}

func reversedStrings(values []string) []string {
	result := append([]string{}, values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func callCognitionOptimizationMaintain(t *testing.T, session *mcp.ClientSession, objectRefs []string) optimizationMaintainPayload {
	t.Helper()
	arguments := map[string]any{"intent": maintainIntentCognitionOptimization, "scope": cognition.ScopeCode}
	if objectRefs != nil {
		arguments["object_refs"] = objectRefs
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_maintain", Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if result.IsError {
		t.Fatalf("optimization Maintain failed:\n%s", text)
	}
	var payload optimizationMaintainPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode optimization Maintain: %v\n%s", err, text)
	}
	return payload
}

func callCognitionOptimizationUpdate(t *testing.T, session *mcp.ClientSession, arguments map[string]any) optimizationUpdatePayload {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_update_entry", Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if result.IsError {
		t.Fatalf("optimization Update returned an MCP error:\n%s", text)
	}
	var payload optimizationUpdatePayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode optimization Update: %v\n%s", err, text)
	}
	return payload
}

func optimizationUpdateArguments(maintain optimizationMaintainPayload, author func(int, optimizationCandidatePayload) string) map[string]any {
	entries := make([]map[string]any, 0, len(maintain.Candidates))
	for position, candidate := range maintain.Candidates {
		newEntry := candidate.ExistingEntry
		if author != nil {
			newEntry = author(position, candidate)
		}
		entries = append(entries, map[string]any{
			"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"candidate_id": candidate.CandidateID, "new_entry": newEntry,
		})
	}
	return map[string]any{"code_batch_id": maintain.CodePlan.BatchID, "entries": entries}
}

func assertOptimizationReviewBatch(t *testing.T, payload optimizationMaintainPayload, total, included, remaining int) {
	t.Helper()
	if payload.Status != autoStatusApplied || !payload.Aligned || payload.CodePlan == nil || payload.Governance == nil ||
		!payload.Governance.GovernanceAligned || len(payload.Candidates) != included || payload.CodePlan.Included != included ||
		payload.CodePlan.Included > 200 || payload.Batch.MaxEntries != 200 || payload.Optimization == nil {
		t.Fatalf("invalid aligned optimization batch: %#v", payload)
	}
	assertOptimizationProgress(t, payload.Optimization, "review_required", total,
		payload.Optimization.Reviewed, payload.Optimization.NoChange, payload.Optimization.Replaced, remaining, remaining > 0)
	if payload.Optimization.Included != included || payload.Optimization.CurrentBatchID != payload.CodePlan.BatchID {
		t.Fatalf("optimization and Code batch identities diverged: optimization=%#v plan=%#v", payload.Optimization, payload.CodePlan)
	}
}

func assertOptimizationProgress(t *testing.T, progress *optimizationProgressPayload, state string, total, reviewed, noChange, replaced, remaining int, continuation bool) {
	t.Helper()
	if progress == nil || progress.Version != "cognition-optimization/v1" || progress.State != state ||
		progress.OptimizationID == "" || progress.TotalTargets != total || progress.Reviewed != reviewed ||
		progress.NoChange != noChange || progress.Replaced != replaced || progress.Remaining != remaining ||
		progress.ContinuationRequired != continuation {
		t.Fatalf("unexpected optimization progress: %#v", progress)
	}
}

func assertOptimizationCostContract(t *testing.T, candidate optimizationCandidatePayload) {
	t.Helper()
	for _, field := range []string{
		"f_tokens", "r_tokens", "a_tokens", "s_tokens", "total_tokens",
		"r_target_tokens", "r_max_tokens", "s_target_tokens", "s_max_tokens",
	} {
		raw, ok := candidate.Cost[field]
		var value int
		if !ok || json.Unmarshal(raw, &value) != nil || value < 0 {
			t.Fatalf("candidate %s lacks valid cost.%s: %#v", candidate.ObjectRef, field, candidate.Cost)
		}
	}
	sum := sha256.Sum256([]byte(candidate.ExistingEntry))
	if candidate.ExistingEntrySHA256 != hex.EncodeToString(sum[:]) || candidate.Importance < 1 || candidate.Importance > 9 ||
		candidate.SelectionReason == "" || candidate.SourceSHA256 == "" || candidate.CandidateID == "" {
		t.Fatalf("candidate identity/priority metadata is incomplete: %#v", candidate)
	}
}

func optimizationCostValue(t *testing.T, candidate optimizationCandidatePayload, field string) int {
	t.Helper()
	raw, ok := candidate.Cost[field]
	var value int
	if !ok || json.Unmarshal(raw, &value) != nil {
		t.Fatalf("candidate %s lacks integer cost.%s: %#v", candidate.ObjectRef, field, candidate.Cost)
	}
	return value
}

func optimizationCandidatesByRef(candidates []optimizationCandidatePayload) map[string]optimizationCandidatePayload {
	result := make(map[string]optimizationCandidatePayload, len(candidates))
	for _, candidate := range candidates {
		result[candidate.ObjectRef] = candidate
	}
	return result
}

func optimizationCandidateRefs(candidates []optimizationCandidatePayload) []string {
	refs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		refs = append(refs, candidate.ObjectRef)
	}
	return refs
}

func optimizationContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func refinedOptimizationEntry(t *testing.T, current string) string {
	t.Helper()
	parsed, ok := index.ParseEntryLine(current, 1)
	if !ok {
		t.Fatalf("current optimization Entry is invalid: %q", current)
	}
	return fmt.Sprintf("%s[%s]: F:represent the semantically refined optimization fixture | R:%s | A:%s | S:%s",
		parsed.Filename, parsed.TagsRaw, parsed.R, parsed.Api, parsed.S)
}

func assertOrdinaryVolumeGovernanceAligned(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || len(facts.CodeDrift.Missing) != 0 || len(facts.CodeDrift.Stale) != 0 ||
		len(facts.CodeDrift.Unbaselined) != 0 || len(facts.CodeDrift.Orphan) != 0 {
		t.Fatalf("ordinary Verify facts are not aligned after optimization: facts=%#v err=%v", facts, err)
	}
	ordinary := maintainVolumeBatch(t, connectMCPClient(t, root))
	if ordinary.Status != autoStatusApplied || !ordinary.Aligned || len(ordinary.Candidates) != 0 {
		t.Fatalf("ordinary Maintain/Check/Guide alignment changed after optimization: %#v", ordinary)
	}
}

func readOptimizationTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cognitionOptimizationFormalPreimages(t *testing.T, root string, paths []string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		result[path] = readOptimizationTestFile(t, filepath.Join(root, filepath.FromSlash(path)))
	}
	return result
}

func assertCognitionOptimizationFormalPreimages(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	for path, expected := range before {
		actual := readOptimizationTestFile(t, filepath.Join(root, filepath.FromSlash(path)))
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("rejected optimization request changed formal asset %s", path)
		}
	}
}
