// aoci update-entry —— 默认目标索引收尾或人用单条回写命令
// 索引条目: update_entry.go[CUE7T]
//
// 无单条输入时固定收尾 aoci.code.target.txt；与 MCP 复用同一目标绑定、整批 Apply
// 和正式索引回写管线。显式单条模式继续复用原子落盘与 Baseline 管线；
// ledger 记 source=human，--preview 只渲染 diff 不落盘。
// 退出码按 Fail.Code 分流(审查修正,此前一律 3 与 root.go 契约不符):
//
//	index_invalid → 2(索引损坏);internal → 10(内部错);
//	其余(bad_args/path_unsafe/write_conflict/not_initialized)→ 3(前置条件/参数)。
//
// 注: Fail.Code 字符串取值为 mcptools 稳定契约(出现在错误文案首行),此处按字面量比对。
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

type updateEntryRepairReport struct {
	Status                  string                    `json:"status"`
	Attempted               int                       `json:"attempted"`
	Applied                 int                       `json:"applied"`
	Remaining               int                       `json:"remaining"`
	FormalWritesStarted     bool                      `json:"formal_writes_started"`
	FindingCount            int                       `json:"finding_count"`
	Findings                []cognition.RepairFinding `json:"findings"`
	PreserveOtherCandidates bool                      `json:"preserve_other_candidates"`
	RetryScope              []string                  `json:"retry_scope"`
	LocalValidation         string                    `json:"local_validation"`
	ImpactValidation        string                    `json:"impact_validation"`
	NextAction              string                    `json:"next_action"`
}

func updateEntryRepairFindings(findings []cognition.RepairFinding) []cognition.RepairFinding {
	return mcptools.LocalizeRepairFindings(findings)
}

func init() {
	registerCommand(
		newUpdateEntryCmd(),
	)
}

// newUpdateEntryCmd构造人用单条Entry回写命令。
func newUpdateEntryCmd() *cobra.Command {
	var path, entry, sourceSHA256 string
	var useStdin, preview bool

	cmd := &cobra.Command{
		Use:   "update-entry",
		Short: cliMessage("cli.short.update_entry"),
		Long:  updateEntryLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCLIUpdateEntryMessages(); err != nil {
				return &ExitError{Code: ExitInternal, Err: err}
			}
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			singleEntryInput := false
			for _, name := range []string{"path", "entry", "source-sha256", "stdin", "preview"} {
				if cmd.Flags().Changed(name) {
					singleEntryInput = true
					break
				}
			}
			if !singleEntryInput {
				return runDefaultCodeTargetUpdate(cmd, root)
			}
			if path == "" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.path_required")}
			}
			// 条目来源: --entry 或 --stdin(二选一,stdin 优先级由显式 flag 决定)
			raw := entry
			if useStdin {
				data, rerr := io.ReadAll(os.Stdin)
				if rerr != nil {
					return fmt.Errorf("%s", cliMessage("cli.update_entry.stdin_failed", rerr))
				}
				raw = string(data)
			}
			if raw == "" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.entry_empty")}
			}
			sourceSHA256 = strings.ToLower(strings.TrimSpace(sourceSHA256))
			if (!preview || sourceSHA256 != "") && !validEntrySourceSHA256(sourceSHA256) {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.source_sha_invalid")}
			}
			out, fail := mcptools.ApplyUpdateEntriesAtomic(
				root,
				[]mcptools.AtomicUpdateItem{{
					Path: path, NewEntry: raw, SourceSHA256: sourceSHA256,
				}},
				ledger.SourceHuman,
				preview,
			)
			if fail != nil {
				if flagJSON && fail.Repairable {
					findings := updateEntryRepairFindings(fail.Findings)
					report := updateEntryRepairReport{
						Status:                  entriesAutoStatusRepairRequired,
						Attempted:               1,
						Applied:                 0,
						Remaining:               1,
						FormalWritesStarted:     fail.FormalWritesStarted,
						FindingCount:            len(findings),
						Findings:                findings,
						PreserveOtherCandidates: true,
						RetryScope:              mcptools.RepairRetryScope(findings),
						LocalValidation:         "passed",
						ImpactValidation:        "failed",
						NextAction:              cliMessage("entries.auto.recovery.repair_findings"),
					}
					if err := writeEntriesJSON(cmd.OutOrStdout(), report); err != nil {
						return &ExitError{Code: ExitInternal, Err: err}
					}
					return &ExitError{Code: ExitInvalid, Msg: ""}
				}
				msg := "[" + fail.Code + "] " + fail.Msg
				if fail.Hint != "" {
					msg += "\n" + cliMessage("cli.update_entry.hint_prefix") + fail.Hint
				}
				return &ExitError{Code: exitCodeForFail(fail.Code), Msg: msg}
			}
			if !flagQuiet {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), mcptools.RenderAtomicBatchOutcome(out))
			}
			if out != nil && !out.BaselineComplete {
				return &ExitError{
					Code: ExitInternal,
					Msg:  cliMessage("cli.update_entry.baseline_incomplete"),
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", cliMessage("cli.flag.entry_path"))
	cmd.Flags().StringVar(&entry, "entry", "", cliMessage("cli.flag.entry_text"))
	cmd.Flags().StringVar(&sourceSHA256, "source-sha256", "", cliMessage("cli.flag.source_sha256"))
	cmd.Flags().BoolVar(&useStdin, "stdin", false, cliMessage("cli.flag.entry_stdin"))
	cmd.Flags().BoolVar(&preview, "preview", false, cliMessage("cli.flag.entry_preview"))
	return cmd
}

func runDefaultCodeTargetUpdate(cmd *cobra.Command, root string) error {
	outcome, err := mcptools.ApplyCodeTargetIndex(root, version)
	if err != nil {
		return &ExitError{Code: ExitInternal, Err: err}
	}
	if !flagQuiet || !outcome.Applied {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), outcome.OutputJSON); err != nil {
			return &ExitError{Code: ExitInternal, Err: err}
		}
	}
	if outcome.Applied {
		return nil
	}
	exitCode := ExitConfig
	if outcome.RepairRequired {
		exitCode = ExitInvalid
	}
	return &ExitError{Code: exitCode, Msg: ""}
}

var cliUpdateEntryMessageArguments = map[string][]any{
	"cli.update_entry.path_required":        nil,
	"cli.update_entry.stdin_failed":         {fmt.Errorf("read")},
	"cli.update_entry.entry_empty":          nil,
	"cli.update_entry.source_sha_invalid":   nil,
	"cli.update_entry.hint_prefix":          nil,
	"cli.update_entry.baseline_incomplete":  nil,
	"entries.auto.recovery.repair_findings": nil,
}

func validateCLIUpdateEntryMessages() error {
	for key, arguments := range cliUpdateEntryMessageArguments {
		if _, err := textassets.Message(textassets.ActiveLocale(), key, arguments...); err != nil {
			return fmt.Errorf("update_entry_text_catalog_invalid:%s", key)
		}
	}
	return nil
}

// exitCodeForFail Fail 分类码 → CLI 退出码契约的映射
func exitCodeForFail(code string) int {
	switch code {
	case "index_invalid":
		return ExitInvalid // 2: 索引格式损坏
	case "internal":
		return ExitInternal // 10: 内部错误
	default:
		return ExitConfig // 3: 参数/路径/冲突/未初始化等前置条件问题
	}
}
