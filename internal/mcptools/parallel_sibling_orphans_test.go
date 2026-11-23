package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Siblings handed out by no-argument Maintain in different sessions carry
// the same orphan removals; the first applies them, and the second, whose
// removals are already done in the Volume its plan replays to, applies
// instead of stopping on a stale preimage (audit G2-10).
func TestParallelSiblingsSharingOrphanRemovalsBothApply(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	runParallelBuild(t, root, false)
	orphans := []string{"pkg/mod000/file000.go", "pkg/mod000/file001.go"}
	for _, rel := range orphans {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	for index := 10; index < 60; index++ {
		rel := fmt.Sprintf("pkg/mod%03d/file%03d.go", index/10, index)
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("// changed\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, second := connectMCPClient(t, root), connectMCPClient(t, root)
	responseA := maintainJSON(t, first, map[string]any{})
	responseB := maintainJSON(t, second, map[string]any{})
	planA, planB := codePlanOf(t, responseA), codePlanOf(t, responseB)
	if planA["batch_id"] == planB["batch_id"] {
		t.Skip("fixture handed both sessions the same batch")
	}
	apply := func(session any, response map[string]any) map[string]any {
		plan := codePlanOf(t, response)
		entries := []map[string]any{}
		for _, candidate := range response["candidates"].([]any) {
			item := candidate.(map[string]any)
			entries = append(entries, map[string]any{"path": item["path"], "source_sha256": item["source_sha256"],
				"candidate_id": item["candidate_id"], "new_entry": fixtureEntryLine(item["path"].(string))})
		}
		removals := []map[string]any{}
		if listed, ok := response["orphan_remove_candidates"].([]any); ok {
			for _, ref := range listed {
				removals = append(removals, map[string]any{"path": ref})
			}
		}
		arguments := map[string]any{"code_batch_id": plan["batch_id"], "entries": entries}
		if len(removals) > 0 {
			arguments["removals"] = removals
		}
		return decodeMaintainJSON(t, callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", arguments))
	}
	if listed, _ := responseB["orphan_remove_candidates"].([]any); len(listed) == 0 {
		t.Skip("the second sibling carries no orphan removals")
	}
	if applied := apply(first, responseA); applied["status"] != autoStatusApplied {
		t.Fatalf("first sibling: %v %v", applied["status"], applied["findings"])
	}
	if applied := apply(second, responseB); applied["status"] != autoStatusApplied {
		t.Fatalf("second sibling with the same orphan removals: %v %v", applied["status"], applied["findings"])
	}
}
