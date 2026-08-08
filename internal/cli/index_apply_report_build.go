// 三类Apply执行后的状态分类、恢复动作和JSON编码。
package cli

import (
	"encoding/json"
	"io"
	"strings"
)

// buildReport观察执行后状态并构造相应领域报告。
func (observation *applyObservation) buildReport(
	runErr error,
	capturedOutput string,
) any {
	afterAsset, assetStateErr := readApplyFileState(
		observation.AssetPath,
	)
	afterBaseline, baselineStateErr := readApplyFileState(
		observation.BaselinePath,
	)
	afterManifest := readApplyManifestState(
		observation.Root,
		observation.RunID,
	)

	assetWritten := applyFileReplaced(
		observation.BeforeAsset,
		afterAsset,
	)

	baselineAdvanced := false
	if observation.BaselineApplicable {
		baselineAdvanced = applyFileReplaced(
			observation.BeforeBaseline,
			afterBaseline,
		)
	}

	application, applicationRecorded :=
		newApplyApplication(
			observation.BeforeManifest,
			afterManifest,
		)

	manifestApplied := false
	if afterManifest.Value != nil &&
		afterManifest.Value.AppliedAt != "" {
		beforeAppliedAt := ""
		if observation.BeforeManifest.Value != nil {
			beforeAppliedAt =
				observation.BeforeManifest.Value.AppliedAt
		}

		manifestApplied =
			afterManifest.Value.AppliedAt != beforeAppliedAt
	}

	auditRecorded :=
		applicationRecorded ||
			manifestApplied

	warnings := []string{}

	if observation.BeforeManifest.LoadErr != nil {
		warnings = append(warnings, cliMessage(
			"apply.warning.before_manifest",
			localeSafeCLIDetail(observation.BeforeManifest.LoadErr.Error()),
		))
	}

	if afterManifest.LoadErr != nil {
		warnings = append(warnings, cliMessage(
			"apply.warning.after_manifest",
			localeSafeCLIDetail(afterManifest.LoadErr.Error()),
		))
	}

	if assetStateErr != nil {
		warnings = append(warnings, cliMessage(
			"apply.warning.after_asset",
			localeSafeCLIDetail(assetStateErr.Error()),
		))
	}

	if baselineStateErr != nil &&
		observation.BaselineApplicable {
		warnings = append(warnings, cliMessage(
			"apply.warning.after_baseline",
			localeSafeCLIDetail(baselineStateErr.Error()),
		))
	}

	if assetWritten &&
		observation.BaselineApplicable &&
		!baselineAdvanced {
		warnings = append(warnings, cliMessage("apply.warning.baseline_unconfirmed"))
	}

	if assetWritten &&
		!auditRecorded {
		warnings = append(warnings, cliMessage("apply.warning.audit_unconfirmed"))
	}

	backupCreated := false
	if observation.Kind == applyKindHeader {
		backupCreated = applyBackupCreated(
			observation.BeforeBackups,
			observation.AssetPath,
		)

		if observation.BeforeAsset.Exists &&
			assetWritten &&
			!backupCreated {
			warnings = append(warnings, cliMessage("apply.warning.header_backup_unconfirmed"))
		}
	}

	diagnostics := []string{}
	if runErr != nil {
		diagnostics = splitApplyDiagnostics(
			capturedOutput,
		)
	}

	outcome := classifyApplyOutcome(
		runErr,
		assetWritten,
		warnings,
	)

	applied := 0
	rejected := 0
	rejectKinds := ""

	if applicationRecorded {
		applied = application.Applied
		rejected = application.Rejected
		rejectKinds = application.RejectKinds
	} else {
		switch {
		case assetWritten:
			applied = observation.Attempted

		case runErr != nil:
			rejected = observation.Attempted
		}
	}

	base := applyReportBase{
		Version:             applyReportVersion,
		OK:                  runErr == nil,
		Operation:           observation.Kind,
		Outcome:             outcome,
		RunID:               observation.RunID,
		PlanID:              observation.PlanID,
		Agent:               observation.Agent,
		DraftHash:           observation.DraftHash,
		ReviewHash:          observation.ReviewHash,
		AssetPath:           observation.AssetRel,
		AssetWritten:        assetWritten,
		AssetSHA256:         afterAsset.SHA256,
		BaselineApplicable:  observation.BaselineApplicable,
		BaselineAdvanced:    baselineAdvanced,
		AuditRecorded:       auditRecorded,
		ApplicationRecorded: applicationRecorded,
		ManifestApplied:     manifestApplied,
		Attempted:           observation.Attempted,
		Applied:             applied,
		Rejected:            rejected,
		RejectKinds:         rejectKinds,
		Warnings:            warnings,
		Diagnostics:         diagnostics,
	}

	if runErr != nil {
		exitCode := executionExitCode(
			runErr,
		)
		message := localizedCLIErrorMessage(runErr, exitCode)

		if isSilentReportedError(
			runErr,
		) &&
			len(diagnostics) > 0 {
			message = strings.Join(
				diagnostics,
				"\n",
			)
		}

		base.Error = &applyErrorReport{
			ExitCode: exitCode,
			Code: classifyCLIErrorCode(
				runErr,
				exitCode,
			),
			Message: message,
		}
		base.Recovery = applyRecovery(
			observation.Kind,
			assetWritten,
		)
	}

	if runErr == nil ||
		assetWritten {
		base.NextCommand = applyNextCommand(
			observation.Kind,
			observation.Agent,
		)
	}

	switch observation.Kind {
	case applyKindEntries:
		return entriesApplyJSONReport{
			applyReportBase: base,
			Paths:           observation.Paths,
		}

	case applyKindHeader:
		return headerApplyJSONReport{
			applyReportBase: base,
			BackupCreated:   backupCreated,
		}

	case applyKindCuration:
		return curationApplyJSONReport{
			applyReportBase: base,
			Paths:           observation.Paths,
			Include:         observation.Include,
			Exclude:         observation.Exclude,
		}

	default:
		return base
	}
}

