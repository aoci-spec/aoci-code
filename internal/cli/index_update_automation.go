// R65 Entries Automation编排入口。
//
// Endpoint生成完成后，本文件按automation.mode分流：
//   - legacy: 只保留草稿并提示人工Check、Diff和Apply；
//   - review: 复用机器Check并停在人工Diff与Apply之前；
//   - auto:   复用Entries Auto共享内核完成Check、Diff审计和原子Apply；
//   - off:    不应进入生成后编排。
//
// R65以后，Endpoint Auto与Host-Agent Auto不得分别维护两套Check、Diff、
// P-23、原子Apply或Application审计逻辑。两者只通过Ledger source区分来源。
//
// 本文件不复制格式、字典、R关系、S配额、E档位、索引编辑或CAS判据。
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/workflow"
)

// finishIndexUpdateAutomation按团队automation.mode分流生成后的治理动作。
func finishIndexUpdateAutomation(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	draftResult *workflow.EntriesDraftResult,
	targetCount int,
	out io.Writer,
) error {
	if out == nil {
		out = io.Discard
	}

	switch cfg.EffectiveAutomationMode() {
	case config.AutomationModeLegacy:
		fmt.Fprintln(out, cliMessage("automation.legacy.next"))
		return nil

	case config.AutomationModeReview:
		return finishUpdateReviewMode(
			repoRoot,
			cfg,
			doc,
			draftResult,
			out,
		)

	case config.AutomationModeAuto:
		return finishUpdateAutoMode(
			repoRoot,
			cfg,
			doc,
			draftResult,
			targetCount,
			out,
		)

	case config.AutomationModeOff:
		return &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("automation.off.unexpected")),
		}

	default:
		return &ExitError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"automation.mode.unknown",
				cfg.EffectiveAutomationMode(),
			)),
		}
	}
}

// finishUpdateReviewMode执行共用机器预检，并停在人工Diff与Apply之前。
func finishUpdateReviewMode(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	draftResult *workflow.EntriesDraftResult,
	out io.Writer,
) error {
	if draftResult == nil ||
		draftResult.RunID == "" {
		return &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("automation.review.draft_missing")),
		}
	}

	fmt.Fprintln(out, cliMessage("automation.review.start"))

	checkResult, err := runEntriesCheckCore(
		repoRoot,
		draftResult.RunID,
		cfg,
		doc,
		out,
		ledger.SourceCLIAI,
	)
	if err != nil {
		return err
	}
	if checkResult == nil {
		return &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("automation.review.result_missing")),
		}
	}

	if checkResult.Review.Rejected > 0 ||
		checkResult.Review.Skipped > 0 {
		fmt.Fprintln(out, cliMessage("automation.review.stopped"))
		return &ExitError{
			Code: ExitInvalid,
			Msg:  "",
		}
	}

	fmt.Fprintln(out, cliMessage("automation.review.ready"))

	return nil
}

// finishUpdateAutoMode先锁定Endpoint generation完整性，再进入共享Auto收口。
//
// 共享内核统一负责Check、Diff、P-23、原子Apply和Application审计；
// 本函数不得重新实现其中任何判据。
func finishUpdateAutoMode(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	draftResult *workflow.EntriesDraftResult,
	targetCount int,
	out io.Writer,
) error {
	if err := validateAutoGeneration(
		draftResult,
		targetCount,
	); err != nil {
		fmt.Fprintln(out, cliMessage("automation.auto.generation_stopped"))
		return &ExitError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	fmt.Fprintln(out, cliMessage("automation.auto.start"))

	finalizeResult, err := runEntriesAutoFinalize(
		repoRoot,
		cfg,
		doc,
		draftResult.RunID,
		targetCount,
		ledger.SourceCLIAI,
		out,
	)
	if err != nil {
		return err
	}

	if finalizeResult == nil ||
		finalizeResult.Status !=
			entriesAutoStatusApplied ||
		(!finalizeResult.AssetWritten && finalizeResult.Recovered == 0) ||
		!finalizeResult.AuditRecorded {
		return &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"automation.auto.incomplete_success",
				finalizeResult,
			)),
		}
	}

	return nil
}

