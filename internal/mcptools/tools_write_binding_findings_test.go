package mcptools

import "testing"

// A binding defect in one submitted candidate used to come back as a bare
// bad_args naming nothing: a model that had mistyped one candidate_id received
// the same rejection for every resubmission. The rejection now carries a
// Repair Finding with the candidate's position, path, and the field to fix,
// with no formal write started.
func TestCodeBatchBindingDefectsNameTheCandidateAndField(t *testing.T) {
	for _, test := range []struct {
		name   string
		field  string
		mutate func(entry map[string]any)
	}{
		{"empty candidate_id", "candidate_id", func(entry map[string]any) { entry["candidate_id"] = "" }},
		{"malformed source_sha256", "source_sha256", func(entry map[string]any) { entry["source_sha256"] = "not-a-digest" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 3)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			if maintain.CodePlan == nil || len(maintain.CodePlan.Candidates) != 3 {
				t.Fatalf("expected three candidates: %#v", maintain.CodePlan)
			}
			arguments := codeBatchArguments(maintain.CodePlan)
			entries := arguments["entries"].([]map[string]any)
			test.mutate(entries[1])
			result := applyVolumeBatch(t, session, arguments)
			if result.Status != autoStatusRepairRequired || result.Applied != 0 || result.FormalWritesStarted {
				t.Fatalf("binding defect must stop before any write as repair_required: %#v", result)
			}
			if len(result.Findings) != 1 {
				t.Fatalf("expected exactly one finding: %#v", result.Findings)
			}
			finding := result.Findings[0]
			if finding.CandidateIndex != 2 || finding.Path != maintain.CodePlan.Candidates[1].Path ||
				finding.Field != test.field || finding.SafeRepairAction == "" {
				t.Fatalf("finding must name candidate 2, its path, the %s field, and a repair action: %#v", test.field, finding)
			}
		})
	}
}
