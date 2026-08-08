// Package textassets renders embedded natural-language templates by stable ID.
package textassets

import (
	"fmt"
	"strings"
	"text/template"
)

const (
	// PromptHeaderUser injects repository facts into the Header drafting request.
	PromptHeaderUser ID = "prompts/header-user"

	// PromptEntryUser injects one target file and its evidence into an Entry request.
	PromptEntryUser ID = "prompts/entry-user"
)

// Render executes one embedded asset as a strict text/template.
//
// The asset source is loaded through the same locale and manifest checks as Load.
// Missing template fields are treated as compiled-asset defects rather than being
// silently rendered as zero values.
func Render(
	locale string,
	id ID,
	data any,
) (string, error) {
	source, err := Load(
		locale,
		id,
	)
	if err != nil {
		return "", err
	}

	compiled, err := template.New(
		string(id),
	).Option(
		"missingkey=error",
	).Parse(
		source,
	)
	if err != nil {
		return "", fmt.Errorf(
			"parse text asset template %q: %w",
			id,
			err,
		)
	}

	var builder strings.Builder

	if err := compiled.Execute(
		&builder,
		data,
	); err != nil {
		return "", fmt.Errorf(
			"render text asset template %q: %w",
			id,
			err,
		)
	}

	return builder.String(), nil
}
