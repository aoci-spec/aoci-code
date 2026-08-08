// aoci verify —— 只读四态事实、换行等价信息、文件级策展解释和治理终态校验。
//
// 纪律:
//   - 绝不刷新Baseline或修改正式索引;
//   - Result.Missing永远保留baseline.Detect原始物理事实;
//   - RawMissing是Result.Missing的规范顶层别名，便于机器消费者直接区分原始事实;
//   - Missing互斥分为Actionable/CurationExcluded/Skipped三组;
//   - Included是Actionable子集，Pending是Skipped子集;
//   - 原始事实与治理债务分层:
//     原始Missing即使已经exclude，仍保留在Result.Missing和RawMissing中;
//     退出码只由尚未解决的Actionable、Pending、Orphan、Stale和Unbaselined决定;
//   - CurationExcluded与非Pending技术跳过属于已解释负空间，不导致exit 1;
//   - LineEndingOnly表示原始字节不同但CRLF/LF规范化指纹相同，
//     仅在团队line_ending_tolerance=true时出现，不计Stale与治理债务;
//   - 每次运行落verify_history快照，失败只警告;
//   - Ledger的exit_code与最终未解决治理债务判据同源。
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/spf13/cobra"
)

// verifyReport是verify的人读、JSON与历史快照统一模型。
//
// Result保留baseline.Detect原始事实和LineEndingOnly信息态；
// RawMissing是Result.Missing的规范顶层机器字段，二者必须始终完全一致；
// 其余字段表达治理解释。
// 消费方不得用len(Result.Missing)或len(RawMissing)单独判断仓库是否已经治理完成。
type verifyReport struct {
	Root                    string                       `json:"root"`
	IndexEntries            int                          `json:"index_entries"`
	DiskFiles               int                          `json:"disk_files"`
	BaselineExists          bool                         `json:"baseline_exists"`
	Result                  *baseline.DetectResult       `json:"result"`
	RawMissing              []string                     `json:"raw_missing"`
	ActionableMissing       []string                     `json:"actionable_missing"`
	IncludedMissing         []string                     `json:"included_missing"`
	CurationExcludedMissing []string                     `json:"curation_excluded_missing"`
	CurationExcludedDetails []curation.ExcludedMissing   `json:"curation_excluded_details"`
	SkippedMissing          []indexgen.SkippedMissing    `json:"skipped_missing"`
	PendingCurationMissing  []curation.PendingCandidate  `json:"pending_curation_missing"`
	StaleCurationDecisions  []string                     `json:"stale_curation_decisions"`
	CurationSHA256          string                       `json:"curation_sha256"`
	FormatWarnings          []string                     `json:"format_warnings"`
	ManagedScope            indexgen.ManagedScopeSummary `json:"managed_scope"`
	CognitionBudget         *cognitionbudget.Report      `json:"cognition_budget,omitempty"`
	GeneratedAt             string                       `json:"generated_at"`
}

type volumeVerifyAsset struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	AssetState  string `json:"asset_state"`
	SHA256      string `json:"sha256,omitempty"`
	ObjectCount int    `json:"object_count"`
}

type volumeVerifyReport struct {
	Root              string                  `json:"root"`
	LayoutMode        string                  `json:"layout_mode"`
	LayoutVersion     string                  `json:"layout_version"`
	StructureValid    bool                    `json:"structure_valid"`
	GovernanceAligned bool                    `json:"governance_aligned"`
	ReadOnlyCandidate bool                    `json:"read_only_candidate"`
	RootSHA256        string                  `json:"root_sha256"`
	MetaSHA256        string                  `json:"meta_sha256"`
	CompositeIdentity string                  `json:"composite_identity"`
	Volumes           []volumeVerifyAsset     `json:"volumes"`
	Warnings          []cognition.Finding     `json:"warnings,omitempty"`
	Governance        *volumegovernance.Facts `json:"governance"`
}

func init() {
	registerCommand(newVerifyCmd())
}

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: verifyShortHelp(),
		Long:  verifyLongHelp(),
		RunE:  runVerify,
	}
}

