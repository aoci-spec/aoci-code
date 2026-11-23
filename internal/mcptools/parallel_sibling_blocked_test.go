package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
)

// A sibling fetched while the repository is blocked, here by a pending
// recovery receipt, is not handed out: the plan keeps it issued and the
// answer is the stop (audit G2-06).
func TestParallelSiblingIsWithheldWhileGovernanceIsBlocked(t *testing.T) {
	root := parallelVolumesRepo(t, 60, 2)
	session := connectMCPClient(t, root)
	plan := codePlanOf(t, maintainJSON(t, session, map[string]any{}))
	sibling := siblingsOf(plan)[0]
	transactions := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(transactions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactions, "foreign-receipt.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delivery := maintainJSON(t, session, map[string]any{"batch_id": sibling["batch_id"]})
	if delivery["status"] != autoStatusStopped || len(candidatePaths(delivery)) != 0 {
		t.Fatalf("a blocked repository delivered a sibling: status=%v candidates=%v", delivery["status"], candidatePaths(delivery))
	}
	stored, err := codebatch.LoadParallelPlan(root)
	if err != nil || stored == nil {
		t.Fatalf("parallel plan: %v", err)
	}
	for _, delivered := range stored.Delivered {
		if delivered == sibling["batch_id"] {
			t.Fatal("a withheld sibling was recorded as delivered")
		}
	}
}
