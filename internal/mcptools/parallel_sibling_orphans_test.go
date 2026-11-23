package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Code orphans ride only the response that issued a parallel plan; a sibling
// handed out later by no-argument Maintain carries none, both apply in either
// order, and the build reaches aligned (audit G2-10, rv37).
func TestParallelSiblingsCarryNoOrphanRemovalsAndTheBuildStillAligns(t *testing.T) {
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
	if codePlanOf(t, responseA)["batch_id"] == codePlanOf(t, responseB)["batch_id"] {
		t.Skip("fixture handed both sessions the same batch")
	}
	if listed, _ := responseA["orphan_remove_candidates"].([]any); len(listed) != len(orphans) {
		t.Fatalf("the response that issued the plan does not carry the orphans: %v", listed)
	}
	if listed, _ := responseB["orphan_remove_candidates"].([]any); len(listed) != 0 {
		t.Fatalf("a sibling handed out later carries the same orphans: %v", listed)
	}
	withRemovals := func(response map[string]any) map[string]any {
		plan := codePlanOf(t, response)
		entries := []map[string]any{}
		for _, candidate := range response["candidates"].([]any) {
			item := candidate.(map[string]any)
			entries = append(entries, map[string]any{"path": item["path"], "source_sha256": item["source_sha256"],
				"candidate_id": item["candidate_id"], "new_entry": fixtureEntryLine(item["path"].(string))})
		}
		arguments := map[string]any{"code_batch_id": plan["batch_id"], "entries": entries}
		if listed, _ := response["orphan_remove_candidates"].([]any); len(listed) > 0 {
			removals := []map[string]any{}
			for _, ref := range listed {
				removals = append(removals, map[string]any{"path": ref})
			}
			arguments["removals"] = removals
		}
		return arguments
	}
	// Second first, then the issuing one with its removals: either order applies.
	if applied := decodeMaintainJSON(t, callVolumeTool(t, second, "aoci_update_entry", withRemovals(responseB))); applied["status"] != autoStatusApplied {
		t.Fatalf("second sibling: %v %v", applied["status"], applied["findings"])
	}
	if applied := decodeMaintainJSON(t, callVolumeTool(t, first, "aoci_update_entry", withRemovals(responseA))); applied["status"] != autoStatusApplied {
		t.Fatalf("issuing sibling with the removals: %v %v", applied["status"], applied["findings"])
	}
	session := connectMCPClient(t, root)
	for round := 0; round < 20; round++ {
		response := maintainJSON(t, session, map[string]any{})
		if response["status"] == autoStatusApplied && response["aligned"] == true {
			return
		}
		plan, _ := response["code_plan"].(map[string]any)
		entries := []map[string]any{}
		candidates, _ := response["candidates"].([]any)
		for _, candidate := range candidates {
			item := candidate.(map[string]any)
			entries = append(entries, map[string]any{"path": item["path"], "source_sha256": item["source_sha256"],
				"candidate_id": item["candidate_id"], "new_entry": fixtureEntryLine(item["path"].(string))})
		}
		arguments := map[string]any{"entries": entries}
		if batch, ok := plan["batch_id"]; ok && batch != nil && batch != "" {
			arguments["code_batch_id"] = batch
		}
		if listed, ok := response["orphan_remove_candidates"].([]any); ok && len(listed) > 0 {
			removals := []map[string]any{}
			for _, ref := range listed {
				removals = append(removals, map[string]any{"path": ref})
			}
			arguments["removals"] = removals
		}
		applied := decodeMaintainJSON(t, callVolumeTool(t, session, "aoci_update_entry", arguments))
		if applied["status"] != autoStatusApplied {
			t.Fatalf("round %d: %v %v", round, applied["status"], applied["findings"])
		}
	}
	t.Fatal("the orphans were never removed within 20 rounds")
}
