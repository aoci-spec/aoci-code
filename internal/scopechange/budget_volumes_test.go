package scopechange

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
)

// Under Volumes the Scope Change measures the Code Volume, where the Entries
// are, and the Whole-Index over every declared asset (audit G1-08): it used to
// measure the Entry-less Root alone, so its bands checked nothing and its
// plan and approval recorded zero Entries.
func TestVolumesScopeChangeMeasuresTheCodeVolume(t *testing.T) {
	root := buildVolumesChangeFixture(t)
	preview, err := Build(root, authorizationTestTime, emptyCandidateSet())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		total += len(data)
	}
	for name, report := range map[string]cognitionbudget.Report{"before": preview.Plan.WholeIndexBefore, "after": preview.Plan.WholeIndexAfter} {
		if report.EntryCount != 1 {
			t.Fatalf("%s: the plan must count the Code Volume's Entry: %d", name, report.EntryCount)
		}
		if report.WholeIndexTokens != cognitionbudget.EstimateTokensOfSize(total) {
			t.Fatalf("%s: the Whole-Index must span every asset: %d", name, report.WholeIndexTokens)
		}
	}
}
