package textassets

import "strings"

const (
	ContractGuideBaselineFirstMessage      ID = "contracts/guide/baseline-first-message"
	ContractGuideBaselineFirstInstructions ID = "contracts/guide/baseline-first-instructions"
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