func runVerify(
	cmd *cobra.Command,
	args []string,
) error {
	start := time.Now()

	root, err := config.FindRepoRoot(
		".",
		flagRepo,
	)
	if err != nil {
		return &ExitError{
			Code: ExitConfig,
			Msg:  err.Error(),
		}
	}

	cfg, err := config.Load(root)
	if err != nil {
		return &ExitError{
			Code: ExitConfig,
			Msg:  err.Error(),
		}
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	cognitionSet, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ExitError{
				Code: ExitConfig,
				Msg:  cliMessage("verify.index_missing", paths.IndexPath),
			}
		}

		return fmt.Errorf("%s", cliMessage(
			"mcp.error.cognition_invalid",
			localeSafeCLIDetail(err.Error()),
		))
	}
	if cognitionSet.LayoutMode == cognition.LayoutVolumesV1 {
		return renderVolumeVerify(cmd, root, paths, cfg, cognitionSet, start)
	}

	document := cognitionSet.Root.Document
	if len(document.Sections) == 0 {
		return &ExitError{
			Code: ExitInvalid,
			Msg:  cliMessage("verify.index_invalid", paths.IndexPath),
		}
	}

	state, stateErr := managedstate.Load(root, cfg)
	if stateErr != nil {
		return &ExitError{
			Code: ExitInvalid,
			Msg:  stateErr.Error(),
		}
	}
	baselineExists := state.Baseline != nil
	result := &baseline.DetectResult{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}, Warnings: []string{}}
	if !state.ScopeChangeRequired {
		result, err = managedstate.Detect(root, cfg, document, state)
		if err != nil {
			return fmt.Errorf("%s", cliMessage("verify.snapshot_failed", localeSafeCLIDetail(err.Error())))
		}
	}

	missingClassification, _, err :=
		indexgen.BuildMissingClassification(
			root,
			cfg,
			document,
			result.Missing,
		)
	if err != nil {
		return &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"verify.classification_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	formatWarnings := []string{}

	for _, warning := range cognitionSet.Warnings {
		formatWarnings = append(
			formatWarnings,
			fmt.Sprintf(
				"L%d: %s",
				warning.Line,
				localeSafeCLIDetail(warning.Message),
			),
		)
	}

	formatWarnings = append(formatWarnings, localizePlanWarnings(state.Warnings)...)

	entryCount := 0

	for _, section := range document.Sections {
		entryCount += len(
			section.Entries,
		)
	}
	managedSummary := indexgen.ManagedScopeSummary{ScopeChangeRequired: state.ScopeChangeRequired,
		PolicyIdentity: state.DesiredPolicyIdentity, ActivePolicyIdentity: state.ActivePolicyIdentity}
	if state.Evaluation != nil {
		managedSummary.IndexCount, managedSummary.ObserveCount, managedSummary.ExcludeCount =
			state.Evaluation.IndexCount, state.Evaluation.ObserveCount, state.Evaluation.ExcludeCount
		policy := cfg.EffectiveManagedScope()
		managedSummary.ObserveReviewRequired = policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired
		if managedSummary.ObserveReviewRequired {
			managedSummary.ObservedPendingReview = len(result.ObservedNew) + len(result.ObservedChanged) + len(result.ObservedRemoved)
		}
	}
	budgetReport, err := cognitionbudget.Build(root, []byte(document.RawText), cfg.EffectiveCognitionBudget())
	if err != nil {
		return &ExitError{Code: ExitInvalid, Err: err}
	}
	diskFiles := len(state.Snapshot)
	if state.ScopeChangeRequired && state.Baseline != nil {
		// A desired-policy edit must not make the active governed set appear
		// empty. Until Apply, the Baseline receipt remains the formal scope.
		diskFiles = len(state.Baseline.Files)
	}

	report := &verifyReport{
		Root:                    root,
		IndexEntries:            entryCount,
		DiskFiles:               diskFiles,
		BaselineExists:          baselineExists,
		Result:                  result,
		RawMissing:              append([]string{}, result.Missing...),
		ActionableMissing:       missingClassification.Actionable,
		IncludedMissing:         missingClassification.Included,
		CurationExcludedMissing: missingClassification.CurationExcluded,
		CurationExcludedDetails: missingClassification.CurationExcludedDetails,
		SkippedMissing:          missingClassification.Skipped,
		PendingCurationMissing:  missingClassification.Pending,
		StaleCurationDecisions:  missingClassification.StaleDecisions,
		CurationSHA256:          missingClassification.CurationSHA256,
		FormatWarnings:          formatWarnings,
		ManagedScope:            managedSummary,
		CognitionBudget:         budgetReport,
		GeneratedAt: time.Now().
			UTC().
			Format(time.RFC3339),
	}

	human := renderVerifyHuman(report)

	if err := os.MkdirAll(
		paths.VerifyHistoryDir,
		0o755,
	); err == nil {
		filename := time.Now().
			UTC().
			Format("20060102T150405Z") +
			".txt"

		if writeErr := afs.AtomicWrite(
			filepath.Join(
				paths.VerifyHistoryDir,
				filename,
			),
			[]byte(human),
		); writeErr != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), cliMessage(
				"verify.history_write_warning",
				localeSafeCLIDetail(writeErr.Error()),
			))
		}
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), cliMessage(
			"verify.history_mkdir_warning",
			localeSafeCLIDetail(err.Error()),
		))
	}

	unresolved := verifyUnresolvedDriftCount(report)

	exitCode := ExitOK
	if unresolved > 0 {
		exitCode = ExitDrift
	}

	ledger.Append(
		root,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:          "verify",
			PathsCount:  diskFiles,
			DurationMs:  time.Since(start).Milliseconds(),
			DriftWarned: unresolved > 0,
			Source:      ledger.SourceHuman,
			ExitCode:    &exitCode,
		},
	)

	if flagJSON {
		encoder := json.NewEncoder(
			cmd.OutOrStdout(),
		)
		encoder.SetIndent(
			"",
			"  ",
		)

		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("%s", cliMessage(
				"verify.json_failed",
				localeSafeCLIDetail(err.Error()),
			))
		}
	} else if !flagQuiet {
		fmt.Fprint(
			cmd.OutOrStdout(),
			human,
		)
	}

	if exitCode != ExitOK {
		return &ExitError{
			Code: exitCode,
		}
	}

	return nil
}

