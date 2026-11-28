// Parallel current batches (L4): the conformance the Volumes contract names.
package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// parallelVolumesRepo is the many-file write fixture under N parallel batches.
func parallelVolumesRepo(t *testing.T, files, parallel int) string {
	t.Helper()
	root := buildManyFileVolumesRepo(t, files)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionParallelBatches(parallel); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func maintainJSON(t *testing.T, session *mcp.ClientSession, arguments map[string]any) map[string]any {
	t.Helper()
	return decodeMaintainJSON(t, callVolumeTool(t, session, "aoci_maintain", arguments))
}

func candidatePaths(response map[string]any) []string {
	paths := []string{}
	candidates, _ := response["candidates"].([]any)
	for _, candidate := range candidates {
		paths = append(paths, candidate.(map[string]any)["path"].(string))
	}
	return paths
}

func codePlanOf(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	plan, ok := response["code_plan"].(map[string]any)
	if !ok {
		t.Fatalf("response carries no code_plan: %v", response["status"])
	}
	return plan
}

func siblingsOf(plan map[string]any) []map[string]any {
	siblings := []map[string]any{}
	values, _ := plan["siblings"].([]any)
	for _, value := range values {
		siblings = append(siblings, value.(map[string]any))
	}
	return siblings
}

// siblingTargets reads a listed sibling's targets from its receipt: the
// listing names the batch and its size, the paths stay off the wire.
func siblingTargets(t *testing.T, root string, sibling map[string]any) []string {
	t.Helper()
	receipt, err := codebatch.LoadReceipt(root, sibling["batch_id"].(string))
	if err != nil {
		t.Fatalf("a listed sibling has no receipt: %v", err)
	}
	if _, onWire := sibling["targets"]; onWire {
		t.Fatal("a sibling listing must not carry its target paths")
	}
	targets := []string{}
	for _, target := range receipt.Targets {
		targets = append(targets, target.Path)
	}
	return targets
}

func fixtureEntryLine(path string) string {
	base := filepath.Base(path)
	return fmt.Sprintf("%s[CD5T]: F:Provides the fixture unit %s | R:- | A:- | S:-", base, strings.TrimSuffix(base, ".go"))
}

// applyMaintainBatch authors every candidate of the response and applies it
// under the response's code_plan.batch_id.
func applyMaintainBatch(t *testing.T, session *mcp.ClientSession, response map[string]any) map[string]any {
	t.Helper()
	plan := codePlanOf(t, response)
	entries := []map[string]any{}
	for _, candidate := range response["candidates"].([]any) {
		item := candidate.(map[string]any)
		entries = append(entries, map[string]any{"path": item["path"], "source_sha256": item["source_sha256"],
			"candidate_id": item["candidate_id"], "new_entry": fixtureEntryLine(item["path"].(string))})
	}
	raw := callVolumeTool(t, session, "aoci_update_entry", map[string]any{"code_batch_id": plan["batch_id"], "entries": entries})
	return decodeMaintainJSON(t, raw)
}

func verifyAligned(t *testing.T, root string) (*volumegovernance.Facts, *cognition.Set) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	return facts, set
}

func codeEntrySet(set *cognition.Set) []string {
	lines := []string{}
	for _, object := range set.Volumes[cognition.ScopeCode].Objects {
		lines = append(lines, object.CanonicalRef+" "+object.CanonicalLine)
	}
	sort.Strings(lines)
	return lines
}

