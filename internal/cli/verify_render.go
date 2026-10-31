// Verify human-readable report rendering.
//
// This file only converts verifyReport into stable text; it reads no repository, changes no asset,
// and redefines neither the four states nor the governance debt criterion.
//
// The Pending workflow hint must follow the real priority of the Agent Plan:
//   - Entries tasks such as Actionable or Stale come before Curation;
//   - prerequisite tasks such as Unbaselined come before semantic generation;
//   - only once the higher-priority tasks are cleared does Pending enter the Curation phase.
//
// So it must not claim unconditionally that the Guide's next step is Curation merely because Pending exists.
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
		writeSCoverage(&builder, report.CognitionBudget.SCoverage, report.CognitionBudget.HighImportanceEntries,
			report.CognitionBudget.HighImportanceSAbsent, report.CognitionBudget.HighImportanceSAbsentPercent)
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

// renderVerifyPendingWorkflowHint explains the next step for Pending by the Plan's real priority.
//
// Verify does not rebuild the Plan and cannot confirm Header completeness on its own, so the hint states only the priority
// the current report can determine and defers to the real phase agent guide returns.
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