// classifyApplyOutcome区分零写入拒绝、干净应用和写后审计失败。
func classifyApplyOutcome(
	runErr error,
	assetWritten bool,
	warnings []string,
) string {
	if runErr != nil {
		if assetWritten {
			return applyOutcomeAssetWrittenAuditFailed
		}
		return applyOutcomeRejected
	}

	if len(warnings) > 0 {
		return applyOutcomeAppliedWithWarnings
	}

	return applyOutcomeApplied
}

// applyRecovery返回不会诱导重复写入的恢复动作。
func applyRecovery(
	kind string,
	assetWritten bool,
) string {
	if assetWritten {
		return cliMessage("apply.recovery.asset_written")
	}

	switch kind {
	case applyKindEntries:
		return cliMessage("apply.recovery.entries")

	case applyKindHeader:
		return cliMessage("apply.recovery.header")

	case applyKindCuration:
		return cliMessage("apply.recovery.curation")

	default:
		return cliMessage("apply.recovery.default")
	}
}

// applyNextCommand返回Apply后唯一安全的下一步。
func applyNextCommand(
	kind,
	agent string,
) string {
	if kind == applyKindCuration {
		if strings.TrimSpace(
			agent,
		) == "" {
			agent = "codex"
		}

		return "aoci index agent guide --agent " +
			agent +
			" --json"
	}

	return "aoci verify --json"
}

// splitApplyDiagnostics把人读拒绝细节作为补充诊断暴露，限制最大行数。
func splitApplyDiagnostics(
	text string,
) []string {
	result := []string{}

	for _, rawLine := range strings.Split(
		text,
		"\n",
	) {
		line := strings.TrimSpace(
			rawLine,
		)
		if line == "" {
			continue
		}

		result = append(result, localeSafeCLIDetail(line))

		if len(result) == 20 {
			result = append(result, cliMessage("apply.diagnostics.truncated"))
			break
		}
	}

	return result
}

// writeApplyJSON输出单一缩进JSON业务对象。
func writeApplyJSON(
	writer io.Writer,
	value any,
) error {
	encoder := json.NewEncoder(
		writer,
	)
	encoder.SetIndent(
		"",
		"  ",
	)
	return encoder.Encode(
		value,
	)
}
