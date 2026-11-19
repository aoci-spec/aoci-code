package managedscope

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A path Git ignores never fills a row of the directories the proposal shows.
func TestProposalDirectoriesLeaveOutWhatGitIgnores(t *testing.T) {
	evaluation := &Evaluation{
		Index: []PathEvaluation{{Path: "src/a.go", Role: machinecontract.ScopeRoleIndex, GitStatus: "tracked"}},
		Exclude: []PathEvaluation{
			{Path: "build-install/lib/x.so", Role: machinecontract.ScopeRoleExclude, GitStatus: "ignored"},
			{Path: "build-install/lib/y.so", Role: machinecontract.ScopeRoleExclude, GitStatus: "ignored"},
			{Path: "vendor/z.go", Role: machinecontract.ScopeRoleExclude, GitStatus: "tracked"},
		},
	}
	proposal := BuildProposal(evaluation, machinecontract.ScopeProfileProduction, 0)
	for _, row := range proposal.LargestDirectories {
		if row.Directory == "build-install/lib" {
			t.Fatalf("an ignored directory is listed: %+v", proposal.LargestDirectories)
		}
	}
	if len(proposal.LargestDirectories) != 2 {
		t.Fatalf("the tracked directories are not all listed: %+v", proposal.LargestDirectories)
	}
}
