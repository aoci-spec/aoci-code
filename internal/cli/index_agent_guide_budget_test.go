package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// bindAgentGuideCommands rewrites every command in the Guide to the host's
// absolute binary path. Its field list is maintained by hand, so a new command
// ships unbound unless somebody remembers to extend it — a bare "aoci …" string
// in a Guide where every sibling is an absolute quoted path. This test is the
// reason the next person does not have to remember.
func TestEveryAgentGuideCommandFieldIsBound(t *testing.T) {
	commands := agentGuideCommands{}
	value := reflect.ValueOf(&commands).Elem()
	for index := 0; index < value.NumField(); index++ {
		if value.Field(index).Kind() != reflect.String {
			t.Fatalf("agentGuideCommands.%s is not a string; the binder assumes every field is a command",
				value.Type().Field(index).Name)
		}
		value.Field(index).SetString("aoci sentinel")
	}
	bindAgentGuideCommands(&commands, "/host/bin/aoci")
	for index := 0; index < value.NumField(); index++ {
		got := value.Field(index).String()
		if !strings.HasPrefix(got, "/host/bin/aoci") {
			t.Fatalf("agentGuideCommands.%s was not bound to the host binary: %q\n"+
				"add &commands.%s to the field list in bindAgentGuideCommands",
				value.Type().Field(index).Name, got, value.Type().Field(index).Name)
		}
	}
}

// A repository over its Whole-Index budget used to receive a bare finding code
// and an instruction to compress — the wrong remediation for an index that grew
// because the repository did, and the only one offered. Both levers must reach
// the operator, and the raise lever must carry the command that performs it.
func TestBudgetBlockedGuideHandsBackBothLevers(t *testing.T) {
	facts := &volumegovernance.Facts{
		Findings: []volumegovernance.Finding{{Code: "cognition_budget_exceeded"}},
		Budget: volumegovernance.BudgetFacts{
			Mode: "enforce", WholeIndexTokens: 250000, MaxTokens: 240000,
			Violations: []cognitionbudget.Violation{
				{Code: "whole_index_budget_exceeded", Actual: 250000, Maximum: 240000}},
		},
	}
	guide := volumeAgentGuide{}
	applyVolumeBlockedRemediation(&guide, facts)

	if guide.Stop == nil {
		t.Fatal("a budget-blocked Guide returned no Stop facts, so the host has nothing machine-readable to act on")
	}
	if guide.Stop.RuleCode != "cognition_budget_exceeded" {
		t.Fatalf("stop rule code = %q", guide.Stop.RuleCode)
	}
	if guide.Commands.ScopeBudget == "" {
		t.Fatal("the Guide did not hand back the command that raises the budget")
	}
	if !strings.Contains(guide.Commands.ScopeBudget, "scope budget set") {
		t.Fatalf("scope budget command = %q", guide.Commands.ScopeBudget)
	}
	joined := strings.Join(guide.Instructions, "\n")
	if len(guide.Instructions) < 2 {
		t.Fatalf("expected both levers, got %d instruction(s): %q", len(guide.Instructions), joined)
	}
	if !strings.Contains(joined, "scope budget set") {
		t.Fatalf("no instruction names the budget-raise lever:\n%s", joined)
	}
	if !strings.Contains(joined, "scope rule") && !strings.Contains(joined, "scope explain") {
		t.Fatalf("no instruction names the scope-reduction lever:\n%s", joined)
	}
	if !strings.Contains(guide.Stop.SafeNextAction, "scope budget set") {
		t.Fatalf("SafeNextAction omits the raise lever: %q", guide.Stop.SafeNextAction)
	}
}
