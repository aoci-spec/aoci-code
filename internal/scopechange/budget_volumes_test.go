package scopechange

import (
	"github.com/aoci-spec/aoci-code/internal/config"
	"os"
	"path/filepath"
	"strings"
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

// Under Volumes the S hard limit is the Meta's declared quota, as governance
// applies it (audit G4-06): an S within a raised quota is no violation.
func TestVolumesBudgetReportUsesTheMetaSQuota(t *testing.T) {
	root := buildVolumesChangeFixture(t)
	meta, err := os.ReadFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	writeChangeFixture(t, root, "aoci.meta.txt", strings.TrimRight(string(meta), "\n")+"\n#S quota: C9-8≤600 C7-4≤400 C3-1≤50\n")
	code, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("确", 300)
	writeChangeFixture(t, root, "aoci.code.txt", strings.Replace(string(code), "S:Execution remains deterministic", "S:"+long, 1))
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildBudgetReport(root, cfg, raw, cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) != 0 {
		t.Fatalf("an S within the Meta quota is no violation: %+v", report.Violations)
	}
}
