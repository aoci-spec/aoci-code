package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// The authoring contract (authoring_meta and instructions) is the same text on
// every round of one logical plan. One process sends it once per refresh
// generation; the next candidate-carrying response carries instructions_ref
// instead, include_instructions=true brings it back, and a generation advance
// (a completed context-compaction reload) sends it again without a flag.
func TestMaintainSendsTheAuthoringContractOncePerGeneration(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 60)
	session := connectMCPClient(t, root)
	call := func(arguments map[string]any) map[string]any {
		return decodeMaintainJSON(t, callVolumeTool(t, session, "aoci_maintain", arguments))
	}
	first := call(map[string]any{})
	if first["instructions"] == nil || first["authoring_meta"] == nil || first["instructions_ref"] != nil {
		t.Fatalf("the first response of a process carries the contract: instructions=%v meta=%v ref=%v",
			first["instructions"] != nil, first["authoring_meta"] != nil, first["instructions_ref"])
	}
	second := call(map[string]any{})
	ref, ok := second["instructions_ref"].(map[string]any)
	if !ok || second["instructions"] != nil || second["authoring_meta"] != nil {
		t.Fatalf("the second response carries the reference and not the contract: %v", second["instructions_ref"])
	}
	digest, _ := ref["sha256"].(string)
	if len(digest) != 64 || ref["recall_input"] != maintainRecallInput || int(ref["delivered_in_generation"].(float64)) != 0 {
		t.Fatalf("reference shape: %v", ref)
	}
	if second["status"] != first["status"] || second["result"] != first["result"] {
		t.Fatal("the reference changes no verdict")
	}
	third := call(map[string]any{"include_instructions": true})
	if third["instructions"] == nil || third["authoring_meta"] == nil || third["instructions_ref"] != nil {
		t.Fatal("include_instructions=true brings the contract back")
	}
	fourth := call(map[string]any{})
	if fourth["instructions_ref"] == nil || fourth["instructions"] != nil {
		t.Fatal("a recall does not change the rule for the next round")
	}
	// The contract and its reference name the same text.
	instructions := []string{}
	for _, value := range third["instructions"].([]any) {
		instructions = append(instructions, value.(string))
	}
	if authoringContractDigest(third["authoring_meta"].(string), instructions) != digest {
		t.Fatal("instructions_ref.sha256 is the digest of the contract the process sent")
	}
}

func TestAuthoringContractMemoFollowsTheRefreshGeneration(t *testing.T) {
	session := newCognitionRefreshSession()
	digest := authoringContractDigest("#meta", []string{"a", "b"})
	if onWire, generation := session.authoringContractOnWire(digest, false); !onWire || generation != 0 {
		t.Fatal("an unseen contract goes on the wire")
	}
	session.recordAuthoringContract(digest)
	if onWire, _ := session.authoringContractOnWire(digest, false); onWire {
		t.Fatal("a sent contract travels by reference")
	}
	if onWire, _ := session.authoringContractOnWire(digest, true); !onWire {
		t.Fatal("include_instructions overrides the memo")
	}
	if onWire, _ := session.authoringContractOnWire(authoringContractDigest("#meta", []string{"a", "c"}), false); !onWire {
		t.Fatal("a different text is a different contract")
	}
	session.mu.Lock()
	session.generation = 3
	session.mu.Unlock()
	if onWire, generation := session.authoringContractOnWire(digest, false); !onWire || generation != 3 {
		t.Fatal("a generation advance sends the contract again")
	}
	session.recordAuthoringContract(digest)
	if onWire, _ := session.authoringContractOnWire(digest, false); onWire {
		t.Fatal("once per generation")
	}
	var nilSession *cognitionRefreshSession
	if onWire, _ := nilSession.authoringContractOnWire(digest, false); !onWire {
		t.Fatal("without a session every response carries the contract")
	}
	nilSession.recordAuthoringContract(digest)
}

