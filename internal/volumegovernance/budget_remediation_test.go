package volumegovernance

import (
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A finding that carries only a code makes the operator guess. A field band is
// a project-owned policy with two real levers, so both levers and the numbers
// that produced the refusal travel with the finding.
func TestBudgetExceededFindingNamesBothLevers(t *testing.T) {
	budget := BudgetFacts{Mode: machinecontract.BudgetModeEnforce,
		WholeIndexTokens: 250000, MaxTokens: 240000,
		Violations: []cognitionbudget.Violation{
			{Code: "entry_field_budget_exceeded", Path: "a.go", Field: "R", Actual: 95, Maximum: 90}}}

	cause := budgetExceededCause(budget)
	for _, want := range []string{"entry_field_budget_exceeded=1", "250000", "240000"} {
		if !strings.Contains(cause, want) {
			t.Fatalf("cause omits %s: %q", want, cause)
		}
	}
	action := budgetExceededRepairAction(budget)
	for _, want := range []string{"re-author", "scope budget set", "Scope Change"} {
		if !strings.Contains(action, want) {
			t.Fatalf("repair action omits %q: %s", want, action)
		}
	}
}

func TestEntryFieldBudgetFindingDoesNotOfferTheWholeIndexLever(t *testing.T) {
	budget := BudgetFacts{Mode: machinecontract.BudgetModeEnforce,
		WholeIndexTokens: 1000, MaxTokens: 240000,
		Violations: []cognitionbudget.Violation{
			{Code: "entry_field_budget_exceeded", Path: "a.go", Field: "S", Actual: 90, Maximum: 80}}}
	action := budgetExceededRepairAction(budget)
	if strings.Contains(action, "--max-tokens") {
		t.Fatalf("a per-Entry band violation was told to raise the Whole-Index maximum: %s", action)
	}
	if !strings.Contains(action, "re-author") {
		t.Fatalf("a per-Entry band violation was not told to re-author: %s", action)
	}
}

// The published numbers must be enforced by the thing that produces them, or
// they drift into being wrong the first time somebody changes a default. This
// pins the doc against the code, not against a copy of the code.
func TestScaleBoundaryDocumentationMatchesTheNewProjectDefault(t *testing.T) {
	raw, err := os.ReadFile("../../docs/managed-scope-and-budget.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce).WholeIndex
	for label, value := range map[string]int{
		"target": policy.TargetTokens, "warning": policy.WarningTokens, "max": policy.MaxTokens,
	} {
		if !strings.Contains(text, itoa(value)) {
			t.Fatalf("docs/managed-scope-and-budget.md does not state the new-project %s budget %d; "+
				"the published number and the code have diverged", label, value)
		}
	}
	legacy := cognitionbudget.LegacyPolicy().WholeIndex
	if !strings.Contains(text, itoa(legacy.MaxTokens)) {
		t.Fatalf("the docs no longer state the frozen legacy maximum %d, which every repository "+
			"without a cognition_budget block still resolves", legacy.MaxTokens)
	}
	if !strings.Contains(text, "scope budget set") {
		t.Fatal("the Scale boundary section does not name the command that changes the budget")
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
