package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func init() {
	registerCommand(newCognitionCmd())
}

func newCognitionCmd() *cobra.Command {
	command := &cobra.Command{Use: "cognition", Short: cliMessage("cli.short.cognition")}
	command.AddCommand(newCognitionPlanCmd(), newCognitionBootstrapCmd(), newCognitionMigrationCmd(), newCognitionOnboardCmd(), newCognitionSystemCmd())
	return command
}

func newCognitionPlanCmd() *cobra.Command {
	command := &cobra.Command{Use: "plan", Short: cliMessage("cli.short.cognition_plan")}
	command.AddCommand(newCognitionBootstrapPlanCmd(), newCognitionMigrationPlanCmd(), newCognitionCandidateValidateCmd())
	return command
}

func newCognitionBootstrapPlanCmd() *cobra.Command {
	var kinds []string
	var locale string
	command := &cobra.Command{
		Use:   "bootstrap",
		Short: cliMessage("cli.short.cognition_plan_bootstrap"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return plannerExitError(err)
			}
			if locale == "" {
				locale = textassets.ActiveLocale()
			}
			if err := cognitionplan.ValidateLocale(locale); err != nil {
				return plannerExitError(err)
			}
			plan, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale, TargetKinds: kinds})
			if err != nil {
				return plannerExitError(err)
			}
			return writePlannerPlan(cmd, plan)
		},
	}
	command.Flags().StringSliceVar(&kinds, "kind", nil, cliMessage("cognition.plan.flag.kind"))
	command.Flags().StringVar(&locale, "locale", "", cliMessage("cognition.plan.flag.locale"))
	return command
}

func newCognitionMigrationPlanCmd() *cobra.Command {
	var kinds []string
	var locale string
	command := &cobra.Command{
		Use:   "migration",
		Short: cliMessage("cli.short.cognition_plan_migration"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return plannerExitError(err)
			}
			if locale == "" {
				locale = textassets.ActiveLocale()
			}
			if err := cognitionplan.ValidateLocale(locale); err != nil {
				return plannerExitError(err)
			}
			plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale, TargetKinds: kinds})
			if err != nil {
				return plannerExitError(err)
			}
			return writePlannerPlan(cmd, plan)
		},
	}
	command.Flags().StringSliceVar(&kinds, "kind", nil, cliMessage("cognition.plan.flag.kind"))
	command.Flags().StringVar(&locale, "locale", "", cliMessage("cognition.plan.flag.locale"))
	return command
}

func newCognitionCandidateValidateCmd() *cobra.Command {
	var planFile, candidateFile string
	command := &cobra.Command{
		Use:   "validate",
		Short: cliMessage("cli.short.cognition_plan_validate"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return plannerExitError(err)
			}
			planData, err := readPlannerInput(planFile)
			if err != nil {
				return plannerExitError(err)
			}
			candidateData, err := readPlannerInput(candidateFile)
			if err != nil {
				return plannerExitError(err)
			}
			plan, err := cognitionplan.DecodePlan(planData)
			if err != nil {
				return plannerExitError(err)
			}
			candidate, err := cognitionplan.DecodeCandidate(candidateData)
			if err != nil {
				return plannerExitError(err)
			}
			preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
			if err != nil {
				return plannerExitError(err)
			}
			if flagJSON {
				if err := writePlannerJSON(cmd, preview); err != nil {
					return err
				}
				if preview.Status != "preview_ready" && preview.Status != "superseded" {
					return &ExitError{Code: ExitInvalid}
				}
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.preview_summary", preview.Operation, preview.Status, preview.PlanID, len(preview.Risks), preview.FormalAssetProof.FormalAssetsUnchanged, preview.NetworkAccessed))
			for _, risk := range preview.Risks {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.risk", risk.Code, risk.Target))
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.next_action", preview.NextAction))
			if preview.Status != "preview_ready" && preview.Status != "superseded" {
				return &ExitError{Code: ExitInvalid}
			}
			return nil
		},
	}
	command.Flags().StringVar(&planFile, "plan-file", "", cliMessage("cognition.plan.flag.plan_file"))
	command.Flags().StringVar(&candidateFile, "candidate-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	_ = command.MarkFlagRequired("plan-file")
	_ = command.MarkFlagRequired("candidate-file")
	return command
}

func writePlannerPlan(cmd *cobra.Command, plan *cognitionplan.Plan) error {
	if flagJSON {
		return writePlannerJSON(cmd, plan)
	}
	mappingRecords := 0
	if plan.Mapping != nil {
		mappingRecords = len(plan.Mapping.Records)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.summary", plan.Operation, plan.Layout, plan.Status, plan.PlanID, len(plan.AuthoringTasks), mappingRecords, plan.FormalAssetProof.FormalAssetsUnchanged, plan.NetworkAccessed))
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.next_action", plan.NextAction))
	return nil
}

func writePlannerJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readPlannerInput(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("planner_input_path_missing")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("planner_input_unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("planner_input_not_regular")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("planner_input_unavailable")
	}
	return data, nil
}

func plannerExitError(err error) error {
	var exitError *ExitError
	if errors.As(err, &exitError) {
		return err
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "cognition_planner_invalid", Msg: cliMessage("cognition.plan.error", textassets.DiagnosticFacts(err.Error()))}
}
