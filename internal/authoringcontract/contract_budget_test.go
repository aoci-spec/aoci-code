package authoringcontract

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Under an enforced budget the effective R rune and byte limits of every
// importance band are in the contract, derived from cognitionbudget's shared
// projection; observe mode adds nothing.
func TestTheContractStatesTheEnforcedRBudget(t *testing.T) {
	enforce := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	rBands := cognitionbudget.EffectiveRFieldBands(enforce.R)
	wantEN := "C9<=360 runes/542 bytes, C8<=360 runes/422 bytes, C7-5<=272 runes/272 bytes, C4-1<=152 runes/152 bytes"
	if got := formatEffectiveRBands(rBands, textassets.DefaultLocale); got != wantEN {
		t.Fatalf("en-US R bands = %q, want %q", got, wantEN)
	}
	wantZH := "C9<=360字/542字节、C8<=360字/422字节、C7-5<=272字/272字节、C4-1<=152字/152字节"
	if got := formatEffectiveRBands(rBands, textassets.LegacyLocale); got != wantZH {
		t.Fatalf("zh-CN R bands = %q, want %q", got, wantZH)
	}
	for locale, want := range map[string]string{
		textassets.DefaultLocale: wantEN,
		textassets.LegacyLocale:  wantZH,
	} {
		template, err := textassets.Render(locale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
		if err != nil {
			t.Fatal(err)
		}
		meta := []byte(template)
		with, err := BuildWithBudget(meta, []string{"code"}, locale, &enforce)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(with.Instructions, "\n"), want) {
			t.Fatalf("%s enforced R budget is missing %q: %v", locale, want, with.Instructions)
		}
		observe := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
		without, err := BuildWithBudget(meta, []string{"code"}, locale, &observe)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := Build(meta, []string{"code"}, locale)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(without.Instructions, "\n") != strings.Join(plain.Instructions, "\n") {
			t.Fatalf("%s observe mode must not change the contract", locale)
		}
	}
}
