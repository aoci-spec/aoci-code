package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestMaintainBudgetCountsEncodedPathsAndContract(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	set := loaded.set
	// Valid Meta comments can contain characters that expand on the wire,
	// and so can the repository root and candidate identities.
	set.Meta.Raw = append(set.Meta.Raw, []byte("# "+strings.Repeat("<&", 128)+"\n")...)
	set.RepositoryRoot = strings.Repeat("&", 32)
	contract, err := buildAuthoringContract(loaded.cfg, set.Meta.Raw, []string{cognition.ScopeCode})
	if err != nil {
		t.Fatal(err)
	}
	contractJSON, err := json.Marshal(volumeMaintainResult{AuthoringMeta: contract.AuthoringMeta, Instructions: contract.Instructions})
	if err != nil {
		t.Fatal(err)
	}
	emptyJSON, err := json.Marshal(volumeMaintainResult{})
	if err != nil {
		t.Fatal(err)
	}
	if reserved := fixedOverheadBytes(loaded.cfg, set, true) - maintainBaseOverheadBytes; reserved < len(contractJSON)-len(emptyJSON)+len(set.RepositoryRoot)*6 {
		t.Fatalf("fixed part reserves %d bytes for the encoded paths and contract", reserved)
	}
	ref := "code:" + strings.Repeat("&", 128)
	reserved := sampleBytes([]codebatch.Candidate{{Target: codebatch.Target{ObjectRef: ref}}})
	if minimum := 4 * (len("code:") + 128*6); reserved < minimum {
		t.Fatalf("sample reserves %d bytes for four encoded references, need at least %d", reserved, minimum)
	}
}

func TestApplyFindingsFitTransportWithDeepUnissuedPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixture needs Linux paths over 3 KiB")
	}
	const shortFiles, longFiles, leadSize = 40, 35, 27
	root := buildManyFileVolumesRepo(t, shortFiles)
	activateEnforceBudget(t, root)
	directory := strings.Repeat(strings.Repeat("z", 218)+"/", 15)
	longPath := func(i int) string { return fmt.Sprintf("%sdeep%02d.go", directory, i) }
	// The short files are already in the Baseline. Every deep file therefore
	// contributes both an unbaselined and a missing finding.
	for i := range longFiles {
		writeVolumeTestFile(t, root, longPath(i), "package fixture\n")
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionBatchEntries(leadSize); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	budget := cfg.MaintainTransportBudget()
	session := connectMCPClient(t, root)
	maintain := maintainJSON(t, session, nil)
	paths := candidatePaths(maintain)
	if len(paths) != leadSize || len(siblingsOf(codePlanOf(t, maintain))) != 2 {
		t.Fatalf("the fixture must issue a %d-target lead and two siblings, got %d targets", leadSize, len(paths))
	}
	for _, path := range paths {
		if len(path) > 100 {
			t.Fatalf("the lead must contain short paths, got %d bytes", len(path))
		}
	}
	wire := applyMaintainBatchRaw(t, session, maintain)
	result := decodeMaintainJSON(t, wire)
	if result["status"] != autoStatusApplied || result["applied"] != float64(len(paths)) || result["aligned"] != false {
		t.Fatalf("the lead must apply and leave pending work: status=%v applied=%v aligned=%v",
			result["status"], result["applied"], result["aligned"])
	}
	if size := len(wire); size > budget {
		t.Fatalf("long-path Apply response is %d bytes, budget is %d (applied=%d, finding_count=%v)", size, budget, len(paths), result["finding_count"])
	}
	meta, _ := result["findings_truncated"].(map[string]any)
	kinds, _ := meta["by_kind"].(map[string]any)
	missing := shortFiles + longFiles - len(paths)
	total := missing + longFiles
	if result["finding_count"] != float64(total) || meta["total"] != float64(total) || meta["byte_limit"] != float64(budget) ||
		len(kinds) != 2 || kinds["missing"] != float64(missing) || kinds["unbaselined"] != float64(longFiles) {
		t.Fatalf("the cut must report all findings and both kinds: count=%v truncation=%v", result["finding_count"], meta)
	}
	findings, _ := result["findings"].([]any)
	if len(findings) == 0 || len(findings) >= int(meta["limit"].(float64)) {
		t.Fatalf("the byte budget should shorten the count-limited prefix: shown=%d", len(findings))
	}
	for i, finding := range findings {
		if finding != "unbaselined: "+longPath(i) {
			t.Fatalf("finding %d is outside the original prefix", i)
		}
	}
	next, err := json.Marshal("unbaselined: " + longPath(len(findings)))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire)+1+len(next) <= budget {
		t.Fatal("the prefix left enough budget for another finding")
	}
	t.Logf("applied=%d findings=%d/%d bytes=%d/%d", len(paths), len(findings), total, len(wire), budget)
}

