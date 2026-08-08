package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func newCognitionMigrationCmd() *cobra.Command {
	command := &cobra.Command{Use: "migration", Short: cliMessage("cli.short.cognition_migration")}
	command.AddCommand(newMigrationSnapshotCmd(), newMigrationMappingCmd(), newMigrationPrepareCmd(), newMigrationApproveCmd(),
		newMigrationApplyCmd(), newMigrationStatusCmd(), newMigrationResumeCmd(), newMigrationRollbackCmd(), newMigrationReversalCmd())
	return command
}

func newMigrationSnapshotCmd() *cobra.Command {
	var kinds []string
	var locale, capturedAt string
	command := &cobra.Command{Use: "snapshot", Short: cliMessage("cli.short.cognition_migration_snapshot"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		if locale == "" {
			locale = textassets.ActiveLocale()
		}
		if capturedAt == "" {
			capturedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		snapshot, err := migrationapply.CaptureSnapshot(root, locale, kinds, capturedAt)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, snapshot)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.snapshot_summary", snapshot.Eligibility, snapshot.SnapshotIdentity, snapshot.EntryCount, len(snapshot.Ranges), len(snapshot.Findings), snapshot.NetworkAccessed))
		return nil
	}}
	command.Flags().StringSliceVar(&kinds, "kind", nil, cliMessage("cognition.plan.flag.kind"))
	command.Flags().StringVar(&locale, "locale", "", cliMessage("cognition.plan.flag.locale"))
	command.Flags().StringVar(&capturedAt, "captured-at", "", cliMessage("cognition.migration.flag.captured_at"))
	return command
}

func newMigrationMappingCmd() *cobra.Command {
	command := &cobra.Command{Use: "mapping", Short: cliMessage("cli.short.cognition_migration_mapping")}
	command.AddCommand(newMigrationMappingTemplateCmd(), newMigrationMappingValidateCmd())
	return command
}

func newMigrationMappingTemplateCmd() *cobra.Command {
	var snapshotFile, planFile, candidateFile string
	command := &cobra.Command{Use: "template", Short: cliMessage("cli.short.cognition_migration_mapping_template"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		snapshot, plan, candidate, err := readMigrationAuthoringInputs(snapshotFile, planFile, candidateFile)
		if err != nil {
			return migrationExitError(err)
		}
		mapping, err := migrationapply.BuildMappingTemplate(root, snapshot, plan, candidate)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, mapping)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.mapping_template_summary", len(mapping.Records), len(mapping.TargetRanges), len(mapping.AuthoringTasks)))
		return nil
	}}
	bindMigrationAuthoringFlags(command, &snapshotFile, &planFile, &candidateFile)
	return command
}

func newMigrationMappingValidateCmd() *cobra.Command {
	var snapshotFile, planFile, candidateFile, mappingFile string
	command := &cobra.Command{Use: "validate", Short: cliMessage("cli.short.cognition_migration_mapping_validate"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		snapshot, plan, candidate, err := readMigrationAuthoringInputs(snapshotFile, planFile, candidateFile)
		if err != nil {
			return migrationExitError(err)
		}
		data, err := readPlannerInput(mappingFile)
		if err != nil {
			return migrationExitError(err)
		}
		mapping, err := migrationapply.DecodeMapping(data)
		if err != nil {
			return migrationExitError(err)
		}
		validated, err := migrationapply.ValidateMapping(root, snapshot, plan, candidate, mapping)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, validated)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.mapping_summary", validated.MappingSHA256,
			validated.Coverage.LegacyEntryCoverage, validated.Coverage.LegacySemanticAtomCoverage,
			validated.Coverage.SemanticEquivalence, validated.Coverage.AmbiguousMappingCount))
		return nil
	}}
	bindMigrationAuthoringFlags(command, &snapshotFile, &planFile, &candidateFile)
	command.Flags().StringVar(&mappingFile, "mapping-file", "", cliMessage("cognition.migration.flag.mapping_file"))
	_ = command.MarkFlagRequired("mapping-file")
	return command
}

