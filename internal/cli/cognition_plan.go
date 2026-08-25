package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
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
	command.AddCommand(newCognitionBootstrapPlanCmd(), newCognitionMigrationPlanCmd(), newCognitionCandidateValidateCmd(), newCognitionCodeTargetDiffCmd())
	return command
}

func newCognitionCodeTargetDiffCmd() *cobra.Command {
	var targetIndex string
	command := &cobra.Command{
		Use:   "diff",
		Short: cliMessage("cli.short.cognition_plan_diff"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			defer func() { targetIndex = "" }()
			root, err := resolveRepoRoot()
			if err != nil {
				return plannerExitError(err)
			}
			cfg, err := config.LoadReadOnly(root)
			if err != nil {
				return plannerExitError(err)
			}
			targetRaw, err := readOrInitializeCodeTarget(root, cfg.IndexPath, targetIndex)
			if err != nil {
				return plannerExitError(err)
			}
			report, err := cognitionplan.CompareCodeTargetIndex(root, cfg.IndexPath, targetRaw)
			if err != nil {
				return plannerExitError(err)
			}
			if flagJSON {
				return writePlannerJSON(cmd, report)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.code_diff_summary", report.Summary.Created,
				report.Summary.Updated, report.Summary.Deleted, report.Summary.Unchanged, report.RawBytesChanged, report.DiffSHA256))
			for _, change := range report.Changes {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.code_diff_change", change.Change,
					change.ObjectRef, strings.Join(change.ChangedFields, ",")))
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.plan.next_action", report.NextAction))
			return nil
		},
	}
	command.Flags().StringVar(&targetIndex, "target-index", "", cliMessage("cognition.plan.flag.target_index"))
	_ = command.MarkFlagRequired("target-index")
	return command
}

const defaultCodeTargetIndexPath = "aoci.code.target.txt"

func readOrInitializeCodeTarget(root, indexPath, requested string) ([]byte, error) {
	targetPath, conventional := codeTargetPlannerPath(root, requested)
	if !conventional {
		return readPlannerInput(targetPath)
	}
	if _, err := os.Lstat(targetPath); err == nil {
		return readPlannerInput(targetPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("planner_input_unavailable")
	}

	set, err := cognition.Load(root, indexPath)
	if err != nil {
		return nil, err
	}
	code := set.Volumes[cognition.ScopeCode]
	if set.LayoutMode != cognition.LayoutVolumesV1 || code == nil || code.State != cognition.AssetPresent {
		return nil, fmt.Errorf("code_target_index_requires_active_code_volume")
	}
	if err := afs.AtomicCreateCAS(targetPath, code.Raw); err != nil && !errors.Is(err, afs.ErrAtomicCreateConflict) {
		return nil, fmt.Errorf("code_target_initialize_failed")
	}
	return readPlannerInput(targetPath)
}

func codeTargetPlannerPath(root, requested string) (string, bool) {
	trimmed := strings.TrimSpace(requested)
	canonical := filepath.Join(root, defaultCodeTargetIndexPath)
	if !filepath.IsAbs(trimmed) && filepath.Clean(trimmed) == defaultCodeTargetIndexPath {
		return canonical, true
	}
	if filepath.IsAbs(trimmed) && filepath.Clean(trimmed) == filepath.Clean(canonical) {
		return canonical, true
	}
	return trimmed, false
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

// approvalArtifactMode keeps a minted approval readable only by the operator who
// produced it. The repository default is 0644, and this deliberately is not: an
// approval stands in for a human at a TTY, so anything that can read the file
// can be that human until the change it authorizes has been applied.
const approvalArtifactMode = 0o600

// writePlannerArtifact emits a machine artifact either to stdout, as before, or
// to an operator-chosen path.
//
// The two-command dance — mint the credential on stdout, redirect it into a
// file, hand the file to Apply — put a digest-bound artifact in a human's hands
// for no reason, and losing the redirect silently discarded a confirmation that
// had already been given. When outFile is empty the behavior is byte-identical
// to writePlannerJSON.
func writePlannerArtifact(cmd *cobra.Command, value any, outFile string) error {
	if strings.TrimSpace(outFile) == "" {
		return writePlannerJSON(cmd, value)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	target, err := plannerOutputTarget(outFile)
	if err != nil {
		return err
	}
	if err := afs.AtomicCreateCASMode(target, append(data, '\n'), approvalArtifactMode); err != nil {
		return classifyPlannerOutput(err)
	}
	if !flagQuiet {
		fmt.Fprintln(cmd.ErrOrStderr(), cliMessage("planner.artifact_written", outFile))
	}
	return nil
}

// validatePlannerOutput answers, before any human is asked to confirm anything,
// whether the artifact will have somewhere to land.
func validatePlannerOutput(outFile string) error {
	if strings.TrimSpace(outFile) == "" {
		return nil
	}
	target, err := plannerOutputTarget(outFile)
	if err != nil {
		return err
	}
	return classifyPlannerOutput(afs.ValidateCreateTarget(target))
}

func plannerOutputTarget(outFile string) (string, error) {
	trimmed := strings.TrimSpace(outFile)
	if trimmed == "" {
		return "", fmt.Errorf("planner_output_path_missing")
	}
	target, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("planner_output_unavailable")
	}
	return target, nil
}

// classifyPlannerOutput maps the filesystem layer's reasons onto the stable
// planner_output_* codes. An existing target and an existing symlink collapse
// into one code on purpose: both mean something is already there, and this
// command will not decide what it was.
func classifyPlannerOutput(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, afs.ErrAtomicCreateConflict):
		return fmt.Errorf("planner_output_target_exists")
	default:
		return fmt.Errorf("planner_output_unavailable")
	}
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