func TestParallelMaintainIssuesDisjointSiblingsUnderOnePlan(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	first := maintainJSON(t, session, map[string]any{})
	plan := codePlanOf(t, first)
	logical, _ := plan["logical_plan"].(string)
	siblings := siblingsOf(plan)
	if first["status"] != autoStatusRepairRequired || len(logical) != 64 || len(siblings) != 1 || siblings[0]["state"] != "issued" {
		t.Fatalf("N=2 issues one delivered batch and one issued sibling: status=%v logical=%q siblings=%v", first["status"], logical, siblings)
	}
	delivered := candidatePaths(first)
	ahead := siblingTargets(t, root, siblings[0])
	if len(delivered) == 0 || len(ahead) == 0 {
		t.Fatal("both siblings carry targets")
	}
	held := map[string]bool{}
	for _, path := range delivered {
		held[path] = true
	}
	for _, path := range ahead {
		if held[path] {
			t.Fatalf("sibling targets overlap on %s", path)
		}
	}
	if int(plan["included"].(float64)) != len(delivered) || int(siblings[0]["included"].(float64)) != len(ahead) {
		t.Fatalf("included counts: plan=%v sibling=%v", plan["included"], siblings[0]["included"])
	}
	total := int(plan["total_targets"].(float64))
	if int(plan["remaining"].(float64)) != total-len(delivered)-len(ahead) || plan["continuation_required"] != true {
		t.Fatalf("remaining counts the targets no sibling holds: %v", plan)
	}
	batch := first["authoring_batch"].(map[string]any)
	if int(batch["remaining"].(float64)) != total-len(delivered)-len(ahead) {
		t.Fatalf("authoring_batch.remaining follows the plan: %v", batch["remaining"])
	}
	// The same state keeps the same plan. The server that was handed a
	// sibling answers that one again while it is out, as a single batch
	// always did; another server's no-argument Maintain is handed the
	// sibling no one holds, and asked again it answers its own.
	second := maintainJSON(t, session, map[string]any{})
	if secondPlan := codePlanOf(t, second); secondPlan["logical_plan"] != logical || secondPlan["batch_id"] != plan["batch_id"] ||
		siblingsOf(secondPlan)[0]["state"] != "issued" {
		t.Fatalf("the same server asking twice is handed the same sibling, the other stays unfetched: %v", secondPlan)
	}
	other := connectMCPClient(t, root)
	otherPlan := codePlanOf(t, maintainJSON(t, other, map[string]any{}))
	if otherPlan["logical_plan"] != logical || otherPlan["batch_id"] != siblings[0]["batch_id"] ||
		siblingsOf(otherPlan)[0]["batch_id"] != plan["batch_id"] || siblingsOf(otherPlan)[0]["state"] != "delivered" {
		t.Fatalf("another server is handed the undelivered sibling under the same plan: %v", otherPlan)
	}
	if again := codePlanOf(t, maintainJSON(t, other, map[string]any{})); again["batch_id"] != siblings[0]["batch_id"] || again["logical_plan"] != logical {
		t.Fatalf("with every sibling delivered each server keeps its own: %v", again["batch_id"])
	}
}

func TestParallelSiblingDeliveryByBatchIDCarriesItsTargetsOnly(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	first := maintainJSON(t, session, map[string]any{})
	plan := codePlanOf(t, first)
	sibling := siblingsOf(plan)[0]
	delivery := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]})
	deliveryPlan := codePlanOf(t, delivery)
	if delivery["status"] != autoStatusRepairRequired || deliveryPlan["batch_id"] != sibling["batch_id"] ||
		deliveryPlan["sibling_of"] != plan["logical_plan"] || delivery["next_action"] != "author_complete_current_machine_batch" {
		t.Fatalf("a sibling delivers as the complete current batch: %v %v", delivery["status"], deliveryPlan)
	}
	if got, want := strings.Join(candidatePaths(delivery), ","), strings.Join(siblingTargets(t, root, sibling), ","); got != want {
		t.Fatalf("delivery carries the issued targets and nothing else:\n got %s\nwant %s", got, want)
	}
	others := siblingsOf(deliveryPlan)
	if len(others) != 1 || others[0]["batch_id"] != plan["batch_id"] || others[0]["state"] != "delivered" {
		t.Fatalf("the first sibling is listed as delivered: %v", others)
	}
	if delivery["instructions_ref"] == nil && delivery["instructions"] == nil {
		t.Fatal("a delivered sibling carries the authoring contract or its reference")
	}
	unknown := callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": strings.Repeat("0", 64)})
	if !strings.Contains(unknown, "maintain_batch_id_unknown") {
		t.Fatalf("an unknown batch_id is refused: %s", unknown)
	}
}

