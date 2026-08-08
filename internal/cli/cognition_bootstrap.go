package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func newCognitionBootstrapCmd() *cobra.Command {
	command := &cobra.Command{Use: "bootstrap", Short: cliMessage("cli.short.cognition_bootstrap")}
	command.AddCommand(
		newCognitionBootstrapPrepareCmd(), newCognitionBootstrapApproveCmd(), newCognitionBootstrapApplyCmd(),
		newCognitionBootstrapStatusCmd(), newCognitionBootstrapResumeCmd(), newCognitionBootstrapRollbackCmd(),
	)
	return command
}

func newCognitionBootstrapPrepareCmd() *cobra.Command {
	var planFile, candidateFile, previewFile, baselineTimestamp string
	command := &cobra.Command{
		Use:   "prepare",
		Short: cliMessage("cli.short.cognition_bootstrap_prepare"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return bootstrapExitError(err)
			}
			planData, err := readPlannerInput(planFile)
			if err != nil {
				return bootstrapExitError(err)
			}
			candidateData, err := readPlannerInput(candidateFile)
			if err != nil {
				return bootstrapExitError(err)
			}
			previewData, err := readPlannerInput(previewFile)
			if err != nil {
				return bootstrapExitError(err)
			}
			plan, err := cognitionplan.DecodePlan(planData)
			if err != nil {
				return bootstrapExitError(err)
			}
			candidate, err := cognitionplan.DecodeCandidate(candidateData)
			if err != nil {
				return bootstrapExitError(err)
			}
			preview, err := cognitionplan.DecodePreview(previewData)
			if err != nil {
				return bootstrapExitError(err)
			}
			if baselineTimestamp == "" {
				baselineTimestamp = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			}
			envelope, err := bootstrapapply.Prepare(root, &bootstrapapply.ApplyRequest{
				Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
				Candidate: *candidate, Preview: *preview, BaselineTimestamp: baselineTimestamp,
			})
			if err != nil {
				return bootstrapExitError(err)
			}
			if flagJSON {
				return writePlannerJSON(cmd, envelope)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage(
				"cognition.bootstrap.prepare_summary", envelope.PlanID, envelope.EnvelopeDigest,
				len(envelope.WriteSet), envelope.RootLast, envelope.NetworkAccessed,
			))
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.bootstrap.next_action", "human_approval_required"))
			return nil
		},
	}
	command.Flags().StringVar(&planFile, "plan-file", "", cliMessage("cognition.plan.flag.plan_file"))
	command.Flags().StringVar(&candidateFile, "candidate-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("cognition.bootstrap.flag.preview_file"))
	command.Flags().StringVar(&baselineTimestamp, "baseline-timestamp", "", cliMessage("cognition.bootstrap.flag.baseline_timestamp"))
	_ = command.MarkFlagRequired("plan-file")
	_ = command.MarkFlagRequired("candidate-file")
	_ = command.MarkFlagRequired("preview-file")
	return command
}

func newCognitionBootstrapApproveCmd() *cobra.Command {
	var envelopeFile, actor string
	command := &cobra.Command{
		Use:   "approve",
		Short: cliMessage("cli.short.cognition_bootstrap_approve"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readPlannerInput(envelopeFile)
			if err != nil {
				return bootstrapExitError(err)
			}
			envelope, err := bootstrapapply.DecodeApplyEnvelope(data)
			if err != nil {
				return bootstrapExitError(err)
			}
			phrase := "APPROVE BOOTSTRAP " + envelope.EnvelopeDigest
			exact := hostInteractionCommand("cognition", "bootstrap", "approve", "--envelope-file", envelopeFile, "--actor", actor, "--json")
			if err := requireHumanPhrase(cmd, phrase, cliMessage("cognition.bootstrap.approval_prompt", envelope.EnvelopeDigest, strings.Join(envelope.WriteSet, ", "), phrase), exact, envelope.EnvelopeDigest, fmt.Sprintf("write_set_count=%d", len(envelope.WriteSet)), "cognition bootstrap apply"); err != nil {
				return bootstrapExitError(err)
			}
			approval, err := bootstrapapply.RecordApproval(
				envelope, actor, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), envelope.EnvelopeDigest,
			)
			if err != nil {
				return bootstrapExitError(err)
			}
			if flagJSON {
				return writePlannerJSON(cmd, approval)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.bootstrap.approval_summary", approval.Actor, approval.ApplyEnvelopeDigest, approval.ApprovalDigest))
			return nil
		},
	}
	command.Flags().StringVar(&envelopeFile, "envelope-file", "", cliMessage("cognition.bootstrap.flag.envelope_file"))
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	_ = command.MarkFlagRequired("envelope-file")
	_ = command.MarkFlagRequired("actor")
	return command
}

