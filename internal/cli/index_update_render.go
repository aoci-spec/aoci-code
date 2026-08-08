// index update分类结果与兼容文案渲染。
package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func renderUpdateClassification(
	out io.Writer,
	mode,
	indexPath string,
	classified updateClassification,
	detected *baseline.DetectResult,
) {
	fmt.Fprintf(
		out,
		"AOCI update @ %s\n",
		time.Now().UTC().Format(time.RFC3339),
	)
	fmt.Fprintf(
		out,
		"automation.mode: %s\n",
		mode,
	)
	fmt.Fprintf(
		out,
		"curation_sha256: %s\n",
		classified.CurationSHA256,
	)
	fmt.Fprintln(out, cliMessage("update.classification.title"))

	printUpdateList(
		out,
		cliMessage("update.label.changed"),
		classified.Changed,
	)
	printUpdateList(
		out,
		cliMessage("update.label.new"),
		classified.NewFiles,
	)
	printUpdateList(
		out,
		cliMessage("update.label.included"),
		classified.IncludedNew,
	)
	printUpdateList(
		out,
		cliMessage("update.label.excluded"),
		classified.CurationExcludedNew,
	)
	printUpdateSkippedList(
		out,
		cliMessage("update.label.skipped"),
		classified.SkippedMissing,
	)
	printUpdateSkippedList(
		out,
		cliMessage("update.label.pending"),
		classified.PendingCuration,
	)
	printUpdateList(
		out,
		"stale_curation_decisions",
		classified.StaleCurationDecisions,
	)
	printUpdateList(
		out,
		cliMessage("update.label.deleted"),
		detected.Orphan,
	)
	printUpdateList(
		out,
		cliMessage("update.label.unbaselined"),
		detected.Unbaselined,
	)
	fmt.Fprintln(out, cliMessage("update.db_changed"))

	if len(classified.NewFiles) > 0 {
		fmt.Fprint(out, cliMessage(
			"update.actionable_note",
			len(classified.NewFiles),
			len(classified.IncludedNew),
		))
	}

	if classified.IndexSelfStale {
		fmt.Fprint(out, cliMessage("update.index_stale", indexPath))
	}

	if len(classified.CurationExcludedNew) > 0 {
		fmt.Fprint(out, cliMessage(
			"update.curation_excluded_note",
			len(classified.CurationExcludedNew),
		))
	}

	if len(classified.SkippedMissing) > 0 {
		fmt.Fprint(out, cliMessage("update.skipped_note", len(classified.SkippedMissing)))
	}

	if len(classified.PendingCuration) > 0 {
		fmt.Fprint(out, cliMessage("update.pending_note", len(classified.PendingCuration)))
	}

	if len(classified.StaleCurationDecisions) > 0 {
		fmt.Fprint(out, cliMessage(
			"update.stale_decision_note",
			len(classified.StaleCurationDecisions),
		))
	}

	if len(detected.Orphan) > 0 {
		fmt.Fprintln(out, cliMessage("update.orphan_note"))
	}

	if len(detected.Unbaselined) > 0 {
		fmt.Fprintln(out, cliMessage("update.unbaselined_note"))
	}
}

func renderUpdateDryRunSummary(
	out io.Writer,
	targets []string,
	classified updateClassification,
) {
	// 第一行保持R60-F.1以来的稳定人读契约，避免旧脚本与测试因新增子集失效。
	fmt.Fprint(out, cliMessage(
		"update.dry_run.summary",
		len(targets),
		len(classified.Changed),
		len(classified.NewFiles),
		len(classified.CurationExcludedNew),
		len(classified.SkippedMissing),
	))

	fmt.Fprint(out, cliMessage(
		"update.dry_run.curation",
		len(classified.IncludedNew),
		len(classified.PendingCuration),
		len(classified.StaleCurationDecisions),
	))

	fmt.Fprintln(out, cliMessage("update.dry_run.next"))
}

func printUpdateList(
	out io.Writer,
	label string,
	items []string,
) {
	fmt.Fprintf(
		out,
		"  %-34s %d\n",
		label+":",
		len(items),
	)

	for position, rel := range items {
		if position >= updateListCap {
			fmt.Fprint(out, cliMessage("update.list.truncated", len(items)))
			break
		}

		fmt.Fprintln(
			out,
			"    "+rel,
		)
	}
}

func printUpdateSkippedList(
	out io.Writer,
	label string,
	items []indexgen.SkippedMissing,
) {
	fmt.Fprintf(
		out,
		"  %-34s %d\n",
		label+":",
		len(items),
	)

	for position, item := range items {
		if position >= updateListCap {
			fmt.Fprint(out, cliMessage("update.list.truncated", len(items)))
			break
		}

		fmt.Fprintf(
			out,
			"    %s —— %s\n",
			item.Path,
			localeSafeCLIDetail(item.Reason),
		)
	}
}