func TestParallelLeadCutReservesRecalledContract(t *testing.T) {
	paths := make([]codebatch.Candidate, 24)
	for i := range paths {
		paths[i].ObjectRef = fmt.Sprintf("code:file%02d.go", i)
	}
	full := codebatch.Budget{Bytes: 6000, Size: func(codebatch.Candidate) int { return 1000 }}
	ref := full
	ref.Bytes = 12000
	cuts := cutParallelSiblings(paths, len(paths), 3, ref, full)
	if len(cuts) != 3 || len(cuts[0]) < 2 || len(cuts[0]) != len(cuts[1]) {
		t.Fatal("a reference-only lead batch must leave room for the same full contract as its siblings")
	}
}

func TestMaintainTransportBudgetForVeryLongPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixture needs Linux paths over 1 KiB")
	}
	for _, fixture := range []struct {
		name      string
		character string
		oversize  bool
	}{
		{name: "long_path", character: "d"},
		{name: "oversize_candidate", character: "&", oversize: true},
	} {
		for _, parallel := range []bool{false, true} {
			mode := "single"
			if parallel {
				mode = "default_parallel"
			}
			t.Run(fixture.name+"/"+mode, func(t *testing.T) {
				root := buildSingleCodeWriteRepo(t, false)
				if !parallel {
					pinSingleBatch(t, root)
				}
				// Each component is below NAME_MAX; the full path is over
				// 1.6 KiB. The second fixture expands further in JSON.
				directory := strings.Repeat(strings.Repeat(fixture.character, 227)+"/", 8)
				const targets = 8
				for i := range targets {
					writeVolumeTestFile(t, root, fmt.Sprintf("%sentry%02d.go", directory, i), "package fixture\n")
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
				leader := connectMCPClient(t, root)
				budget := cfg.MaintainTransportBudget()
				authored := 0
				for round := 1; authored < targets && round <= targets; round++ {
					first := checkLongPathMaintain(t, leader, nil, budget, fixture.oversize, fmt.Sprintf("round %d", round))
					if round == 1 && parallel && len(first.CodePlan.Siblings) != 2 {
						t.Fatalf("default first round issued %d siblings, want 2", len(first.CodePlan.Siblings))
					}
					batches := []*volumeMaintainResult{first}
					sessions := []*mcp.ClientSession{leader}
					for _, sibling := range first.CodePlan.Siblings {
						worker := connectMCPClient(t, root)
						args := map[string]any{"batch_id": sibling.BatchID}
						batches = append(batches, checkLongPathMaintain(t, worker, args, budget, fixture.oversize, fmt.Sprintf("round %d sibling", round)))
						sessions = append(sessions, worker)
					}
					for i, batch := range batches {
						applied := applyVolumeBatch(t, sessions[i], codeBatchArguments(wireCodePlan(*batch)))
						if applied.Status != autoStatusApplied || applied.Applied != len(batch.Candidates) {
							t.Fatalf("round %d: Apply did not write its complete batch: status=%s, applied=%d", round, applied.Status, applied.Applied)
						}
						authored += applied.Applied
						if applied.Aligned != (authored == targets) {
							t.Fatalf("round %d: aligned=%v after %d/%d candidates", round, applied.Aligned, authored, targets)
						}
					}
				}
				if authored != targets {
					t.Fatalf("authored %d of %d candidates", authored, targets)
				}
			})
		}
	}
}

