// aoci_report: the Legacy follow-up record is retired (0.2.0), so the tool
// validates its arguments and answers each layout's read-only refusal.
package mcptools

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

type reportIn struct {
	Path string `json:"path"`
	Note string `json:"note"`
}

func handleReport(
	root string,
	in reportIn,
) *mcp.CallToolResult {
	if fail := validateWriteMessages(requiredReportMessages); fail != nil {
		return failResult(fail)
	}
	if _, err := afs.NormalizeRelPath(
		in.Path,
	); err != nil {
		return errResult(
			errPathUnsafe,
			writeMessage(
				"report.path_rejected",
				in.Path,
				localizeRelPathError(err),
			),
			writeMessage("report.hint.relative_path"),
		)
	}

	note := strings.TrimSpace(
		strings.ReplaceAll(
			in.Note,
			"\n",
			" ",
		),
	)

	if note == "" {
		return errResult(
			errBadArgs,
			writeMessage("report.note_empty"),
			"",
		)
	}

	// Since 0.2.0 no layout records a report. Volumes v1 keeps answering
	// volume_read_only exactly as the Legacy loader did; a Legacy layout, or an
	// index that cannot be loaded at all, answers the read-only token.
	if loaded, loadFail := loadCognitionCtx(root); loadFail == nil && loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return failResult(&Fail{Code: errVolumeReadOnly, Msg: mcpMessage("mcp.error.volume_read_only"), Hint: mcpMessage("mcp.error.volume_read_only_hint")})
	}
	return failResult(legacyReadOnlyFail())
}
