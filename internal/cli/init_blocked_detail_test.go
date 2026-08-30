package cli

import (
	"strings"
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// The refusal explains why initialization stopped, and it must explain the
// reason that actually stopped it. Approval keys on AutoBlockerCount, but the
// explanation still branched on RequiredHumanReview — the retired predicate —
// so a repository blocked because its profile indexes nothing was told instead
// that a tracked file needed a decision, and following that advice deleted the
// file for nothing before the real message appeared.
func TestInitRefusalNamesTheReasonThatActuallyBlocked(t *testing.T) {
	// Only reason: the policy would index nothing, while candidates exist.
	// Excluded tracked paths are present too, and must not be blamed.
	evaluation := &managedscope.Evaluation{
		IndexCount: 0,
		Exclude: []managedscope.PathEvaluation{
			{Path: "vendor/framework/index.js", GitStatus: "tracked"},
		},
		SafeInventory: afs.SafeInventorySummary{
			FinalManagedCandidates: 3, AutoBlockerCount: 0, RequiredHumanReview: 1},
		RequiredHumanReview: 1,
	}

	detail := initialScopeBlockedDetail(evaluation, "production", 0)
	if strings.Contains(detail, "vendor/framework/index.js") {
		t.Fatalf("the refusal blames an excluded tracked path that did not block anything:\n%s\n"+
			"Approval no longer keys on excluded tracked paths, so the explanation must not either; "+
			"following this advice deletes a file and reaches the same refusal.", detail)
	}

	// And when an opt-in genuinely blocks, that path is still named.
	blocked := &managedscope.Evaluation{
		IndexCount: 5,
		Exclude: []managedscope.PathEvaluation{
			{Path: "config/secrets.env", GitStatus: "tracked"},
		},
		SafeInventory: afs.SafeInventorySummary{
			FinalManagedCandidates: 5, AutoBlockerCount: 1, RequiredHumanReview: 1},
		RequiredHumanReview: 1,
	}
	if got := initialScopeBlockedDetail(blocked, "production", 0); !strings.Contains(got, "config/secrets.env") {
		t.Fatalf("a path whose content an opt-in will read is no longer named:\n%s", got)
	}
}
