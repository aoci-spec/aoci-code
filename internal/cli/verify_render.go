// Verify人读报告渲染。
//
// 本文件只把verifyReport转换为稳定文本，不读取仓库、不修改资产，
// 也不重新定义四态或治理债务判据。
//
// Pending工作流提示必须服从Agent Plan真实优先级：
//   - Actionable或Stale等Entries任务先于Curation；
//   - Unbaselined等前置任务先于语义生成；
//   - 只有更高优先级任务清零后，Pending才进入Curation阶段。
//
// 因此不得仅因Pending存在就无条件声称Guide下一步一定是Curation。
package cli

import (
	"fmt"
	"strings"
)

func renderVerifyHuman(
	report *verifyReport,
) string {
	var builder strings.Builder

	fmt.Fprintln(&builder, cliMessage("verify.start", report.GeneratedAt))

	fmt.Fprintln(
		&builder,
		cliMessage("verify.repository", report.Root, report.IndexEntries, report.DiskFiles, report.BaselineExists),
	)

	if report.CurationSHA256 != "" {
		fmt.Fprintln(
			&builder,
			cliMessage("verify.curation_digest", report.CurationSHA256),
		)
	}

	section := func(
		messageKey string,
		items []string,
	) {
		fmt.Fprintln(&builder, cliMessage(messageKey, len(items)))

		for _, item := range items {
			fmt.Fprintf(
				&builder,
				"  %s\n",
				item,
			)
		}
	}

	section(
		"verify.section_missing",
		report.Result.Missing,
	)

	section(
		"verify.section_actionable",
		report.ActionableMissing,
	)

	section(
		"verify.section_included",
		report.IncludedMissing,
	)

	section(
		"verify.section_excluded",
		report.CurationExcludedMissing,
	)

	fmt.Fprintln(&builder, cliMessage("verify.section_skipped", len(report.SkippedMissing)))

	for _, item := range report.SkippedMissing {
		fmt.Fprintln(&builder, cliMessage("verify.skipped_item", item.Path, item.Reason))
	}

	fmt.Fprintln(&builder, cliMessage("verify.section_pending", len(report.PendingCurationMissing)))

	for _, item := range report.PendingCurationMissing {
		fmt.Fprintln(&builder, cliMessage("verify.pending_item", item.Path, item.ProfileReason, item.SourceSHA256))
	}

	section(
		"verify.section_stale_curation",
		report.StaleCurationDecisions,
	)

	section(
		"verify.section_orphan",
		report.Result.Orphan,
	)

	section(
		"verify.section_stale",
		report.Result.Stale,
	)

	section(
		"verify.section_line_endings",
		report.Result.LineEndingOnly,
	)

	section(
		"verify.section_unbaselined",
		report.Result.Unbaselined,
	)

	section("verify.section_observed_new", report.Result.ObservedNew)
	section("verify.section_observed_changed", report.Result.ObservedChanged)
	section("verify.section_observed_removed", report.Result.ObservedRemoved)

	if report.ManagedScope.ScopeChangeRequired {
		fmt.Fprintln(&builder, cliMessage("verify.scope_change_required", report.ManagedScope.PolicyIdentity, report.ManagedScope.ActivePolicyIdentity))
	} else if report.ManagedScope.PolicyIdentity != "" {
		fmt.Fprintln(&builder, cliMessage("verify.scope_aligned", report.ManagedScope.PolicyIdentity,
			report.ManagedScope.IndexCount, report.ManagedScope.ObserveCount, report.ManagedScope.ExcludeCount))
	}
	if report.CognitionBudget != nil {
		fmt.Fprintln(&builder, cliMessage("verify.cognition_budget", report.CognitionBudget.WholeIndexTokens,
			report.CognitionBudget.TargetTokens, report.CognitionBudget.WarningTokens, report.CognitionBudget.MaxTokens,
			report.CognitionBudget.Mode, report.CognitionBudget.Status, len(report.CognitionBudget.Violations)))
	}

	if len(report.FormatWarnings) > 0 {
		section(
			"verify.section_format",
			report.FormatWarnings,
		)
	}

	rawTotal := verifyRawDriftCount(report)
	unresolvedTotal :=
		verifyUnresolvedDriftCount(report)

	if unresolvedTotal == 0 {
		if rawTotal == 0 {
			if len(
				report.Result.LineEndingOnly,
			) > 0 {
				fmt.Fprintln(&builder, cliMessage("verify.aligned_line_endings", len(report.Result.LineEndingOnly)))

				return builder.String()
			}

			fmt.Fprintln(&builder, cliMessage("verify.aligned"))

			return builder.String()
		}

		fmt.Fprintln(&builder, cliMessage(
			"verify.governed",
			rawTotal,
			len(report.ActionableMissing),
			len(report.IncludedMissing),
			len(report.CurationExcludedMissing),
			len(report.SkippedMissing),
			len(report.PendingCurationMissing),
		))

		fmt.Fprintln(&builder, cliMessage("verify.governed_hint"))

		return builder.String()
	}

	fmt.Fprintln(&builder, cliMessage(
		"verify.drift",
		unresolvedTotal,
		rawTotal,
		len(report.ActionableMissing),
		len(report.IncludedMissing),
		len(report.CurationExcludedMissing),
		len(report.SkippedMissing),
		len(report.PendingCurationMissing),
	))

	builder.WriteString(
		renderVerifyPendingWorkflowHint(report),
	)

	return builder.String()
}

// renderVerifyPendingWorkflowHint根据Plan真实优先级解释Pending下一步。
//
// Verify不重新构建Plan，也无法单独确认Header完整性，因此提示只陈述能够由
// 当前报告确定的优先级，并要求以agent guide返回的实际阶段为准。
func renderVerifyPendingWorkflowHint(
	report *verifyReport,
) string {
	if report == nil ||
		len(report.PendingCurationMissing) == 0 {
		return ""
	}

	if len(report.ActionableMissing) > 0 {
		return cliMessage("verify.pending_after_entries") + "\n"
	}

	if report.Result != nil &&
		len(report.Result.Unbaselined) > 0 {
		return cliMessage("verify.pending_after_baseline") + "\n"
	}

	if report.Result != nil &&
		len(report.Result.Stale) > 0 {
		return cliMessage("verify.pending_after_stale") + "\n"
	}

	return cliMessage("verify.pending_guide") + "\n"
}
