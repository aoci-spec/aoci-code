// aoci status: a one-line glance at the alignment state.
//
// Discipline:
//   - the default mode stays a millisecond-level pure read and computes no whole-repository file fingerprints;
//   - only --deep collects the current snapshot and computes the four-state differences;
//   - --deep must consume the team line_ending_tolerance together with Verify, Score, and Check;
//   - LineEndingOnly is shown only as an informational count and does not count as Stale;
//   - a missing Baseline prompts to establish the Baseline first;
//   - this command does not change the formal index or the Baseline.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/spf13/cobra"
)

// statusReport is the stable output model of status --json.
type statusReport struct {
	Root              string            `json:"root"`
	IndexEntries      int               `json:"index_entries"`
	BaselineExists    bool              `json:"baseline_exists"`
	BaselineFiles     int               `json:"baseline_files"`
	BaselineUpdated   string            `json:"baseline_updated,omitempty"`
	LastVerify        string            `json:"last_verify,omitempty"`
	ReportsPending    int               `json:"reports_pending"`
	RecentOps         []string          `json:"recent_ops"`
	LayoutMode        string            `json:"layout_mode,omitempty"`
	CompositeIdentity string            `json:"composite_identity,omitempty"`
	VolumeStates      map[string]string `json:"volume_states,omitempty"`

	// Deep is present only in --deep mode.
	Deep *deepCounts `json:"deep,omitempty"`
}

// deepCounts are the four-state and line-ending informational counts --deep computes live.
type deepCounts struct {
	Missing               int                     `json:"missing"`
	Orphan                int                     `json:"orphan"`
	Stale                 int                     `json:"stale"`
	Unbaselined           int                     `json:"unbaselined"`
	LineEndingOnly        int                     `json:"line_ending_only"`
	ObservedNew           int                     `json:"observed_new"`
	ObservedChanged       int                     `json:"observed_changed"`
	ObservedRemoved       int                     `json:"observed_removed"`
	ScopeChangeRequired   bool                    `json:"scope_change_required"`
	ScopePolicyIdentity   string                  `json:"scope_policy_identity,omitempty"`
	ActivePolicyIdentity  string                  `json:"active_scope_policy_identity,omitempty"`
	ObserveReviewRequired bool                    `json:"observe_review_required"`
	CognitionBudget       *cognitionbudget.Report `json:"cognition_budget,omitempty"`
}

func init() {
	var deep bool

	command := &cobra.Command{
		Use:   "status",
		Short: cliMessage("cli.short.status"),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
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

			report := &statusReport{
				Root:      root,
				RecentOps: []string{},
			}

			// The index entry count. With the index missing it stays 0, and the output prompts init.
			if set, readErr := cognition.Load(root, cfg.IndexPath); readErr == nil {
				report.IndexEntries = set.Root.ObjectCount
				if set.LayoutMode == cognition.LayoutVolumesV1 {
					report.IndexEntries = volumeObjectCount(set)
					report.LayoutMode = set.LayoutMode
					report.CompositeIdentity = set.CompositeIdentity
					report.VolumeStates = map[string]string{"meta": set.Meta.State}
					for _, id := range []string{"code", "database"} {
						state := cognition.AssetAbsent
						if asset := set.Volumes[id]; asset != nil {
							state = asset.State
						}
						report.VolumeStates[id] = state
					}
				}
			} else if set != nil && set.LayoutMode == cognition.LayoutVolumesV1 {
				return &ExitError{
					Code: ExitInvalid,
					Msg:  cliMessage("mcp.error.cognition_invalid", localeSafeCLIDetail(readErr.Error())),
				}
			}

			// The Baseline overview. The default mode reads only metadata and does not re-hash the repository.
			baselineValue, exists, loadErr := baseline.Load(root)
			if loadErr != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  loadErr,
					Msg:  cliMessage("status.baseline_unreadable", localeSafeCLIDetail(loadErr.Error())),
				}
			}
			if exists {
				report.BaselineExists = true
				report.BaselineFiles = len(
					baselineValue.Files,
				)
				report.BaselineUpdated =
					baselineValue.UpdatedAt
			}

			// The latest Verify snapshot. The compact timestamp filenames sort lexically in time order.
			if entries, readErr := os.ReadDir(
				paths.VerifyHistoryDir,
			); readErr == nil &&
				len(entries) > 0 {
				names := make(
					[]string,
					0,
					len(entries),
				)

				for _, entry := range entries {
					if !entry.IsDir() {
						names = append(
							names,
							entry.Name(),
						)
					}
				}

				sort.Strings(names)

				if len(names) > 0 {
					report.LastVerify =
						names[len(names)-1]
				}
			}

			report.ReportsPending =
				countJSONLLines(
					paths.ReportsPath,
				)

			recent, _ := ledger.Recent(
				root,
				3,
			)

			for _, event := range recent {
				report.RecentOps = append(
					report.RecentOps,
					event.Ts+" "+event.Op,
				)
			}

			// --deep is documented Legacy-only; diagnose the layout rather than
			// computing a drift set the Volumes governance path owns.
			if deep {
				// allowMissing, because the absent-index case has its own curated
				// answer below that names the command which resolves it. Guarding
				// it here first shadowed that answer with the raw filesystem
				// error and leaked an absolute host path into the machine message.
				if layoutErr := requireLegacyLayout(root, cfg, true); layoutErr != nil {
					return &ExitError{Code: ExitConfig, Err: layoutErr}
				}
			}

			if deep {
				deepResult, deepErr :=
					buildStatusDeepCounts(
						root,
						paths.IndexPath,
						cfg,
					)
				if deepErr != nil {
					if errors.Is(
						deepErr,
						os.ErrNotExist,
					) {
						return &ExitError{
							Code: ExitConfig,
							Msg:  cliMessage("status.deep_no_index"),
						}
					}

					return deepErr
				}

				report.Deep = deepResult
			}

			if flagJSON {
				output, marshalErr :=
					json.MarshalIndent(
						report,
						"",
						"  ",
					)
				if marshalErr != nil {
					return errors.New(cliMessage("status.json_error", marshalErr))
				}

				fmt.Fprintln(
					cmd.OutOrStdout(),
					string(output),
				)

				return nil
			}

			if flagQuiet {
				return nil
			}

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, cliMessage("status.repository", report.Root))

			fmt.Fprint(
				out,
				cliMessage("status.index_baseline", report.IndexEntries),
			)

			if report.BaselineExists {
				fmt.Fprint(
					out,
					cliMessage("status.baseline_exists", report.BaselineFiles, report.BaselineUpdated),
				)
			} else {
				fmt.Fprint(
					out,
					cliMessage("status.baseline_missing"),
				)
			}

			fmt.Fprintln(out)

			if report.LastVerify != "" {
				fmt.Fprintln(
					out,
					cliMessage("status.last_verify", report.LastVerify),
				)
			} else {
				fmt.Fprintln(
					out,
					cliMessage("status.no_verify"),
				)
			}

			fmt.Fprintln(out, cliMessage("status.pending_reports", report.ReportsPending))

			for _, operation := range report.RecentOps {
				fmt.Fprintln(
					out,
					cliMessage("status.recent_operation"),
					operation,
				)
			}

			if report.Deep != nil {
				fmt.Fprintln(
					out,
					cliMessage(
						"status.deep_counts",
						report.Deep.Missing,
						report.Deep.Orphan,
						report.Deep.Stale,
						report.Deep.Unbaselined,
						report.Deep.LineEndingOnly,
					),
				)
				fmt.Fprintln(out, cliMessage("status.deep_managed", report.Deep.ScopeChangeRequired,
					report.Deep.ObservedNew, report.Deep.ObservedChanged, report.Deep.ObservedRemoved,
					report.Deep.ObserveReviewRequired))
				if report.Deep.CognitionBudget != nil {
					fmt.Fprintln(out, cliMessage("status.deep_budget", cognitionbudget.Summary(report.Deep.CognitionBudget),
						report.Deep.CognitionBudget.Mode, len(report.Deep.CognitionBudget.Violations)))
				}
			}

			return nil
		},
	}

	command.Flags().BoolVar(
		&deep,
		"deep",
		false,
		cliMessage("cli.flag.status_deep"),
	)

	registerCommand(command)
}

