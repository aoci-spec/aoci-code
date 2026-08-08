// aoci_report待办登记能力。
//
// 对索引状态没有把握时只追加结构化待办，不修改正式索引或Baseline。
package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

type reportIn struct {
	Path string `json:"path"`
	Note string `json:"note"`
}

type reportRec struct {
	Ts   string `json:"ts"`
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
	start := time.Now()

	rel, err := afs.NormalizeRelPath(
		in.Path,
	)
	if err != nil {
		return errResult(
			errPathUnsafe,
			writeMessage(
				"report.path_rejected",
				in.Path,
				localeSafeWriteDetail(err.Error()),
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

	rc, fail := loadRepoCtx(
		root,
	)
	if fail != nil {
		return failResult(
			fail,
		)
	}

	record := reportRec{
		Ts: time.Now().
			UTC().
			Format(time.RFC3339),
		Path: rel,
		Note: note,
	}

	data, _ := json.Marshal(
		record,
	)

	if err := appendLine(
		rc.paths.ReportsPath,
		data,
	); err != nil {
		return errResult(
			errInternal,
			writeMessage(
				"report.write_failed",
				localeSafeWriteDetail(err.Error()),
			),
			"",
		)
	}

	pending := countLines(
		rc.paths.ReportsPath,
	)

	ledger.Append(
		root,
		rc.cfg.LedgerEnabled,
		ledger.Event{
			Op:         "report",
			PathsCount: 1,
			DurationMs: time.Since(
				start,
			).Milliseconds(),
			Source: ledger.SourceAgent,
		},
	)

	return textResult(
		writeMessage(
			"report.recorded",
			rel,
			note,
			pending,
		),
	)
}

func appendLine(
	targetPath string,
	line []byte,
) error {
	if err := os.MkdirAll(
		filepath.Dir(targetPath),
		0o755,
	); err != nil {
		return err
	}

	file, err := os.OpenFile(
		targetPath,
		os.O_APPEND|
			os.O_CREATE|
			os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = file.Write(
		append(
			line,
			'\n',
		),
	)

	return err
}

func countLines(
	targetPath string,
) int {
	data, err := os.ReadFile(
		targetPath,
	)
	if err != nil {
		return 0
	}

	count := 0

	for _, line := range strings.Split(
		string(data),
		"\n",
	) {
		if strings.TrimSpace(
			line,
		) != "" {
			count++
		}
	}

	return count
}