func newMigrationPrepareCmd() *cobra.Command {
	var snapshotFile, planFile, candidateFile, mappingFile, previewFile, baselineTimestamp string
	command := &cobra.Command{Use: "prepare", Short: cliMessage("cli.short.cognition_migration_prepare"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		snapshot, plan, candidate, err := readMigrationAuthoringInputs(snapshotFile, planFile, candidateFile)
		if err != nil {
			return migrationExitError(err)
		}
		mappingData, err := readPlannerInput(mappingFile)
		if err != nil {
			return migrationExitError(err)
		}
		previewData, err := readPlannerInput(previewFile)
		if err != nil {
			return migrationExitError(err)
		}
		mapping, err := migrationapply.DecodeMapping(mappingData)
		if err != nil {
			return migrationExitError(err)
		}
		preview, err := cognitionplan.DecodePreview(previewData)
		if err != nil {
			return migrationExitError(err)
		}
		if baselineTimestamp == "" {
			baselineTimestamp = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		envelope, err := migrationapply.Prepare(root, &migrationapply.ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2,
			Snapshot: *snapshot, Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview, BaselineTimestamp: baselineTimestamp})
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, envelope)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.prepare_summary", envelope.PlanID, envelope.EnvelopeDigest, len(envelope.WriteSet), envelope.RootLast, envelope.NetworkAccessed))
		return nil
	}}
	bindMigrationAuthoringFlags(command, &snapshotFile, &planFile, &candidateFile)
	command.Flags().StringVar(&mappingFile, "mapping-file", "", cliMessage("cognition.migration.flag.mapping_file"))
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("cognition.bootstrap.flag.preview_file"))
	command.Flags().StringVar(&baselineTimestamp, "baseline-timestamp", "", cliMessage("cognition.bootstrap.flag.baseline_timestamp"))
	_ = command.MarkFlagRequired("mapping-file")
	_ = command.MarkFlagRequired("preview-file")
	return command
}

func newMigrationApproveCmd() *cobra.Command {
	var envelopeFile, actor string
	command := &cobra.Command{Use: "approve", Short: cliMessage("cli.short.cognition_migration_approve"), RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := readPlannerInput(envelopeFile)
		if err != nil {
			return migrationExitError(err)
		}
		envelope, err := migrationapply.DecodeApplyEnvelope(data)
		if err != nil {
			return migrationExitError(err)
		}
		phrase := "APPROVE MIGRATION " + envelope.EnvelopeDigest
		exact := hostInteractionCommand("cognition", "migration", "approve", "--envelope-file", envelopeFile, "--actor", actor, "--json")
		if err := requireHumanPhrase(cmd, phrase, cliMessage("cognition.migration.approval_prompt", envelope.EnvelopeDigest, strings.Join(envelope.WriteSet, ", "), phrase), exact, envelope.EnvelopeDigest, fmt.Sprintf("write_set_count=%d", len(envelope.WriteSet)), "cognition migration apply"); err != nil {
			return migrationExitError(err)
		}
		approval, err := migrationapply.RecordApproval(envelope, actor, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), envelope.EnvelopeDigest)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, approval)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.approval_summary", approval.Actor, approval.ApplyEnvelopeDigest, approval.ApprovalDigest))
		return nil
	}}
	command.Flags().StringVar(&envelopeFile, "envelope-file", "", cliMessage("cognition.bootstrap.flag.envelope_file"))
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	_ = command.MarkFlagRequired("envelope-file")
	_ = command.MarkFlagRequired("actor")
	return command
}

func newMigrationApplyCmd() *cobra.Command {
	var envelopeFile, approvalFile string
	command := &cobra.Command{Use: "apply", Short: cliMessage("cli.short.cognition_migration_apply"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		envelope, approval, err := readMigrationBindings(envelopeFile, approvalFile)
		if err != nil {
			return migrationExitError(err)
		}
		result, err := migrationapply.Apply(root, envelope, approval)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationResult(cmd, result)
	}}
	bindApplyFlags(command, &envelopeFile, &approvalFile)
	return command
}

func newMigrationStatusCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "status", Short: cliMessage("cli.short.cognition_migration_status"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		status, err := migrationapply.Status(root, transaction)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationStatus(cmd, status)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	return command
}

func newMigrationResumeCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "resume", Short: cliMessage("cli.short.cognition_migration_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		result, err := migrationapply.Resume(root, transaction)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newMigrationRollbackCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "rollback", Short: cliMessage("cli.short.cognition_migration_rollback"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		phrase := "ROLLBACK MIGRATION " + transaction
		exact := hostInteractionCommand("cognition", "migration", "rollback", "--transaction", transaction, "--json")
		if err := requireHumanPhrase(cmd, phrase, cliMessage("cognition.migration.rollback_prompt", phrase), exact, transaction, "rollback_requested=true", "cognition migration status"); err != nil {
			return migrationExitError(err)
		}
		result, err := migrationapply.Rollback(root, transaction)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newMigrationReversalCmd() *cobra.Command {
	command := &cobra.Command{Use: "reversal", Short: cliMessage("cli.short.cognition_migration_reversal")}
	command.AddCommand(newMigrationReversalPrepareCmd(), newMigrationReversalApproveCmd(), newMigrationReversalApplyCmd(), newMigrationReversalStatusCmd(), newMigrationReversalResumeCmd())
	return command
}

func newMigrationReversalPrepareCmd() *cobra.Command {
	var transaction, preparedAt string
	command := &cobra.Command{Use: "prepare", Short: cliMessage("cli.short.cognition_migration_reversal_prepare"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		if preparedAt == "" {
			preparedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		plan, err := migrationapply.PrepareReversal(root, transaction, preparedAt)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, plan)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.reversal_plan_summary", plan.Eligible, plan.PlanDigest, len(plan.Risks)))
		return nil
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	command.Flags().StringVar(&preparedAt, "prepared-at", "", cliMessage("cognition.migration.flag.prepared_at"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newMigrationReversalApproveCmd() *cobra.Command {
	var planFile, actor string
	command := &cobra.Command{Use: "approve", Short: cliMessage("cli.short.cognition_migration_reversal_approve"), RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := readPlannerInput(planFile)
		if err != nil {
			return migrationExitError(err)
		}
		plan, err := migrationapply.DecodeReversalPlan(data)
		if err != nil {
			return migrationExitError(err)
		}
		phrase := "APPROVE MIGRATION REVERSAL " + plan.PlanDigest
		exact := hostInteractionCommand("cognition", "migration", "reversal", "approve", "--plan-file", planFile, "--actor", actor, "--json")
		if err := requireHumanPhrase(cmd, phrase, cliMessage("cognition.migration.reversal_approval_prompt", plan.PlanDigest, phrase), exact, plan.PlanDigest, fmt.Sprintf("write_set_count=%d", len(plan.WriteSet)), "cognition migration reversal apply"); err != nil {
			return migrationExitError(err)
		}
		approval, err := migrationapply.RecordReversalApproval(plan, actor, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), plan.PlanDigest)
		if err != nil {
			return migrationExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, approval)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.reversal_approval_summary", approval.Actor, approval.ReversalPlanDigest, approval.ApprovalDigest))
		return nil
	}}
	command.Flags().StringVar(&planFile, "plan-file", "", cliMessage("cognition.plan.flag.plan_file"))
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	_ = command.MarkFlagRequired("plan-file")
	_ = command.MarkFlagRequired("actor")
	return command
}

func newMigrationReversalApplyCmd() *cobra.Command {
	var planFile, approvalFile string
	command := &cobra.Command{Use: "apply", Short: cliMessage("cli.short.cognition_migration_reversal_apply"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		planData, err := readPlannerInput(planFile)
		if err != nil {
			return migrationExitError(err)
		}
		approvalData, err := readPlannerInput(approvalFile)
		if err != nil {
			return migrationExitError(err)
		}
		plan, err := migrationapply.DecodeReversalPlan(planData)
		if err != nil {
			return migrationExitError(err)
		}
		approval, err := migrationapply.DecodeReversalApproval(approvalData)
		if err != nil {
			return migrationExitError(err)
		}
		result, err := migrationapply.ApplyReversal(root, plan, approval)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationResult(cmd, result)
	}}
	command.Flags().StringVar(&planFile, "plan-file", "", cliMessage("cognition.plan.flag.plan_file"))
	command.Flags().StringVar(&approvalFile, "approval-file", "", cliMessage("cognition.bootstrap.flag.approval_file"))
	_ = command.MarkFlagRequired("plan-file")
	_ = command.MarkFlagRequired("approval-file")
	return command
}

func newMigrationReversalStatusCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "status", Short: cliMessage("cli.short.cognition_migration_reversal_status"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		status, err := migrationapply.ReversalStatus(root, transaction)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationStatus(cmd, status)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	return command
}

func newMigrationReversalResumeCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "resume", Short: cliMessage("cli.short.cognition_migration_reversal_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return migrationExitError(err)
		}
		result, err := migrationapply.ResumeReversal(root, transaction)
		if err != nil {
			return migrationExitError(err)
		}
		return writeMigrationResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func bindMigrationAuthoringFlags(command *cobra.Command, snapshotFile, planFile, candidateFile *string) {
	command.Flags().StringVar(snapshotFile, "snapshot-file", "", cliMessage("cognition.migration.flag.snapshot_file"))
	command.Flags().StringVar(planFile, "plan-file", "", cliMessage("cognition.plan.flag.plan_file"))
	command.Flags().StringVar(candidateFile, "candidate-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	_ = command.MarkFlagRequired("snapshot-file")
	_ = command.MarkFlagRequired("plan-file")
	_ = command.MarkFlagRequired("candidate-file")
}

func bindApplyFlags(command *cobra.Command, envelopeFile, approvalFile *string) {
	command.Flags().StringVar(envelopeFile, "envelope-file", "", cliMessage("cognition.bootstrap.flag.envelope_file"))
	command.Flags().StringVar(approvalFile, "approval-file", "", cliMessage("cognition.bootstrap.flag.approval_file"))
	_ = command.MarkFlagRequired("envelope-file")
	_ = command.MarkFlagRequired("approval-file")
}

func readMigrationAuthoringInputs(snapshotFile, planFile, candidateFile string) (*migrationapply.LegacySnapshot, *cognitionplan.Plan, *cognitionplan.LayoutCandidate, error) {
	snapshotData, err := readPlannerInput(snapshotFile)
	if err != nil {
		return nil, nil, nil, err
	}
	planData, err := readPlannerInput(planFile)
	if err != nil {
		return nil, nil, nil, err
	}
	candidateData, err := readPlannerInput(candidateFile)
	if err != nil {
		return nil, nil, nil, err
	}
	snapshot, err := migrationapply.DecodeLegacySnapshot(snapshotData)
	if err != nil {
		return nil, nil, nil, err
	}
	plan, err := cognitionplan.DecodePlan(planData)
	if err != nil {
		return nil, nil, nil, err
	}
	candidate, err := cognitionplan.DecodeCandidate(candidateData)
	return snapshot, plan, candidate, err
}

func readMigrationBindings(envelopeFile, approvalFile string) (*migrationapply.ApplyEnvelope, *migrationapply.Approval, error) {
	envelopeData, err := readPlannerInput(envelopeFile)
	if err != nil {
		return nil, nil, err
	}
	approvalData, err := readPlannerInput(approvalFile)
	if err != nil {
		return nil, nil, err
	}
	envelope, err := migrationapply.DecodeApplyEnvelope(envelopeData)
	if err != nil {
		return nil, nil, err
	}
	approval, err := migrationapply.DecodeApproval(approvalData)
	return envelope, approval, err
}

func writeMigrationResult(cmd *cobra.Command, result *migrationapply.ApplyResult) error {
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.result_summary", result.TransactionID, result.Status, result.ActiveLayout, result.FormalComplete, result.NetworkAccessed, result.NextAction))
	return nil
}

func writeMigrationStatus(cmd *cobra.Command, status *migrationapply.TransactionStatus) error {
	if flagJSON {
		return writePlannerJSON(cmd, status)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.migration.status_summary", status.TransactionID, status.Status, status.ActiveLayout, status.FormalComplete, status.ThirdPartyConflict, strings.Join(status.NextActions, ",")))
	return nil
}

func migrationExitError(err error) error {
	var interaction *hostInteractionError
	if errors.As(err, &interaction) {
		return &ExitError{Code: ExitInvalid, MachineCode: "interaction_required", Details: interaction.State,
			Msg: cliMessage("cognition.migration.error", textassets.DiagnosticFacts(err.Error()))}
	}
	var mismatch *migrationapply.ReplayMismatchError
	if errors.As(err, &mismatch) {
		return &ExitError{Code: ExitInvalid, MachineCode: mismatch.Code, Details: mismatch.Report,
			Msg: cliMessage("cognition.migration.error", textassets.DiagnosticFacts(err.Error()))}
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "cognition_migration_invalid", Msg: cliMessage("cognition.migration.error", textassets.DiagnosticFacts(err.Error()))}
}
