package cognitionbudget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func budgetFixture(body string) []byte {
	return []byte("# header\n===/repo/===\nmain.go[C.RT.9.T]: F:core | R:" + body + " | A:- | S:" + body + "\n")
}

func TestValidateEnforceRejectsWithoutRewriting(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 10, WarningTokens: 20, MaxTokens: 30}
	policy.R = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	policy.S = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	raw := budgetFixture(strings.Repeat("relationship ", 12))
	validation, err := Validate("/repo", raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Allowed || len(validation.Violations) < 3 {
		t.Fatalf("enforce mode allowed over-budget index: %+v", validation)
	}
	if string(raw) != string(budgetFixture(strings.Repeat("relationship ", 12))) {
		t.Fatal("validation changed model-authored bytes")
	}
}

func TestObserveReportsButDoesNotBlock(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeObserve)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 1, WarningTokens: 2, MaxTokens: 3}
	raw := budgetFixture("one relationship")
	validation, err := Validate("/repo", raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Allowed || validation.Report.Status != machinecontract.BudgetStatusExceeded {
		t.Fatalf("observe mode must report exceeded without blocking: %+v", validation)
	}
}

func TestNormalizeRequiresCompleteNonOverlappingCBands(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.R = []FieldBand{{MinC: 1, MaxC: 8, TargetTokens: 1, MaxTokens: 2}}
	if _, err := Normalize(policy); err == nil {
		t.Fatal("C9 budget gap accepted")
	}
	policy = DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.S = append(policy.S, FieldBand{MinC: 9, MaxC: 9, TargetTokens: 1, MaxTokens: 2})
	if _, err := Normalize(policy); err == nil {
		t.Fatal("overlapping C9 budget accepted")
	}
}

func TestProjectedReportCarriesCompleteHardGateEvidence(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 15, WarningTokens: 20, MaxTokens: 25}
	policy.R = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	policy.S = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	current := budgetFixture("short")
	projected := budgetFixture(strings.Repeat("high entropy relationship ", 12))
	report, err := ValidateProjected("/repo", current, projected, policy)
	if err != nil {
		t.Fatal(err)
	}
	if report.Allowed || report.CurrentTokens == 0 || report.ProjectedWholeIndexTokens <= report.MaxTokens ||
		report.BatchDeltaTokens <= 0 || report.TargetTokens != 15 || report.WarningTokens != 20 || report.MaxTokens != 25 ||
		len(report.LargestEntries) == 0 || len(report.LargestR) == 0 || len(report.LargestS) == 0 ||
		len(report.SuggestedCompression) == 0 || len(report.Violations) < 3 {
		t.Fatalf("projected hard-gate evidence incomplete: %+v", report)
	}
	if string(projected) != string(budgetFixture(strings.Repeat("high entropy relationship ", 12))) {
		t.Fatal("projection validation rewrote model-authored semantics")
	}
}

func TestBuildRejectsIndexParseWarnings(t *testing.T) {
	raw := []byte("# header\n===/repo/===\nmain.go[C.RT.9.T]: F:one | R:- | A:- | S:-\n" +
		"main.go[C.RT.9.T]: F:duplicate | R:- | A:- | S:-\n")
	if _, err := Build("/repo", raw, DefaultPolicy(machinecontract.BudgetModeObserve)); err == nil ||
		!strings.Contains(err.Error(), "cognition_budget_index_parse_warnings") {
		t.Fatalf("malformed index received a budget report: %v", err)
	}
}

func TestBudgetStatisticsScaleToTenThousandEntries(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("# header\n===/repo/===\n")
	for index := 0; index < 10000; index++ {
		fmt.Fprintf(&builder, "entry-%05d.go[C.RT.5.T]: F:core | R:- | A:- | S:-\n", index)
	}
	report, err := Build("/repo", []byte(builder.String()), DefaultPolicy(machinecontract.BudgetModeObserve))
	if err != nil {
		t.Fatal(err)
	}
	if report.EntryCount != 10000 || len(report.LargestEntries) != 10 || report.WholeIndexTokens == 0 {
		t.Fatalf("large budget report incomplete: entries=%d largest=%d tokens=%d", report.EntryCount, len(report.LargestEntries), report.WholeIndexTokens)
	}
}

func TestTokenEstimatorUsesUTF8BytesAcrossLocales(t *testing.T) {
	if got := EstimateTokens([]byte("abc")); got != 1 {
		t.Fatalf("ASCII byte estimate=%d", got)
	}
	if got := EstimateTokens([]byte("认")); got != 1 {
		t.Fatalf("UTF-8 locale byte estimate=%d", got)
	}
	if got := EstimateTokens([]byte("abc认")); got != 2 {
		t.Fatalf("mixed-locale byte estimate=%d", got)
	}
}

func TestWholeIndexStatusBoundaries(t *testing.T) {
	policy := WholeIndexPolicy{TargetTokens: 10, WarningTokens: 20, MaxTokens: 30}
	for _, testCase := range []struct {
		tokens int
		want   string
	}{
		{tokens: 10, want: machinecontract.BudgetStatusHealthy},
		{tokens: 11, want: machinecontract.BudgetStatusNearBudget},
		{tokens: 20, want: machinecontract.BudgetStatusWarning},
		{tokens: 30, want: machinecontract.BudgetStatusWarning},
		{tokens: 31, want: machinecontract.BudgetStatusExceeded},
	} {
		if got := statusFor(testCase.tokens, policy); got != testCase.want {
			t.Fatalf("tokens=%d status=%s want=%s", testCase.tokens, got, testCase.want)
		}
	}
}
