package authoringcontract

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Under an enforced budget the R byte limit of every importance band is in
// the contract, as the write measures it; observe mode adds nothing.
func TestTheContractStatesTheEnforcedRBudget(t *testing.T) {
	enforce := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	if got := rBudgetBands(enforce); got != "C9<=542 C8<=422 C7-5<=272 C4-1<=152" {
		t.Fatalf("R bands: %s", got)
	}
	template, err := textassets.Render(textassets.DefaultLocale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
	if err != nil {
		t.Fatal(err)
	}
	meta := []byte(template)
	with, err := BuildWithBudget(meta, []string{"code"}, textassets.DefaultLocale, &enforce)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(with.Instructions, "\n"), "C9<=542") {
		t.Fatalf("the enforced R budget is not in the contract: %v", with.Instructions)
	}
	observe := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	without, err := BuildWithBudget(meta, []string{"code"}, textassets.DefaultLocale, &observe)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Build(meta, []string{"code"}, textassets.DefaultLocale)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(without.Instructions, "\n") != strings.Join(plain.Instructions, "\n") {
		t.Fatal("observe mode must not change the contract")
	}
}
