// Entries Auto的Diff审计与P-23授权校验。
//
// 本文件只处理审阅事实：
//   - 使用Check返回的同一份草稿内存快照构造Diff；
//   - 追加Diff Review与Ledger；
//   - 验证持久化Check、Diff和当前草稿摘要一致。
//
// Endpoint Auto与Host-Agent Auto必须复用同一实现，仅通过source区分审计来源。
// 本文件不读取源码正文、不调用模型、不修改正式索引或Baseline。
package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// appendPersistedEntriesAutoDiffReview使用Check返回的同一份内存快照
// 构造Diff审计，并按调用方传入的真实来源写入Ledger。
//
// 只有Diff报告完整覆盖全部已通过目标时，才追加Review和Ledger。
func appendPersistedEntriesAutoDiffReview(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	result *entriesCheckResult,
	source string,
	out io.Writer,
) (entriesDiffReport, error) {
	emptyReport := entriesDiffReport{}

	if result == nil ||
		result.Manifest == nil ||
		result.Snapshot == nil {
		return emptyReport, fmt.Errorf("%s", cliMessage("automation.review.diff_input_incomplete"))
	}

	if cfg == nil ||
		doc == nil {
		return emptyReport, fmt.Errorf("%s", cliMessage("automation.review.diff_state_incomplete"))
	}

	if source != ledger.SourceAgent &&
		source != ledger.SourceCLIAI {
		return emptyReport, fmt.Errorf("%s", cliMessage(
			"automation.review.diff_source_invalid",
			source,
		))
	}

	if out == nil {
		out = io.Discard
	}

	start := time.Now()

	report := buildEntriesDiffReport(
		result.RunID,
		result.Manifest,
		result.Snapshot,
		doc,
	)

	if report.DraftHash == "" ||
		report.DraftHash != result.Snapshot.Hash {
		return emptyReport, fmt.Errorf("%s", cliMessage("automation.review.diff_hash_drift"))
	}

	if report.Reviewed != result.Review.Passed ||
		report.Skipped != 0 ||
		report.Reviewed != len(result.Manifest.Entries) {
		return emptyReport, fmt.Errorf("%s", cliMessage(
			"automation.review.diff_coverage_incomplete",
			len(result.Manifest.Entries),
			result.Review.Passed,
			report.Reviewed,
			report.Skipped,
		))
	}

	if err := draft.AppendReview(
		repoRoot,
		result.RunID,
		draft.ReviewRecord{
			Action:     draft.ReviewActionDiff,
			DraftHash:  report.DraftHash,
			PathsCount: report.Total,
			Passed:     report.Reviewed,
			Skipped:    report.Skipped,
		},
	); err != nil {
		return emptyReport, fmt.Errorf("%s", cliMessage(
			"automation.review.diff_audit_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:         "entries_diff",
			Source:     source,
			PathsCount: report.Total,
			DurationMs: time.Since(start).Milliseconds(),
			DraftRunID: result.RunID,
		},
	)

	fmt.Fprint(out, cliMessage(
		"automation.review.diff_complete",
		report.Reviewed,
		shortDraftHash(report.DraftHash),
	))

	return report, nil
}

// validatePersistedAutoReview验证持久化Check与Diff共同授权当前Apply。
func validatePersistedAutoReview(
	repoRoot string,
	result *entriesCheckResult,
) error {
	if result == nil ||
		result.Snapshot == nil {
		return fmt.Errorf("%s", cliMessage("automation.review.persisted_input_incomplete"))
	}

	persisted, err := draft.LoadManifest(
		repoRoot,
		result.RunID,
	)
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"automation.review.reload_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	if len(persisted.Reviews) < 2 {
		return fmt.Errorf("%s", cliMessage("automation.review.incomplete"))
	}

	last := persisted.Reviews[len(persisted.Reviews)-1]

	if last.Action != draft.ReviewActionDiff ||
		last.DraftHash != result.Snapshot.Hash ||
		last.Passed != result.Review.Passed ||
		last.Skipped != 0 {
		return fmt.Errorf("%s", cliMessage(
			"automation.review.latest_mismatch",
			last.Action,
			shortDraftHash(last.DraftHash),
			last.Passed,
			last.Skipped,
		))
	}

	foundCleanCheck := false

	for position := len(persisted.Reviews) - 2; position >= 0; position-- {
		review := persisted.Reviews[position]

		if review.Action != draft.ReviewActionCheck {
			continue
		}

		if review.DraftHash == result.Snapshot.Hash &&
			review.Rejected == 0 &&
			review.Skipped == 0 &&
			review.Passed == result.Review.Passed {
			foundCleanCheck = true
		}

		break
	}

	if !foundCleanCheck {
		return fmt.Errorf("%s", cliMessage("automation.review.clean_check_missing"))
	}

	warning, guardErr := guardReviewedDraftHash(
		persisted,
		result.Snapshot.Hash,
	)
	if guardErr != nil {
		return guardErr
	}

	if warning != "" {
		return fmt.Errorf("%s", cliMessage(
			"automation.review.legacy_rejected",
			localeSafeCLIDetail(warning),
		))
	}

	return nil
}