// In a host whose subagents share the session's MCP server (Claude Code),
// the authoring contract has already gone out once in that process, so a
// sibling fetched by batch_id alone carries only its reference: a subagent
// with a fresh context would author without the rules. The hand-out
// instruction therefore asks for include_instructions, which returns the
// complete contract with the sibling (2026-11-02).
func TestParallelSiblingForAFreshSubagentCarriesTheContract(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	first := maintainJSON(t, session, map[string]any{})
	if first["authoring_meta"] == nil {
		t.Fatal("the first Maintain of the process carries the contract")
	}
	joined := fmt.Sprint(first["instructions"])
	if !strings.Contains(joined, "include_instructions: true") || !strings.Contains(joined, "batch_id") {
		t.Fatalf("the hand-out line does not ask for the contract: %s", joined)
	}
	sibling := siblingsOf(codePlanOf(t, first))[0]
	bare := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]})
	if bare["authoring_meta"] != nil || bare["instructions_ref"] == nil {
		t.Fatalf("in the same process a bare sibling fetch carries only the reference: meta=%v ref=%v", bare["authoring_meta"] != nil, bare["instructions_ref"])
	}
	full := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"], "include_instructions": true})
	if full["authoring_meta"] == nil || full["instructions"] == nil {
		t.Fatal("include_instructions returns the complete contract with the sibling")
	}
}

// runParallelBuild authors every batch of a parallel first build, applying
// the siblings of each round in the order chosen, until Maintain is aligned.
func runParallelBuild(t *testing.T, root string, reverse bool) {
	t.Helper()
	session := connectMCPClient(t, root)
	for round := 0; round < 40; round++ {
		response := maintainJSON(t, session, map[string]any{})
		if response["status"] == autoStatusApplied && response["aligned"] == true {
			return
		}
		plan := codePlanOf(t, response)
		batches := []map[string]any{response}
		for _, sibling := range siblingsOf(plan) {
			if sibling["state"] == "applied" {
				continue
			}
			batches = append(batches, maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]}))
		}
		if reverse {
			for i, j := 0, len(batches)-1; i < j; i, j = i+1, j-1 {
				batches[i], batches[j] = batches[j], batches[i]
			}
		}
		for _, batch := range batches {
			applied := applyMaintainBatch(t, session, batch)
			if applied["status"] != autoStatusApplied {
				t.Fatalf("sibling apply failed: %v %v", applied["status"], applied["findings"])
			}
		}
	}
	t.Fatal("the build did not align within 40 rounds")
}

func TestParallelSiblingsApplyInEitherOrderToTheSameEntrySet(t *testing.T) {
	forward := parallelVolumesRepo(t, 60, 2)
	runParallelBuild(t, forward, false)
	backward := parallelVolumesRepo(t, 60, 2)
	runParallelBuild(t, backward, true)
	factsA, setA := verifyAligned(t, forward)
	factsB, setB := verifyAligned(t, backward)
	if !factsA.GovernanceAligned || !factsB.GovernanceAligned {
		t.Fatalf("both orders end aligned: %v %v", factsA.Result, factsB.Result)
	}
	a, b := codeEntrySet(setA), codeEntrySet(setB)
	if strings.Join(a, "\n") != strings.Join(b, "\n") || len(a) < 61 {
		t.Fatalf("both orders leave the same Entry set: %d vs %d", len(a), len(b))
	}
}

func TestParallelForeignEditInvalidatesOutstandingSiblingsAndKeepsAppliedOnes(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	first := maintainJSON(t, session, map[string]any{})
	plan := codePlanOf(t, first)
	sibling := siblingsOf(plan)[0]
	if applied := applyMaintainBatch(t, session, first); applied["status"] != autoStatusApplied {
		t.Fatalf("first sibling applies: %v", applied)
	}
	appliedPaths := candidatePaths(first)
	// A write outside the plan: the fixture's own main.go Entry re-authored
	// through the direct single-object path.
	direct := callVolumeTool(t, session, "aoci_update_entry", map[string]any{"path": "main.go",
		"source_sha256": volumeSourceSHA(t, root, "main.go"),
		"new_entry":     "main.go[CD9S]: F:run the fixture after a foreign edit | R:- | A:main | S:Keep execution deterministic"})
	if !strings.Contains(direct, `"status":"applied"`) {
		t.Fatalf("direct write: %s", direct)
	}
	delivery := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]})
	deliveryPlan := codePlanOf(t, delivery)
	if delivery["status"] != autoStatusStopped || siblingsOf(deliveryPlan)[0]["state"] != "invalidated" ||
		delivery["next_action"] != "call_no_argument_aoci_maintain_for_current_machine_batch" {
		t.Fatalf("the outstanding sibling is invalidated by a foreign change: %v %v", delivery["status"], siblingsOf(deliveryPlan))
	}
	replanned := maintainJSON(t, session, map[string]any{})
	replannedPlan := codePlanOf(t, replanned)
	if replannedPlan["logical_plan"] == plan["logical_plan"] || replannedPlan["batch_id"] == sibling["batch_id"] {
		t.Fatal("no-argument Maintain plans afresh after a foreign change")
	}
	_, set := verifyAligned(t, root)
	present := map[string]bool{}
	for _, object := range set.Volumes[cognition.ScopeCode].Objects {
		present[object.Entry.RelPath] = true
	}
	for _, path := range appliedPaths {
		if !present[path] {
			t.Fatalf("the applied sibling stays formal: %s missing", path)
		}
	}
	for _, path := range candidatePaths(replanned) {
		if present[path] {
			t.Fatalf("the fresh plan issues only what is still missing: %s", path)
		}
	}
}

