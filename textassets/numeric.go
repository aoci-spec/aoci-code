package textassets

import "github.com/aoci-spec/aoci-code/internal/machinecontract"

// NumericTemplateData returns machine-owned numeric values with only their
// natural-language display unit supplied by the selected locale catalog.
func NumericTemplateData(locale string) (machinecontract.NumericTextValues, error) {
	values := machinecontract.NumericText()
	unit, err := Message(locale, "prompt.unit_characters")
	if err != nil {
		return machinecontract.NumericTextValues{}, err
	}
	values.SQuotaDefaultSpaced = machinecontract.FormatSQuota(" "+unit, " / ")
	values.SQuotaDefaultWithUnits = machinecontract.FormatSQuota(unit, " / ")
	return values, nil
}