func renderVolumeVerify(cmd *cobra.Command, root string, paths config.Paths, cfg *config.Config, set *cognition.Set, start time.Time) error {
	facts, factsErr := volumegovernance.Assess(root, cfg, set)
	if factsErr != nil {
		return &ExitError{Code: ExitInvalid, Err: factsErr}
	}
	report := volumeVerifyReport{
		Root: root, LayoutMode: set.LayoutMode, LayoutVersion: set.LayoutVersion,
		StructureValid: true, GovernanceAligned: facts.GovernanceAligned, ReadOnlyCandidate: true, RootSHA256: set.Root.SHA256,
		MetaSHA256: set.Meta.SHA256, CompositeIdentity: set.CompositeIdentity,
		Warnings: append([]cognition.Finding{}, set.Warnings...), Governance: facts,
	}
	for _, id := range []string{"meta", "code", "database"} {
		asset := set.Volumes[id]
		if asset == nil {
			if id == "meta" {
				continue
			}
			report.Volumes = append(report.Volumes, volumeVerifyAsset{ID: id, Kind: id, Path: "aoci." + id + ".txt", AssetState: cognition.AssetAbsent})
			continue
		}
		report.Volumes = append(report.Volumes, volumeVerifyAsset{ID: id, Kind: asset.Descriptor.Kind, Path: asset.Descriptor.Path, AssetState: asset.State, SHA256: asset.SHA256, ObjectCount: asset.ObjectCount})
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	human := cliMessage("verify.volume.valid", report.RootSHA256, report.MetaSHA256, report.CompositeIdentity)
	if err := os.MkdirAll(paths.VerifyHistoryDir, 0o755); err == nil {
		_ = afs.AtomicWrite(filepath.Join(paths.VerifyHistoryDir, time.Now().UTC().Format("20060102T150405Z")+".txt"), []byte(human))
	}
	exitCode := ExitOK
	if !report.GovernanceAligned {
		exitCode = ExitDrift
	}
	ledger.Append(root, cfg.LedgerEnabled, ledger.Event{Op: "verify", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceHuman, EntryCount: volumeObjectCount(set), ExitCode: &exitCode})
	if flagJSON {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		if err != nil {
			return err
		}
		if exitCode != ExitOK {
			return &ExitError{Code: exitCode}
		}
		return nil
	}
	if !flagQuiet {
		_, err = fmt.Fprint(cmd.OutOrStdout(), human)
	}
	if err != nil {
		return err
	}
	if exitCode != ExitOK {
		return &ExitError{Code: exitCode}
	}
	return nil
}

func volumeObjectCount(set *cognition.Set) int {
	count := 0
	for _, asset := range set.Volumes {
		count += asset.ObjectCount
	}
	return count
}

// verifyRawDriftCount返回baseline.Detect原始四态数量。
//
// LineEndingOnly是信息态，不属于原始四态，也不计入该数字。
func verifyRawDriftCount(
	report *verifyReport,
) int {
	if report == nil ||
		report.Result == nil {
		return 0
	}

	return len(report.Result.Missing) +
		len(report.Result.Orphan) +
		len(report.Result.Stale) +
		len(report.Result.Unbaselined)
}

// verifyUnresolvedDriftCount返回尚未解决的治理债务数量。
//
// Missing只统计Actionable与Pending：
//   - Included已经属于Actionable，不得重复相加；
//   - CurationExcluded是已经完成的负空间裁决；
//   - 非Pending技术跳过是已解释状态，不自动构成治理任务。
//
// Orphan、Stale与Unbaselined仍是未解决事实。
// LineEndingOnly仅为表示层差异，不构成治理债务。
func verifyUnresolvedDriftCount(
	report *verifyReport,
) int {
	if report == nil {
		return 0
	}

	total := len(report.ActionableMissing) +
		len(report.PendingCurationMissing)

	if report.Result != nil {
		total += len(report.Result.Orphan) +
			len(report.Result.Stale) +
			len(report.Result.Unbaselined)
	}
	if report.ManagedScope.ScopeChangeRequired {
		total++
	}
	if report.ManagedScope.ObserveReviewRequired {
		total += len(report.Result.ObservedNew) + len(report.Result.ObservedChanged) + len(report.Result.ObservedRemoved)
	}
	if report.CognitionBudget != nil && report.CognitionBudget.Mode == machinecontract.BudgetModeEnforce {
		total += len(report.CognitionBudget.Violations)
	}

	return total
}

// collectCurationExcludedMissing保留旧包内调用与顺序契约。
// 新生产路径统一使用BuildMissingClassification；本函数仅供兼容测试。
func collectCurationExcludedMissing(
	cfg *config.Config,
	missing []string,
) []string {
	items := []string{}

	if cfg == nil {
		return items
	}

	for _, relPath := range missing {
		if cfg.CurationExcluded(
			relPath,
		) {
			items = append(
				items,
				relPath,
			)
		}
	}

	return items
}
