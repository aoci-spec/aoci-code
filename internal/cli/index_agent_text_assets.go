package cli

import (
	"github.com/aoci-spec/aoci-code/textassets"
)

// renderGuideText and renderGuideLines are the panic-free reading bridge of the Guide domain constructor.
// buildAgentGuide has already prechecked the exact asset set of the current branch; should an internal test bypass the entry, a read failure
// still only produces a diagnosis carrying the ID, never a panic or a fallback to a source copy.
func renderGuideText(locale string, id textassets.ID, data any) string {
	value, err := textassets.RenderScalar(locale, id, data)
	if err != nil {
		return cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))
	}

	return value
}

func renderGuideLines(locale string, id textassets.ID, data any) []string {
	value, err := textassets.RenderLines(locale, id, data)
	if err != nil {
		return []string{cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))}
	}

	return value
}
