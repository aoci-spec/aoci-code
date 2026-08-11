// 索引条目: doctor.go[CDR8M]
// 职责: aoci doctor —— 环境与接入的【只读】诊断命令。四组检查各项 ✓/✗/– 三态,末尾汇总。
//
// 设计原则:
//   - 默认零外发: 仅 --net 显式开启才做端点连通探测(与 R19 数据主权口径一致);
//   - 轻量: 不做全仓 hash(漂移详情指向 aoci verify,与 status "毫秒级纯读"同纪律);
//   - 判据单一事实源: 组二接入判据一律调 hooks.IsXxxInstalled,与 installer 同源不重复;
//   - 密钥安全: 沿用 configToLLMOptions 唯一读密钥,状态只报有无绝不打印值(R19);
//   - 退出码: 全过 0 / 有失败项 1 / 仓库根都找不到 3(对齐 verify 风格)。
//
// 技术产物排除缺项提示(R60-F.9-A2 下半,2026-07-18):
// DefaultConfig 排除清单扩充只救新仓 —— 已落盘的显式 exclude_dirs 会冻结
// 旧默认(applyFallbacks 仅 nil 回填,显式清单必须保留是既有纪律)。存量仓
// 迁移走 doctor 信息态提示: 团队 config.json 显式声明了 exclude_dirs 且
// 缺少当前默认技术产物项时,列出缺项供维护者裁决是否补入;零静默修改配置
// (与"不静默修正"纪律同构)。判定必须读团队层原始 JSON 判断键是否显式
// 在场 —— Load 合并态在字段缺失时已被回填默认,直接对比会对"跟随默认"
// 的仓库误报缺项。
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/spf13/cobra"
)

// checkState 是单项检查的三态结果。
type checkState int

const (
	statePass checkState = iota // 通过
	stateFail                   // 失败(计入退出码非零)
	stateNA                     // 不适用(不影响退出码)
	stateInfo                   // 纯信息(不影响退出码,如数据主权提示行)
)

// stateMark 返回三态的显示前缀。
func stateMark(s checkState) string {
	switch s {
	case statePass:
		return "[✓]"
	case stateFail:
		return "[✗]"
	case stateNA:
		return "[–]"
	default:
		return "[·]"
	}
}

// doctorReport 累积检查结果并统计失败数,用于末尾汇总与退出码。
type doctorReport struct {
	out      io.Writer
	failures int
}

// line 输出一行检查结果;stateFail 时累加失败计数。
func (r *doctorReport) line(s checkState, label, detail string) {
	if detail != "" {
		fmt.Fprintf(r.out, "%s %s: %s\n", stateMark(s), label, detail)
	} else {
		fmt.Fprintf(r.out, "%s %s\n", stateMark(s), label)
	}
	if s == stateFail {
		r.failures++
	}
}

// group 输出一个分组标题。
func (r *doctorReport) group(title string) {
	fmt.Fprintf(r.out, "\n== %s ==\n", title)
}

