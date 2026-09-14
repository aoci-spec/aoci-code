package mcptools

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// A Missing path the team's curation_exclude rules out is never issued as a
// candidate, but the batch total used to count it: one excluded and one
// actionable fresh file answered total_targets 2, included 1, remaining 1,
// continuation_required true, and every later Maintain answered the same,
// while the plan held one candidate. The total now comes from the same
// authoring-work set the candidates are built from.
func TestVolumeMaintainTotalMatchesThePlanWhenAMissingPathIsNotActionable(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CurationExclude = []string{"generated/skip.go"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, "generated/keep.go", "package generated\n\nconst Keep = 1\n")
	writeVolumeTestFile(t, root, "generated/skip.go", "package generated\n\nconst Skip = 2\n")

	maintain := maintainVolumeBatch(t, connectMCPClient(t, root))
	if maintain.Status != autoStatusRepairRequired || len(maintain.Candidates) != 1 ||
		maintain.Candidates[0].Path != "generated/keep.go" {
		t.Fatalf("expected the one actionable candidate: %#v", maintain.Candidates)
	}
	if maintain.Batch.TotalTargets != 1 || maintain.Batch.Included != 1 || maintain.Batch.Remaining != 0 ||
		maintain.Batch.ContinuationRequired {
		t.Fatalf("the batch total must count only work a candidate can carry: %#v", maintain.Batch)
	}
	if maintain.CodePlan == nil || maintain.CodePlan.TotalTargets != 1 {
		t.Fatalf("plan and batch disagree: %#v", maintain.CodePlan)
	}
}