// validateAutoGeneration锁定Endpoint生成结果必须完整覆盖本批全部目标。
func validateAutoGeneration(
	result *workflow.EntriesDraftResult,
	targetCount int,
) error {
	if result == nil {
		return fmt.Errorf("%s", cliMessage("automation.generation.nil"))
	}
	if targetCount <= 0 {
		return fmt.Errorf("%s", cliMessage("automation.generation.target_invalid", targetCount))
	}

	statusCount := len(result.Statuses)
	totalByState := result.Drafted +
		result.Warned +
		result.Failed +
		result.Skipped

	if statusCount != targetCount ||
		totalByState != statusCount ||
		result.Drafted+result.Warned != targetCount ||
		result.Failed != 0 ||
		result.Skipped != 0 {
		return fmt.Errorf("%s", cliMessage(
			"automation.generation.incomplete",
			targetCount,
			statusCount,
			result.Drafted,
			result.Warned,
			result.Failed,
			result.Skipped,
		))
	}

	return nil
}

// atomicItemsFromReviewedSnapshot从Check使用的同一内存快照构造原子输入。
//
// 任一非drafted或warned状态都拒绝，防止Auto静默跳过目标后部分应用。
func atomicItemsFromReviewedSnapshot(
	result *entriesCheckResult,
) ([]mcptools.AtomicUpdateItem, error) {
	if result == nil ||
		result.Manifest == nil ||
		result.Snapshot == nil {
		return nil, fmt.Errorf("%s", cliMessage("automation.atomic.snapshot_missing"))
	}

	items := make(
		[]mcptools.AtomicUpdateItem,
		0,
		len(result.Manifest.Entries),
	)

	for _, status := range result.Manifest.Entries {
		if status.Status != "drafted" &&
			status.Status != "warned" {
			return nil, fmt.Errorf("%s", cliMessage(
				"automation.atomic.status_invalid",
				status.Path,
				status.Status,
			))
		}
		sourceSHA256 := strings.ToLower(strings.TrimSpace(status.SourceSHA256))
		if !validEntrySourceSHA256(sourceSHA256) {
			return nil, fmt.Errorf("%s", cliMessage(
				"automation.atomic.source_missing",
				status.Path,
			))
		}

		line, err := result.Snapshot.line(
			status.Path,
		)
		if err != nil {
			return nil, err
		}

		items = append(
			items,
			mcptools.AtomicUpdateItem{
				Path:         status.Path,
				NewEntry:     line,
				SourceSHA256: sourceSHA256,
			},
		)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("%s", cliMessage("automation.atomic.empty"))
	}

	return items, nil
}

func validEntrySourceSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

// autoRejectKind把底层结构化失败压缩为Application审计原因。
func autoRejectKind(
	code string,
) string {
	switch code {
	case "write_conflict":
		return "conflict"

	case "bad_args",
		"index_invalid",
		"path_unsafe":
		return "format"

	default:
		return "other"
	}
}

// autoFailExitCode复用CLI与MCP共享失败码到进程退出码的映射。
func autoFailExitCode(
	code string,
) int {
	return exitCodeForFail(
		code,
	)
}

// formatAutoApplyFail保留底层失败分类和恢复建议。
func formatAutoApplyFail(
	failure *mcptools.Fail,
) error {
	if failure == nil {
		return fmt.Errorf("%s", cliMessage("automation.apply.unknown"))
	}

	if failure.Hint == "" {
		return fmt.Errorf("%s", cliMessage(
			"automation.apply.failed",
			failure.Code,
			localeSafeCLIDetail(failure.Msg),
		))
	}

	return fmt.Errorf("%s", cliMessage(
		"automation.apply.failed_hint",
		failure.Code,
		localeSafeCLIDetail(failure.Msg),
		localeSafeCLIDetail(failure.Hint),
	))
}