func TestParallelSourceDriftInvalidatesOneSiblingOnly(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	first := maintainJSON(t, session, map[string]any{})
	plan := codePlanOf(t, first)
	sibling := siblingsOf(plan)[0]
	drifted := siblingTargets(t, root, sibling)[0]
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(drifted)), []byte("package drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delivery := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]})
	if delivery["status"] != autoStatusStopped || siblingsOf(codePlanOf(t, delivery))[0]["state"] != "invalidated" {
		t.Fatalf("the sibling holding the drifted target is invalidated: %v", delivery["status"])
	}
	still := maintainJSON(t, session, map[string]any{"batch_id": plan["batch_id"]})
	if still["status"] != autoStatusRepairRequired || strings.Join(candidatePaths(still), ",") != strings.Join(candidatePaths(first), ",") {
		t.Fatalf("the other sibling stays issued: %v", still["status"])
	}
	again := maintainJSON(t, session, map[string]any{})
	againPlan := codePlanOf(t, again)
	if againPlan["logical_plan"] != plan["logical_plan"] || againPlan["batch_id"] != plan["batch_id"] {
		t.Fatal("the plan and its delivered sibling survive one target's drift")
	}
	listed := siblingsOf(againPlan)
	if len(listed) != 1 || listed[0]["batch_id"] != sibling["batch_id"] || listed[0]["state"] != "invalidated" {
		t.Fatalf("the drifted sibling is listed as invalidated: %v", listed)
	}
	// Once the surviving sibling applies, the plan completes and the next one
	// issues the drifted targets again, under their new digests.
	if applied := applyMaintainBatch(t, session, again); applied["status"] != autoStatusApplied || applied["remaining"] != againPlan["remaining"] {
		t.Fatalf("the surviving sibling applies and the invalidated targets stay unissued: status=%v remaining=%v want=%v",
			applied["status"], applied["remaining"], againPlan["remaining"])
	}
	next := maintainJSON(t, session, map[string]any{})
	nextPlan := codePlanOf(t, next)
	issued := map[string]bool{}
	for _, path := range candidatePaths(next) {
		issued[path] = true
	}
	for _, other := range siblingsOf(nextPlan) {
		for _, path := range siblingTargets(t, root, other) {
			issued[path] = true
		}
	}
	if nextPlan["logical_plan"] == plan["logical_plan"] || !issued[drifted] {
		t.Fatalf("the next plan issues the drifted target: same_plan=%v issued=%v", nextPlan["logical_plan"] == plan["logical_plan"], issued[drifted])
	}
}