// missingTechnicalExcludeDirs 判断团队 config.json 显式声明的 exclude_dirs
// 是否缺少当前默认技术产物项(A2 存量仓迁移提示的唯一判定函数)。
//
// 返回缺项列表;以下情况一律返回空(不提示):
//   - config.json 不存在或不可解析(其余检查已各自报告,不重复);
//   - exclude_dirs 键未显式声明(Load 会回填当前默认,无缺项可言);
//   - 显式清单已覆盖全部当前默认项。
//
// 判定读团队层原始 JSON 而非 Load 合并态 —— 合并态无法区分"显式短清单"
// 与"缺失被回填",直接对比会对后者误报。
func missingTechnicalExcludeDirs(root string) []string {
	data, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		return nil
	}

	var raw struct {
		ExcludeDirs *[]string `json:"exclude_dirs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if raw.ExcludeDirs == nil {
		return nil // 键未显式声明,跟随当前默认,无缺项
	}

	declared := map[string]bool{}
	for _, d := range *raw.ExcludeDirs {
		declared[d] = true
	}

	var missing []string
	for _, want := range config.DefaultTechnicalExcludeDirs() {
		if !declared[want] {
			missing = append(missing, want)
		}
	}
	return missing
}

// newDoctorCmd 构造 `aoci doctor` 命令。
func newDoctorCmd() *cobra.Command {
	var withNet bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: cliMessage("cli.short.doctor"),
		Long:  doctorLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep := &doctorReport{out: cmd.OutOrStdout()}
			fmt.Fprintln(rep.out, cliMessage("doctor.title"))

			// —— 组一: 仓库与核心资产 ——
			rep.group(cliMessage("doctor.group.repository"))

			root, err := resolveRepoRoot()
			if err != nil {
				rep.line(stateFail, cliMessage("doctor.label.repo_root"), cliMessage("doctor.repo_missing"))
				fmt.Fprintf(rep.out, "\n%s\n", cliMessage("doctor.aborted"))
				// 仓库根都找不到 → 退出码 3
				return &doctorExitError{code: 3}
			}
			rep.line(statePass, cliMessage("doctor.label.repo_root"), root)

			// 配置加载
			cfg, cfgErr := config.Load(root)
			if cfgErr != nil {
				rep.line(stateFail, cliMessage("doctor.label.config"), cliMessage("doctor.config_failed", cfgErr))
				// 配置坏了后续 AI 检查无从谈起,但仓库/索引检查仍继续,故用默认配置兜底
				cfg = config.DefaultConfig()
			} else {
				rep.line(statePass, cliMessage("doctor.label.config"), cliMessage("doctor.config_ok", cfg.IndexPath))
			}

			// 技术产物排除缺项(A2 存量仓迁移提示;信息态,零静默修改)
			if missing := missingTechnicalExcludeDirs(root); len(missing) > 0 {
				rep.line(stateInfo, cliMessage("doctor.label.exclude_gaps"),
					cliMessage("doctor.exclude_gaps", strings.Join(missing, ", ")))
			}

			// aoci.txt 解析
			indexPath := cfg.IndexPath
			if indexPath == "" {
				indexPath = "aoci.txt"
			}
			cognitionSet, readErr := cognition.Load(root, indexPath)
			if readErr != nil {
				rep.line(stateFail, cliMessage("doctor.label.index_file"), cliMessage("doctor.index_read_failed", indexPath, readErr))
			} else {
				sectionCount := 0
				entryCount := cognitionSet.Root.ObjectCount
				if cognitionSet.LayoutMode == cognition.LayoutVolumesV1 {
					sectionCount = len(cognitionSet.DeclaredOrder)
					entryCount = volumeObjectCount(cognitionSet)
				} else {
					sectionCount = len(cognitionSet.Root.Document.Sections)
				}
				if sectionCount == 0 {
					rep.line(stateFail, cliMessage("doctor.label.index_parse"), cliMessage("doctor.index_empty", indexPath))
				} else {
					detail := cliMessage("doctor.index_counts", sectionCount, entryCount, len(cognitionSet.Warnings))
					if len(cognitionSet.Warnings) > 0 {
						rep.line(statePass, cliMessage("doctor.label.index_parse"), detail+cliMessage("doctor.index_warning_suffix"))
					} else {
						rep.line(statePass, cliMessage("doctor.label.index_parse"), detail)
					}
				}
			}

			// baseline 存在性(第二返回值 bool = 是否找到)
			_, blExists, blErr := baseline.Load(root)
			switch {
			case blErr != nil:
				rep.line(stateFail, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_read_failed", blErr))
			case !blExists:
				rep.line(stateFail, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_missing"))
			default:
				rep.line(statePass, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_exists"))
			}

			// 漂移: 不现算,指路 verify(保持轻量)
			rep.line(stateInfo, cliMessage("doctor.label.drift"), cliMessage("doctor.drift_deferred"))

			// —— 组二: Agent 接入 ——
			rep.group(cliMessage("doctor.group.agents"))

			detected := hooks.Detect(root)
			if len(detected) == 0 {
				rep.line(stateInfo, cliMessage("doctor.label.detected_agents"), cliMessage("doctor.no_agents"))
			} else {
				rep.line(stateInfo, cliMessage("doctor.label.detected_agents"), fmt.Sprintf("%v", detected))
			}

			// 判据一律调 hooks.IsXxxInstalled(与 installer 单一事实源)
			markInstalled(rep, cliMessage("doctor.label.agents_block"), hooks.IsAgentsBlockPresent(root))
			markInstalled(rep, cliMessage("doctor.label.claude_mcp"), hooks.IsClaudeMCPInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.claude_hook"), hooks.IsClaudeHookInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.codex_mcp"), hooks.IsCodexMCPInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.opencode_mcp"), hooks.IsOpenCodeMCPInstalled(root))

			// —— 组三: AI 增强层 ——
			rep.group(cliMessage("doctor.group.ai"))

			if !cfg.IsAIEnabled() {
				if cfg.AI.Enabled && cfg.AI.BaseURL == "" {
					rep.line(stateFail, cliMessage("doctor.label.ai_status"), cliMessage("doctor.ai_incomplete"))
				} else {
					rep.line(stateNA, cliMessage("doctor.label.ai_status"), cliMessage("doctor.ai_disabled"))
				}
				// 数据主权行照常给出(即使未启用也明确"无外发")
				rep.line(stateInfo, cliMessage("doctor.label.data_sovereignty"), cliMessage("doctor.no_exfiltration"))
			} else {
				rep.line(statePass, cliMessage("doctor.label.ai_status"), cliMessage("doctor.ai_enabled", cfg.AI.Model))
				// 数据主权透明行 —— doctor 最重要的输出之一
				rep.line(stateInfo, cliMessage("doctor.label.data_destination"), cfg.AI.BaseURL)

				// 密钥状态: 只报有无,绝不打印值
				if cfg.AI.APIKeyEnv == "" {
					rep.line(stateInfo, cliMessage("doctor.label.secret"), cliMessage("doctor.no_auth"))
				} else if os.Getenv(cfg.AI.APIKeyEnv) != "" {
					rep.line(statePass, cliMessage("doctor.label.secret"), cliMessage("doctor.secret_set", cfg.AI.APIKeyEnv))
				} else {
					rep.line(stateFail, cliMessage("doctor.label.secret"), cliMessage("doctor.secret_missing", cfg.AI.APIKeyEnv, cfg.AI.APIKeyEnv))
				}

				// 端点连通: 仅 --net 才做
				if withNet {
					doctorProbeEndpoint(rep, cfg)
				} else {
					rep.line(stateInfo, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_not_probed"))
				}
			}

			// —— 组四: 平台特性 ——
			rep.group(cliMessage("doctor.group.platform"))
			if runtime.GOOS == "windows" {
				rep.line(stateInfo, cliMessage("doctor.label.platform"), cliMessage("doctor.platform_windows"))
			} else {
				rep.line(stateInfo, cliMessage("doctor.label.platform"), runtime.GOOS)
			}

			// —— 汇总与退出码 ——
			fmt.Fprintln(rep.out, "")
			if rep.failures == 0 {
				fmt.Fprintln(rep.out, cliMessage("doctor.complete"))
				return nil
			}
			fmt.Fprintln(rep.out, cliMessage("doctor.failed", rep.failures))
			return &doctorExitError{code: 1}
		},
	}

	cmd.Flags().BoolVar(&withNet, "net", false, cliMessage("cli.flag.doctor_net"))
	return cmd
}

// markInstalled 按判据布尔输出"已配置/未配置"("未配置"用信息态而非失败态 ——
// 未接入某 agent 是正常选择,不应计为诊断失败)。
func markInstalled(rep *doctorReport, label string, installed bool) {
	if installed {
		rep.line(statePass, label, cliMessage("doctor.installed"))
	} else {
		rep.line(stateInfo, label, cliMessage("doctor.not_installed"))
	}
}

// doctorProbeEndpoint 执行一次端点连通探测(仅 --net;复用 configToLLMOptions 唯一读密钥)。
func doctorProbeEndpoint(rep *doctorReport, cfg *config.Config) {
	opts, keyMissing := configToLLMOptions(cfg)
	if keyMissing {
		rep.line(stateFail, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_key_missing", cfg.AI.APIKeyEnv))
		return
	}
	client, err := llm.NewClient(opts)
	if err != nil {
		rep.line(stateFail, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_client_failed", err))
		return
	}

	timeout := time.Duration(cfg.AI.TimeoutSeconds) * time.Second
	if cfg.AI.TimeoutSeconds <= 0 {
		timeout = llm.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	request, err := aiTestProbeRequest()
	if err != nil {
		rep.line(stateFail, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_prompt_failed", err))
		return
	}

	start := time.Now()
	_, callErr := client.Complete(
		ctx,
		request,
	)
	elapsed := time.Since(start).Round(time.Millisecond)

	if callErr != nil {
		rep.line(stateFail, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_failed", elapsed, callErr))
		return
	}
	rep.line(statePass, cliMessage("doctor.label.endpoint"), cliMessage("doctor.endpoint_succeeded", elapsed))
}

// doctorExitError 承载 doctor 的退出码,交由 root.go 的 Execute 映射为进程退出码。
type doctorExitError struct{ code int }

func (e *doctorExitError) Error() string {
	return cliMessage("doctor.exit_error", e.code)
}

// ExitCode 供退出码映射读取。
func (e *doctorExitError) ExitCode() int { return e.code }
