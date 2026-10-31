// aoci verify: the read-only four states, line-ending equivalence information, file-level curation explanation, and the governance terminal-state check.
//
// Discipline:
//   - never refreshes the Baseline or changes the formal index;
//   - Result.Missing always keeps the raw physical fact of baseline.Detect;
//   - RawMissing is the canonical top-level alias of Result.Missing, so machine consumers can tell the raw fact apart directly;
//   - Missing splits exclusively into the three groups Actionable/CurationExcluded/Skipped;
//   - Included is a subset of Actionable, Pending a subset of Skipped;
//   - raw facts and governance debt are layered:
//     a raw Missing, even when already excluded, stays in Result.Missing and RawMissing;
//     the exit code is decided only by the unresolved Actionable, Pending, Orphan, Stale, and Unbaselined;
//   - CurationExcluded and non-Pending technical skips are explained negative space and do not cause exit 1;
//   - LineEndingOnly means the raw bytes differ but the CRLF/LF-normalized fingerprints agree;
//     it appears only when the team line_ending_tolerance=true, and counts as neither Stale nor governance debt;
//   - every run lands a verify_history snapshot, and a failure there only warns;
//   - the Ledger's exit_code and the final unresolved governance debt criterion share one source.
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

// verifyReport is verify's unified model for the human-readable report, the JSON, and the history snapshot.
//
// Result keeps the raw facts of baseline.Detect and the LineEndingOnly informational state;
// RawMissing is the canonical top-level machine field for Result.Missing, and the two must always agree exactly;
// the remaining fields express the governance explanation.
// A consumer must not judge whether the repository's governance is complete by len(Result.Missing) or len(RawMissing) alone.
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
	Root              string                        `json:"root"`
	LayoutMode        string                        `json:"layout_mode"`
	LayoutVersion     string                        `json:"layout_version"`
	StructureValid    bool                          `json:"structure_valid"`
	GovernanceAligned bool                          `json:"governance_aligned"`
	ReadOnlyCandidate bool                          `json:"read_only_candidate"`
	RootSHA256        string                        `json:"root_sha256"`
	MetaSHA256        string                        `json:"meta_sha256"`
	CompositeIdentity string                        `json:"composite_identity"`
	Volumes           []volumeVerifyAsset           `json:"volumes"`
	Warnings          []cognition.Finding           `json:"warnings,omitempty"`
	Governance        *volumegovernance.Facts       `json:"governance"`
	EScale            *volumegovernance.EScaleFacts `json:"e_scale,omitempty"`
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

	passed := exitCode == ExitOK
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
			CheckPassed: &passed,
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
	escale := volumegovernance.AssessEScale(root, set)
	report.EScale = &escale
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
	// result says verify ran; check_passed is its verdict (#150).
	passed := exitCode == ExitOK
	ledger.Append(root, cfg.LedgerEnabled, ledger.Event{Op: "verify", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceHuman,
		EntryCount: volumeObjectCount(set), ExitCode: &exitCode,
		GovernanceResult: facts.Result, NextAction: facts.NextRequiredAction, CheckPassed: &passed})
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
		if err == nil && report.EScale != nil {
			writeEScale(cmd.OutOrStdout(), *report.EScale)
		}
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

// verifyRawDriftCount returns the count of baseline.Detect's raw four states.
//
// LineEndingOnly is informational, not one of the raw four states, and does not count toward this number.
func verifyRawDriftCount(
	report *verifyReport,
) int {
	if report == nil ||
		report.Result == nil {
		return 0
	}

	// A file without an entry and outside the Baseline appears in both Missing and Unbaselined; deduplicate by path.
	return len(uniquePaths(report.Result.Missing, report.Result.Stale, report.Result.Unbaselined)) +
		len(report.Result.Orphan)
}

// uniquePaths returns the number of paths in the union of the tables (deduplicated); Missing and Unbaselined overlap on files added
// after scan, and adding table by table would count them twice.
func uniquePaths(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, rel := range list {
			if !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
		}
	}
	return out
}

// verifyUnresolvedDriftCount returns the count of unresolved governance debt.
//
// Missing counts only Actionable and Pending:
//   - Included already belongs to Actionable and must not be added again;
//   - CurationExcluded is a completed negative-space decision;
//   - a non-Pending technical skip is an explained state and does not form a governance task by itself.
//
// Orphan, Stale, and Unbaselined remain unresolved facts.
// LineEndingOnly is only a representation difference and forms no governance debt.
func verifyUnresolvedDriftCount(
	report *verifyReport,
) int {
	if report == nil {
		return 0
	}

	total := len(report.ActionableMissing) +
		len(report.PendingCurationMissing)

	if report.Result != nil {
		// A path in Unbaselined already covered by Missing (a file added after scan) is not counted again:
		// it is either already in Actionable/Pending, or decided negative space.
		missing := map[string]bool{}
		for _, rel := range report.Result.Missing {
			missing[rel] = true
		}
		unbaselinedWithEntry := 0
		for _, rel := range report.Result.Unbaselined {
			if !missing[rel] {
				unbaselinedWithEntry++
			}
		}
		total += len(report.Result.Orphan) +
			len(report.Result.Stale) +
			unbaselinedWithEntry
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

// collectCurationExcludedMissing keeps the old in-package call and ordering contract.
// New production paths all use BuildMissingClassification; this function serves compatibility tests only.
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

// localizePlanWarnings keeps machine warning text safe for the active Locale.
func localizePlanWarnings(warnings []string) []string {
	localized := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		localized = append(localized, localeSafeCLIDetail(warning))
	}
	return localized
}