func TestParallelApplyCountsOnlyUnissuedTargets(t *testing.T) {
	for _, outside := range []int{0, 1} {
		t.Run(fmt.Sprintf("outside_%d", outside), func(t *testing.T) {
			root := parallelVolumesRepo(t, 8+outside, 2)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.SetCodeCognitionBatchEntries(4); err != nil {
				t.Fatal(err)
			}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			one, two := connectMCPClient(t, root), connectMCPClient(t, root)
			first := maintainJSON(t, one, nil)
			plan := codePlanOf(t, first)
			second := maintainJSON(t, two, map[string]any{"batch_id": siblingsOf(plan)[0]["batch_id"]})
			want := float64(outside)
			for _, response := range []map[string]any{first, second} {
				if batch := response["authoring_batch"].(map[string]any); batch["remaining"] != want || batch["continuation_required"] != true {
					t.Fatalf("expected %d unissued targets with a sibling outstanding: %v", outside, batch)
				}
			}
			interim := applyMaintainBatch(t, one, first)
			if interim["status"] != autoStatusApplied || interim["remaining"] != want || interim["aligned"] != false ||
				interim["terminal_proof"] != nil || !strings.Contains(interim["next_action"].(string), "batch_id") {
				t.Fatalf("Apply must count unissued targets and direct the Host to finish the wave: status=%v remaining=%v aligned=%v next_action=%v",
					interim["status"], interim["remaining"], interim["aligned"], interim["next_action"])
			}
			last := applyMaintainBatch(t, two, second)
			if last["status"] != autoStatusApplied || last["remaining"] != want || last["aligned"] != (outside == 0) ||
				(last["terminal_proof"] != nil) != (outside == 0) {
				t.Fatalf("unexpected result after the wave: %v", last)
			}
		})
	}
}

func TestParallelConcurrentAppliesSerializeAndBothSucceed(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	one := connectMCPClient(t, root)
	two := connectMCPClient(t, root)
	first := maintainJSON(t, one, map[string]any{})
	sibling := siblingsOf(codePlanOf(t, first))[0]
	second := maintainJSON(t, two, map[string]any{"batch_id": sibling["batch_id"]})
	results := make([]map[string]any, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); results[0] = applyMaintainBatch(t, one, first) }()
	go func() { defer wait.Done(); results[1] = applyMaintainBatch(t, two, second) }()
	wait.Wait()
	for index, result := range results {
		if result["status"] != autoStatusApplied {
			t.Fatalf("apply %d under contention: %v %v", index, result["status"], result["findings"])
		}
	}
	_, set := verifyAligned(t, root)
	present := map[string]bool{}
	for _, object := range set.Volumes[cognition.ScopeCode].Objects {
		present[object.Entry.RelPath] = true
	}
	for _, path := range append(candidatePaths(first), candidatePaths(second)...) {
		if !present[path] {
			t.Fatalf("both siblings landed: %s missing", path)
		}
	}
}

func TestParallelFieldsStayOffTheWireUnderTheDefaultConfiguration(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 20)
	session := connectMCPClient(t, root)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	plan := codePlanOf(t, decodeMaintainJSON(t, raw))
	for _, key := range []string{"logical_plan", "siblings", "sibling_of"} {
		if _, present := plan[key]; present {
			t.Fatalf("the default configuration's code_plan carries no parallel field %s", key)
		}
	}
	if strings.Contains(raw, `"siblings"`) || strings.Contains(raw, `"sibling_of"`) {
		t.Fatal("the default configuration carries no sibling fields anywhere")
	}
	// Work that fits one batch issues no plan, so no batch_id is known; a
	// repository pinned to one batch refuses the field itself.
	if refused := callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": strings.Repeat("a", 64)}); !strings.Contains(refused, "maintain_batch_id_unknown") {
		t.Fatalf("a batch_id no plan issued is refused: %s", refused)
	}
	pinSingleBatch(t, root)
	refused := callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": strings.Repeat("a", 64)})
	if !strings.Contains(refused, "maintain_batch_id_requires_parallel_batches") {
		t.Fatalf("batch_id without parallel batches is refused: %s", refused)
	}
	cfg := config.DefaultConfig()
	for _, value := range []int{0, 5} {
		if err := cfg.SetCodeCognitionParallelBatches(value); err == nil {
			t.Fatalf("%d is outside the range", value)
		}
	}
	if err := cfg.SetCodeCognitionParallelBatches(4); err != nil || cfg.CodeCognitionParallelBatchLimit() != 4 {
		t.Fatal("the ceiling is 4")
	}
}

// pinSingleBatch sets code_cognition_parallel_batches to 1, the one current
// batch per Maintain the single-batch mechanics tests are written against.
func pinSingleBatch(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionParallelBatches(1); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}
