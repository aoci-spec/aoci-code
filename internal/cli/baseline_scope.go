package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/scoperefresh"
	"github.com/spf13/cobra"
)

func init() {
	registerCommand(newBaselineCmd())
}

func newBaselineCmd() *cobra.Command {
	command := &cobra.Command{Use: "baseline", Short: cliMessage("cli.short.baseline")}
	command.AddCommand(newBaselineScopeCmd())
	return command
}

func newBaselineScopeCmd() *cobra.Command {
	command := &cobra.Command{Use: "scope", Short: cliMessage("cli.short.baseline_scope")}
	command.AddCommand(newBaselineScopePlanCmd(false), newBaselineScopePlanCmd(true), newBaselineScopeApproveCmd(),
		newBaselineScopeApplyCmd(), newBaselineScopeStatusCmd(), newBaselineScopeResumeCmd())
	return command
}

func newBaselineScopePlanCmd(previewMode bool) *cobra.Command {
	var timestamp string
	use := "plan"
	short := cliMessage("cli.short.baseline_scope_plan")
	if previewMode {
		use = "preview"
		short = cliMessage("cli.short.baseline_scope_preview")
	}
	command := &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return scopeExitError(err)
		}
		if timestamp == "" {
			timestamp = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		preview, err := scoperefresh.Build(root, timestamp)
		if err != nil {
			return scopeExitError(err)
		}
		if flagJSON {
			if previewMode {
				return writePlannerJSON(cmd, preview)
			}
			return writePlannerJSON(cmd, preview.Plan)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("baseline.scope.plan_summary", preview.Plan.PlanID, len(preview.Plan.Added), len(preview.Plan.Removed), len(preview.Plan.SourceDrift), preview.Plan.InteractionRequired))
		if previewMode {
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("baseline.scope.preview_summary", preview.PreviewID, preview.BaselinePostimageSHA256))
		}
		return nil
	}}
	command.Flags().StringVar(&timestamp, "baseline-timestamp", "", cliMessage("cognition.bootstrap.flag.baseline_timestamp"))
	return command
}

func newBaselineScopeApproveCmd() *cobra.Command {
	var previewFile, actor, outFile string
	command := &cobra.Command{Use: "approve", Short: cliMessage("cli.short.baseline_scope_approve"), RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validatePlannerOutput(outFile); err != nil {
			return scopeExitError(err)
		}
		data, err := readPlannerInput(previewFile)
		if err != nil {
			return scopeExitError(err)
		}
		preview, err := scoperefresh.DecodePreview(data)
		if err != nil {
			return scopeExitError(err)
		}
		if !preview.Plan.InteractionRequired {
			return scopeExitError(fmt.Errorf("baseline_scope_approval_not_required"))
		}
		exact := hostInteractionCommand("baseline", "scope", "approve", "--preview-file", previewFile, "--actor", actor, "--json")
		if err := requireHumanPhrase(cmd, preview.Plan.ConfirmationPhrase, cliMessage("baseline.scope.approval_prompt", preview.Plan.PlanID, preview.Plan.ConfirmationPhrase), exact, preview.Plan.PlanID, fmt.Sprintf("removed_count=%d", len(preview.Plan.Removed)), "baseline scope apply"); err != nil {
			return scopeExitError(err)
		}
		approval, err := scoperefresh.NewApproval(preview, actor, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
		if err != nil {
			return scopeExitError(err)
		}
		return writePlannerArtifact(cmd, approval, outFile)
	}}
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("baseline.scope.flag.preview_file"))
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	command.Flags().StringVar(&outFile, "out-file", "", cliMessage("baseline.scope.flag.out_file"))
	_ = command.MarkFlagRequired("preview-file")
	_ = command.MarkFlagRequired("actor")
	return command
}

func newBaselineScopeApplyCmd() *cobra.Command {
	var previewFile, approvalFile string
	command := &cobra.Command{Use: "apply", Short: cliMessage("cli.short.baseline_scope_apply"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return scopeExitError(err)
		}
		data, err := readPlannerInput(previewFile)
		if err != nil {
			return scopeExitError(err)
		}
		preview, err := scoperefresh.DecodePreview(data)
		if err != nil {
			return scopeExitError(err)
		}
		var approval *scoperefresh.Approval
		if approvalFile != "" {
			approvalBytes, readErr := os.ReadFile(approvalFile)
			if readErr != nil {
				return scopeExitError(readErr)
			}
			approval, err = scoperefresh.DecodeApproval(approvalBytes)
			if err != nil {
				return scopeExitError(err)
			}
		}
		result, err := scoperefresh.Apply(root, preview, approval)
		if err != nil {
			return scopeExitError(err)
		}
		return writeScopeResult(cmd, result)
	}}
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("baseline.scope.flag.preview_file"))
	command.Flags().StringVar(&approvalFile, "approval-file", "", cliMessage("baseline.scope.flag.approval_file"))
	_ = command.MarkFlagRequired("preview-file")
	return command
}

func newBaselineScopeStatusCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "status", Short: cliMessage("cli.short.baseline_scope_status"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return scopeExitError(err)
		}
		status, err := scoperefresh.Inspect(root, transaction)
		if err != nil {
			return scopeExitError(err)
		}
		return writePlannerJSON(cmd, status)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newBaselineScopeResumeCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "resume", Short: cliMessage("cli.short.baseline_scope_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return scopeExitError(err)
		}
		result, err := scoperefresh.Resume(root, transaction)
		if err != nil {
			return scopeExitError(err)
		}
		return writeScopeResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func writeScopeResult(cmd *cobra.Command, result *scoperefresh.ApplyResult) error {
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("baseline.scope.apply_summary", result.Status, result.TransactionID, result.AddedCount, result.RemovedCount, result.BaselineSHA256))
	return nil
}

func scopeExitError(err error) error {
	var interaction *hostInteractionError
	if errors.As(err, &interaction) {
		return &ExitError{Code: ExitInvalid, MachineCode: "interaction_required", Details: interaction.State,
			Msg: cliMessage("baseline.scope.error", err.Error())}
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "baseline_scope_invalid", Msg: cliMessage("baseline.scope.error", err.Error())}
}
