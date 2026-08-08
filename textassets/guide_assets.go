package textassets

import "strings"

const (
	ContractGuideBaseInstructions                ID = "contracts/guide/base-instructions"
	ContractGuideBaselineFirstMessage            ID = "contracts/guide/baseline-first-message"
	ContractGuideBaselineFirstInstructions       ID = "contracts/guide/baseline-first-instructions"
	ContractGuideBaselineBlockedMessage          ID = "contracts/guide/baseline-blocked-message"
	ContractGuideBaselineBlockedInstructions     ID = "contracts/guide/baseline-blocked-instructions"
	ContractGuideObserveMessage                  ID = "contracts/guide/observe-message"
	ContractGuideObserveInstructions             ID = "contracts/guide/observe-instructions"
	ContractGuideAlignedCleanMessage             ID = "contracts/guide/aligned-clean-message"
	ContractGuideAlignedCleanInstructions        ID = "contracts/guide/aligned-clean-instructions"
	ContractGuideAlignedExplainedMessage         ID = "contracts/guide/aligned-explained-message"
	ContractGuideAlignedExplainedInstructions    ID = "contracts/guide/aligned-explained-instructions"
	ContractGuideHeaderBaseInstructions          ID = "contracts/guide/header-base-instructions"
	ContractGuideHeaderAutoMessage               ID = "contracts/guide/header-auto-message"
	ContractGuideHeaderAutoInstructions          ID = "contracts/guide/header-auto-instructions"
	ContractGuideHeaderReviewMessage             ID = "contracts/guide/header-review-message"
	ContractGuideHeaderReviewInstructions        ID = "contracts/guide/header-review-instructions"
	ContractGuideHeaderLegacyMessage             ID = "contracts/guide/header-legacy-message"
	ContractGuideHeaderLegacyInstructions        ID = "contracts/guide/header-legacy-instructions"
	ContractGuideEntriesBaseInstructions         ID = "contracts/guide/entries-base-instructions"
	ContractGuideEntriesAutoMessage              ID = "contracts/guide/entries-auto-message"
	ContractGuideEntriesAutoInstructions         ID = "contracts/guide/entries-auto-instructions"
	ContractGuideEntriesReviewMessage            ID = "contracts/guide/entries-review-message"
	ContractGuideEntriesReviewInstructions       ID = "contracts/guide/entries-review-instructions"
	ContractGuideEntriesLegacyMessage            ID = "contracts/guide/entries-legacy-message"
	ContractGuideEntriesLegacyInstructions       ID = "contracts/guide/entries-legacy-instructions"
	ContractGuideCurationBaseInstructions        ID = "contracts/guide/curation-base-instructions"
	ContractGuideCurationAutoMessage             ID = "contracts/guide/curation-auto-message"
	ContractGuideCurationAutoInstructions        ID = "contracts/guide/curation-auto-instructions"
	ContractGuideCurationReviewMessage           ID = "contracts/guide/curation-review-message"
	ContractGuideCurationReviewInstructions      ID = "contracts/guide/curation-review-instructions"
	ContractGuideCurationLegacyMessage           ID = "contracts/guide/curation-legacy-message"
	ContractGuideCurationLegacyInstructions      ID = "contracts/guide/curation-legacy-instructions"
	ContractGuideIndexReviewBlockedMessage       ID = "contracts/guide/index-review-blocked-message"
	ContractGuideIndexReviewBlockedInstructions  ID = "contracts/guide/index-review-blocked-instructions"
	ContractGuideOrphanReviewBlockedMessage      ID = "contracts/guide/orphan-review-blocked-message"
	ContractGuideOrphanReviewBlockedInstructions ID = "contracts/guide/orphan-review-blocked-instructions"
)

// RenderScalar renders one scalar contract asset and removes one terminal newline.
//
// Text asset files retain their normal final newline in Git. Guide Message fields
// historically contain scalar text without that file-level delimiter, so this
// helper removes exactly one final newline while preserving all internal content.
//
// Prompt and byte-exact consumers must continue to call Render directly.
func RenderScalar(
	locale string,
	id ID,
	data any,
) (string, error) {
	text, err := Render(
		locale,
		id,
		data,
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSuffix(
		text,
		"\n",
	), nil
}

// MustRender is retained for tests and migration compatibility. Production
// callers must use RenderScalar and propagate the returned error.
func MustRender(locale string, id ID, data any) string {
	text, err := RenderScalar(locale, id, data)
	if err != nil {
		panic(err)
	}

	return text
}

// RenderLines renders an asset whose physical lines represent ordered contract
// instructions. One final newline is removed before the text is split.
func RenderLines(
	locale string,
	id ID,
	data any,
) ([]string, error) {
	text, err := Render(
		locale,
		id,
		data,
	)
	if err != nil {
		return nil, err
	}

	text = strings.TrimSuffix(
		text,
		"\n",
	)

	if text == "" {
		return []string{}, nil
	}

	return strings.Split(
		text,
		"\n",
	), nil
}

// MustRenderLines returns ordered contract lines or panics when the compiled
// catalog is inconsistent.
func MustRenderLines(
	locale string,
	id ID,
	data any,
) []string {
	lines, err := RenderLines(
		locale,
		id,
		data,
	)
	if err != nil {
		panic(err)
	}

	return lines
}