func TestParallelBudgetDeliversOrphanWithFirstBatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixture needs Linux paths over 1 KiB")
	}
	root := buildManyFileVolumesRepo(t, 12)
	buildToAligned(t, connectMCPClient(t, root))
	const orphan = "pkg/mod001/file011.go"
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(orphan))); err != nil {
		t.Fatal(err)
	}
	directory := strings.Repeat(strings.Repeat("&", 227)+"/", 8)
	for i := range 3 {
		writeVolumeTestFile(t, root, fmt.Sprintf("%sitem%02d.go", directory, i), "package fixture\n")
	}
	leader := connectMCPClient(t, root)
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	first := checkLongPathMaintain(t, leader, nil, cfg.MaintainTransportBudget(), true, "first delivery")
	if len(first.OrphanRemovals) != 1 || first.OrphanRemovals[0] != "code:"+orphan || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("first delivery must carry the orphan and list both siblings, got orphans=%v siblings=%d", first.OrphanRemovals, len(first.CodePlan.Siblings))
	}
	args := codeBatchArguments(wireCodePlan(*first))
	args["removals"] = []map[string]string{{"path": orphan}}
	if applied := applyVolumeBatch(t, leader, args); applied.Status != autoStatusApplied || applied.Aligned {
		t.Fatalf("lead Apply must remove the orphan and leave sibling work, got status=%s aligned=%v", applied.Status, applied.Aligned)
	}
	for i, sibling := range first.CodePlan.Siblings {
		worker := connectMCPClient(t, root)
		batch := checkLongPathMaintain(t, worker, map[string]any{"batch_id": sibling.BatchID}, cfg.MaintainTransportBudget(), true, "sibling")
		if len(batch.OrphanRemovals) != 0 {
			t.Fatal("later delivery repeated the orphan")
		}
		applied := applyVolumeBatch(t, worker, codeBatchArguments(wireCodePlan(*batch)))
		if applied.Status != autoStatusApplied || applied.Aligned != (i == len(first.CodePlan.Siblings)-1) {
			t.Fatalf("Apply %d status=%s aligned=%v", i, applied.Status, applied.Aligned)
		}
	}
}

func checkLongPathMaintain(t *testing.T, session *mcp.ClientSession, args map[string]any, budget int, expectOversize bool, label string) *volumeMaintainResult {
	t.Helper()
	raw := callVolumeTool(t, session, "aoci_maintain", args)
	var result volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("%s: decode Maintain result: %v, response=%q", label, err, raw)
	}
	if result.CodePlan == nil || len(result.Candidates) == 0 {
		t.Fatalf("%s: no current Code batch, status=%s result=%s next=%s", label, result.Status, result.Result, result.NextAction)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	var plan map[string]json.RawMessage
	if err := json.Unmarshal(fields["code_plan"], &plan); err != nil {
		t.Fatal(err)
	}
	path, err := json.Marshal(result.Candidates[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d/%d bytes, %d candidates, candidate JSON=%d, governance=%d, sets=%d, contract/ref=%d, sibling listing=%d, first path occurrences=%d",
		label, len(raw), budget, len(result.Candidates), len(fields["candidates"]), len(fields["governance"]), len(fields["sets"]),
		len(fields["instructions"])+len(fields["authoring_meta"])+len(fields["instructions_ref"]), len(plan["siblings"]),
		strings.Count(raw, string(path[1:len(path)-1])))
	if len(raw) > budget {
		if !expectOversize || !result.Batch.TransportBudgetExceeded || len(result.Candidates) != 1 {
			t.Errorf("%s: %d bytes exceed %d with %d candidates and transport_budget_exceeded=%v",
				label, len(raw), budget, len(result.Candidates), result.Batch.TransportBudgetExceeded)
		}
		if result.Batch.TransportBudgetExceeded && (result.Batch.TransportBudgetBytes != budget || result.Governance.ListTruncation == nil ||
			result.Governance.ListTruncation.Limit != 0 || len(result.Sets.Review) != 0 || len(result.Sets.Write) != 0) {
			t.Errorf("%s: over-budget response must report its budget and exhaust advisory sample reduction", label)
		}
	} else if result.Batch.TransportBudgetExceeded {
		t.Errorf("%s: %d bytes are within %d but marked over budget", label, len(raw), budget)
	} else if expectOversize {
		t.Errorf("%s: expected the escaped candidate and required fields to exceed %d, got %d", label, budget, len(raw))
	}
	if truncated := result.Governance.ListTruncation; truncated != nil {
		if total := truncated.Totals["code_drift.missing"]; total > 0 {
			if sample := len(result.Governance.CodeDrift.Missing); sample >= total || total != result.Batch.TotalTargets {
				t.Errorf("%s: incorrect missing total or sample: %d of %d, total targets %d", label, sample, total, result.Batch.TotalTargets)
			}
		}
	}
	return &result
}