// The reservation the batch is cut under must cover what the response carries
// beside its candidates, measured on the response itself, and the candidate
// sizes it adds up must cover the candidates as marshaled. Otherwise a batch
// that fits the arithmetic overflows the budget on the wire. The fixture's
// files are aged past the cache's racy window, so after the first Maintain
// (whose profiler counted the targets) the second cuts with measured widths.
func TestMaintainReservationCoversTheMeasuredFixedPart(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 60)
	ageRepositoryFiles(t, root)
	session := connectMCPClient(t, root)
	callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	facts, state, err := volumegovernance.AssessWithSnapshot(root, loaded.cfg, loaded.set)
	if err != nil {
		t.Fatal(err)
	}
	all, lines := codeCandidatesForWork(root, loaded.set, volumegovernance.CodeAuthoringWorkFor(loaded.set, facts.CodeDrift), snapshotOfState(state))
	if len(lines) < len(all)/2 {
		t.Fatalf("after one Maintain the cache holds the line counts of the targets it profiled: %d of %d", len(lines), len(all))
	}
	for _, candidate := range all {
		if candidate.SourceSHA256 != state.Snapshot[candidate.Path].SHA256 {
			t.Fatalf("a candidate binds the digest the verdict's snapshot holds: %s", candidate.Path)
		}
	}
	budget := codeCandidateBudget(loaded.cfg, loaded.set, all, lines, false)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	response := decodeMaintainJSON(t, raw)
	if response["instructions_ref"] == nil {
		t.Fatal("the second round carries the reference, the shape this budget was computed for")
	}
	candidates, err := json.Marshal(response["candidates"])
	if err != nil {
		t.Fatal(err)
	}
	fixed := len(raw) - len(candidates)
	if reserved := budget.Total - budget.Bytes; fixed > reserved {
		t.Fatalf("the response carries %d bytes beside its candidates but the batch was cut as if %d", fixed, reserved)
	}
	included := int(response["code_plan"].(map[string]any)["included"].(float64))
	_, selected := codebatchSelectForTest(all, included)
	estimated := 0
	for _, candidate := range selected {
		estimated += budget.Size(candidate)
	}
	if estimated < len(candidates)-2 {
		t.Fatalf("the candidate sizes add up to %d but the candidates marshal to %d", estimated, len(candidates))
	}
	if len(raw) > budget.Total {
		t.Fatalf("response %d bytes over the budget %d", len(raw), budget.Total)
	}
}

// Deep paths cut the first round below the cap; the second round of the same
// process, which carries the reference instead of the contract, issues more
// of the same targets in the same order and still fits the budget.
func TestMaintainLaterRoundsCarryMoreCandidates(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	directory := filepath.Join("ruoyi-admin", "src", "main", "java", "com", "example", "web", "controller", "system", "monitor")
	for i := 0; i < 60; i++ {
		rel := filepath.Join(directory, fmt.Sprintf("SysOperationLogQueryController%03d.java", i))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(fmt.Sprintf("package monitor;\n\npublic class C%03d {}\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.LoadBase(root)
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
	session := connectMCPClient(t, root)
	paths := func(response map[string]any) []string {
		values := []string{}
		for _, candidate := range response["candidates"].([]any) {
			values = append(values, candidate.(map[string]any)["path"].(string))
		}
		return values
	}
	firstRaw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	first := decodeMaintainJSON(t, firstRaw)
	secondRaw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	second := decodeMaintainJSON(t, secondRaw)
	firstPaths, secondPaths := paths(first), paths(second)
	if len(firstPaths) >= machinecontract.CodeCognitionBatchEntriesDefault {
		t.Fatalf("the fixture must cut the first round below the cap: %d", len(firstPaths))
	}
	if len(secondPaths) <= len(firstPaths) {
		t.Fatalf("the reference round carries more candidates: first=%d second=%d", len(firstPaths), len(secondPaths))
	}
	for index, path := range firstPaths {
		if secondPaths[index] != path {
			t.Fatalf("the same targets in the same order: %v vs %v", firstPaths, secondPaths)
		}
	}
	if len(secondRaw) > machinecontract.MaintainTransportBudgetBytesDefault || len(firstRaw) > machinecontract.MaintainTransportBudgetBytesDefault {
		t.Fatalf("both rounds fit the budget: first=%d second=%d", len(firstRaw), len(secondRaw))
	}
	if second["code_plan"].(map[string]any)["batch_id"] == first["code_plan"].(map[string]any)["batch_id"] {
		t.Fatal("a larger batch is a different batch")
	}
}
