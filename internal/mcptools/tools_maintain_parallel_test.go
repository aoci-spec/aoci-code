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

func siblingTargets(sibling map[string]any) []string {
	targets := []string{}
	for _, value := range sibling["targets"].([]any) {
		targets = append(targets, value.(string))
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
	ahead := siblingTargets(siblings[0])
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
	// The same state keeps the same plan: a second no-argument Maintain hands
	// out the sibling no one has been handed yet, and a third, with every
	// sibling delivered, the first of them again.
	second := maintainJSON(t, session, map[string]any{})
	secondPlan := codePlanOf(t, second)
	if secondPlan["logical_plan"] != logical || secondPlan["batch_id"] != siblings[0]["batch_id"] ||
		siblingsOf(secondPlan)[0]["batch_id"] != plan["batch_id"] || siblingsOf(secondPlan)[0]["state"] != "delivered" {
		t.Fatalf("the second Maintain delivers the undelivered sibling under the same plan: %v", secondPlan)
	}
	third := maintainJSON(t, session, map[string]any{})
	if thirdPlan := codePlanOf(t, third); thirdPlan["batch_id"] != plan["batch_id"] || thirdPlan["logical_plan"] != logical {
		t.Fatalf("with every sibling delivered the first is delivered again: %v", thirdPlan["batch_id"])
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
	if got, want := strings.Join(candidatePaths(delivery), ","), strings.Join(siblingTargets(sibling), ","); got != want {
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
	drifted := siblingTargets(sibling)[0]
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
	if applied := applyMaintainBatch(t, session, again); applied["status"] != autoStatusApplied {
		t.Fatalf("the surviving sibling applies: %v", applied)
	}
	next := maintainJSON(t, session, map[string]any{})
	nextPlan := codePlanOf(t, next)
	issued := map[string]bool{}
	for _, path := range candidatePaths(next) {
		issued[path] = true
	}
	for _, other := range siblingsOf(nextPlan) {
		for _, path := range siblingTargets(other) {
			issued[path] = true
		}
	}
	if nextPlan["logical_plan"] == plan["logical_plan"] || !issued[drifted] {
		t.Fatalf("the next plan issues the drifted target: same_plan=%v issued=%v", nextPlan["logical_plan"] == plan["logical_plan"], issued[drifted])
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
