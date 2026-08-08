// aoci status —— 对齐状态一行速览。
//
// 纪律:
//   - 默认模式保持毫秒级纯读，不计算全仓文件指纹;
//   - --deep才采集当前快照并计算四态差集;
//   - --deep必须与Verify、Score和Check共同消费团队line_ending_tolerance;
//   - LineEndingOnly只作为信息计数展示，不计Stale;
//   - Baseline缺失时提示先建立Baseline;
//   - 本命令不修改正式索引或Baseline。
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

// statusReport是status --json的稳定输出模型。
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

	// Deep只在--deep模式下在场。
	Deep *deepCounts `json:"deep,omitempty"`
}

// deepCounts是--deep现算的四态与换行信息态计数。
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

			// 索引条目数。索引缺失时保持0，由输出提示init。
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

			// Baseline概况。默认模式只读取元数据，不重新哈希仓库。
			if baselineValue, exists, loadErr :=
				baseline.Load(root); loadErr == nil &&
				exists {
				report.BaselineExists = true
				report.BaselineFiles = len(
					baselineValue.Files,
				)
				report.BaselineUpdated =
					baselineValue.UpdatedAt
			}

			// 最近一次Verify快照。紧凑时间戳文件名字典序即时间序。
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

// buildStatusDeepCounts计算status --deep使用的统一漂移计数。
//
// 它与Verify和Score共用baseline.DetectWith及团队换行策略。
// Baseline损坏沿用status既有宽容读取语义：按无Baseline计算Unbaselined，
// 不在本辅助函数内重建或修改任何资产。
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

// countJSONLLines统计JSONL文件中的非空行；文件缺失返回0。
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

// splitLines按LF拆行并剥离行尾CR。
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