func newCognitionBootstrapApplyCmd() *cobra.Command {
	var envelopeFile, approvalFile string
	command := &cobra.Command{
		Use:   "apply",
		Short: cliMessage("cli.short.cognition_bootstrap_apply"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return bootstrapExitError(err)
			}
			envelope, approval, err := readBootstrapBindings(envelopeFile, approvalFile)
			if err != nil {
				return bootstrapExitError(err)
			}
			result, err := bootstrapapply.Apply(root, envelope, approval)
			if err != nil {
				return bootstrapExitError(err)
			}
			return writeBootstrapResult(cmd, result)
		},
	}
	command.Flags().StringVar(&envelopeFile, "envelope-file", "", cliMessage("cognition.bootstrap.flag.envelope_file"))
	command.Flags().StringVar(&approvalFile, "approval-file", "", cliMessage("cognition.bootstrap.flag.approval_file"))
	_ = command.MarkFlagRequired("envelope-file")
	_ = command.MarkFlagRequired("approval-file")
	return command
}

func newCognitionBootstrapStatusCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{
		Use:   "status",
		Short: cliMessage("cli.short.cognition_bootstrap_status"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return bootstrapExitError(err)
			}
			status, err := bootstrapapply.Status(root, transaction)
			if err != nil {
				return bootstrapExitError(err)
			}
			if flagJSON {
				return writePlannerJSON(cmd, status)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.bootstrap.status_summary", status.TransactionID, status.Status, status.LayoutActivated, status.FormalComplete, status.ThirdPartyConflict, strings.Join(status.NextActions, ",")))
			return nil
		},
	}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	return command
}

func newCognitionBootstrapResumeCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{
		Use:   "resume",
		Short: cliMessage("cli.short.cognition_bootstrap_resume"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return bootstrapExitError(err)
			}
			result, err := bootstrapapply.Resume(root, transaction)
			if err != nil {
				return bootstrapExitError(err)
			}
			return writeBootstrapResult(cmd, result)
		},
	}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newCognitionBootstrapRollbackCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{
		Use:   "rollback",
		Short: cliMessage("cli.short.cognition_bootstrap_rollback"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return bootstrapExitError(err)
			}
			info, err := os.Stdin.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				return bootstrapExitError(fmt.Errorf("bootstrap_human_tty_required"))
			}
			phrase := "ROLLBACK BOOTSTRAP " + transaction
			fmt.Fprintln(cmd.ErrOrStderr(), cliMessage("cognition.bootstrap.rollback_prompt", phrase))
			line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil || strings.TrimSpace(line) != phrase {
				return bootstrapExitError(fmt.Errorf("bootstrap_human_rollback_not_confirmed"))
			}
			result, err := bootstrapapply.Rollback(root, transaction)
			if err != nil {
				return bootstrapExitError(err)
			}
			return writeBootstrapResult(cmd, result)
		},
	}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func readBootstrapBindings(envelopeFile, approvalFile string) (*bootstrapapply.ApplyEnvelope, *bootstrapapply.Approval, error) {
	envelopeData, err := readPlannerInput(envelopeFile)
	if err != nil {
		return nil, nil, err
	}
	approvalData, err := readPlannerInput(approvalFile)
	if err != nil {
		return nil, nil, err
	}
	envelope, err := bootstrapapply.DecodeApplyEnvelope(envelopeData)
	if err != nil {
		return nil, nil, err
	}
	approval, err := bootstrapapply.DecodeApproval(approvalData)
	if err != nil {
		return nil, nil, err
	}
	return envelope, approval, nil
}

func writeBootstrapResult(cmd *cobra.Command, result *bootstrapapply.ApplyResult) error {
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage(
		"cognition.bootstrap.result_summary", result.TransactionID, result.Status,
		result.LayoutActivated, result.FormalComplete, result.NetworkAccessed, result.NextAction,
	))
	return nil
}

func bootstrapExitError(err error) error {
	var interaction *hostInteractionError
	if errors.As(err, &interaction) {
		return &ExitError{Code: ExitInvalid, MachineCode: "interaction_required", Details: interaction.State,
			Msg: cliMessage("cognition.bootstrap.error", textassets.DiagnosticFacts(err.Error()))}
	}
	return &ExitError{
		Code: ExitInvalid, MachineCode: "cognition_bootstrap_invalid",
		Msg: cliMessage("cognition.bootstrap.error", textassets.DiagnosticFacts(err.Error())),
	}
}
