package textassets

import (
	"strings"
	"testing"
)

func TestRenderScalarAssetWithoutData(
	t *testing.T,
) {
	result, err := Render(
		LegacyLocale,
		ContractGuideBaselineFirstMessage,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "scan") {
		t.Fatalf("rendered baseline-first message does not name the scan step: %q", result)
	}
}

func TestRenderRejectsUnknownAsset(
	t *testing.T,
) {
	if _, err := Render(
		LegacyLocale,
		ID("prompts/not-found"),
		struct{}{},
	); err == nil {
		t.Fatal(
			"rendering an unknown asset must fail",
		)
	}
}