// buildStatusDeepCounts computes the unified drift counts status --deep uses.
//
// It shares baseline.DetectWith and the team line-ending policy with Verify and Score.
// The command entry refuses an unreadable or corrupt Baseline first;
// this helper rebuilds or changes no asset.
func buildStatusDeepCounts(
	root string,
	indexPath string,
	cfg *config.Config,
) (
	*deepCounts,
	error,
) {
	set, loadErr := cognition.Load(root, cfg.IndexPath)
	if loadErr != nil {
		return nil, loadErr
	}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		return nil, errors.New(cliMessage("mcp.error.volume_read_only"))
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, errors.New(cliMessage("status.index_read_error", err))
	}

	document, _ := index.Parse(
		string(data),
	)

	index.ResolveRelPaths(
		document,
		root,
	)

	state, err := managedstate.Load(root, cfg)
	if err != nil {
		return nil, errors.New(cliMessage("status.snapshot_error", err))
	}
	result := &baseline.DetectResult{}
	if !state.ScopeChangeRequired {
		result, err = managedstate.Detect(root, cfg, document, state)
		if err != nil {
			return nil, errors.New(cliMessage("status.snapshot_error", err))
		}
	}
	budgetReport, err := cognitionbudget.Build(root, data, cfg.EffectiveCognitionBudget())
	if err != nil {
		return nil, errors.New(cliMessage("status.snapshot_error", err))
	}
	policy := cfg.EffectiveManagedScope()

	return &deepCounts{
		Missing: len(result.Missing), Orphan: len(result.Orphan), Stale: len(result.Stale),
		Unbaselined: len(result.Unbaselined), LineEndingOnly: len(result.LineEndingOnly),
		ObservedNew: len(result.ObservedNew), ObservedChanged: len(result.ObservedChanged), ObservedRemoved: len(result.ObservedRemoved),
		ScopeChangeRequired: state.ScopeChangeRequired, ScopePolicyIdentity: state.DesiredPolicyIdentity,
		ActivePolicyIdentity:  state.ActivePolicyIdentity,
		ObserveReviewRequired: !state.Legacy && policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired,
		CognitionBudget:       budgetReport,
	}, nil
}

// countJSONLLines counts the non-empty lines of a JSONL file; a missing file returns 0.
func countJSONLLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	count := 0

	for _, line := range splitLines(
		string(data),
	) {
		if len(line) > 0 {
			count++
		}
	}

	return count
}

// splitLines splits on LF and strips the trailing CR of each line.
func splitLines(text string) []string {
	lines := []string{}
	start := 0

	for index := 0; index < len(text); index++ {
		if text[index] != '\n' {
			continue
		}

		line := text[start:index]

		if len(line) > 0 &&
			line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		lines = append(
			lines,
			line,
		)

		start = index + 1
	}

	if start < len(text) {
		lines = append(
			lines,
			text[start:],
		)
	}

	return lines
}
