// 索引条目: ai.go[CAI8M]
// 职责: 提供 `aoci ai` 命令组(status/setup/test),是 AI 增强层的用户入口。
//   - status: 只读展示当前 AI 配置状态(绝不打印密钥)
//   - setup:  写入 AI 配置(config.json 或 --local 的 config.local.json);只存环境变量名,不存密钥
//   - test:   真实连通性探测(读环境变量密钥 → 调用户配置端点 → 报告结果 → 落 ledger)
//
// 密钥纪律(R19):
//
//	configToLLMOptions 是【唯一】读取环境变量密钥的函数。密钥读出后仅存于内存中的
//	llm.Options,传给 llm.Client 后用完即弃 —— 绝不进任何输出、日志、错误信息或落盘内容。
//	命令行接口层面【不提供】传入密钥值的选项(只有 --key-env 环境变量名),从接口上杜绝
//	密钥出现在 shell history。
//
// 数据主权(边界二):
//
//	test 命令只向 cfg.AI.BaseURL(用户配置端点)发送探测请求,不触达任何其他地址。
//
// ledger v2 落账(D26):
//
//	test 的成败均落账(op=ai_test,source=cli_ai)。token 定级诚实三分:
//	端点返回 usage → exact 记精确值;未返回 → EstimateTokens 粗估标 estimated;
//	调用失败 → missing(无从得知,不编造)。端点入账仅以 EndpointHash 脱敏哈希形态,
//	绝不落 URL 原文(内网端点属基础设施信息,防匿名化导出泄露)。
//
// 失败引导单点(P-12,2026-07-12 httpx 实弹): renderAIFailureHint 是按 llm.Error
// 类别产出可操作排查提示的【唯一】实现 —— 实弹中超时发生在 index build/draft
// 批量链路,错误裸冒泡无任何调参引导(4 分钟耗在排障),而完整引导只接线在
// ai test。修法: 引导提为纯函数,ai test(printTestFailureHint 薄壳)与 index
// 命令组的 AI 错误路径共用,绝不各写一份(D59 同哲学)。
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/spf13/cobra"
)

// newAICmd 构造 `aoci ai` 父命令及其子命令。
func newAICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: cliMessage("cli.short.ai"),
		Long:  aiLongHelp(),
	}
	cmd.AddCommand(newAIStatusCmd())
	cmd.AddCommand(newAISetupCmd())
	cmd.AddCommand(newAITestCmd())
	return cmd
}

// newAIStatusCmd 构造 `aoci ai status`。
func newAIStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: cliMessage("cli.short.ai_status"),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			ai := cfg.AI

			fmt.Fprintln(out, cliMessage("ai.status.title"))
			fmt.Fprintln(out, "──────────────────────────────")
			if cfg.IsAIEnabled() {
				fmt.Fprintln(out, cliMessage("ai.status.enabled"))
			} else if ai.Enabled && ai.BaseURL == "" {
				fmt.Fprintln(out, cliMessage("ai.status.enabled_no_url"))
			} else {
				fmt.Fprintln(out, cliMessage("ai.status.disabled"))
			}
			fmt.Fprint(out, cliMessage("ai.status.provider", orDash(ai.Provider)))
			fmt.Fprint(out, cliMessage("ai.status.base_url", orDash(ai.BaseURL)))
			fmt.Fprint(out, cliMessage("ai.status.model", orDash(ai.Model)))

			// 密钥状态: 只报告"有/无",绝不显示密钥本身、长度或任何片段
			if ai.APIKeyEnv == "" {
				fmt.Fprintln(out, cliMessage("ai.status.key_not_required"))
			} else {
				keyState := cliMessage("ai.status.key_unset")
				if os.Getenv(ai.APIKeyEnv) != "" {
					keyState = cliMessage("ai.status.key_set")
				}
				fmt.Fprint(out, cliMessage("ai.status.key_env", ai.APIKeyEnv, keyState))
			}

			fmt.Fprint(out, cliMessage("ai.status.timeout", intOrDefault(ai.TimeoutSeconds, cliMessage("ai.value.default"))))
			fmt.Fprint(out, cliMessage("ai.status.concurrency", intOrDefault(ai.MaxConcurrency, cliMessage("ai.value.default"))))
			fmt.Fprint(out, cliMessage("ai.status.input_limit", intOrUnlimited(ai.MaxInputTokens)))
			fmt.Fprint(out, cliMessage("ai.status.accounting", orDash(ai.TokenAccounting)))
			fmt.Fprint(out, cliMessage("ai.status.snapshot", orDash(ai.PromptSnapshot)))

			if !cfg.IsAIEnabled() {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, cliMessage("ai.status.setup_hint"))
			}
			return nil
		},
	}
}

// newAISetupCmd 构造 `aoci ai setup`。
func newAISetupCmd() *cobra.Command {
	var (
		baseURL     string
		model       string
		keyEnv      string
		provider    string
		timeout     int
		concurrency int
		maxInput    int
		accounting  string
		snapshot    string
		enable      bool
		disable     bool
		local       bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: cliMessage("cli.short.ai_setup"),
		Long:  aiSetupLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			// 层泄漏修法(P1-c,2026-07-10): 按目标层取源 ——
			// 写团队层(默认)用 LoadBase(不合并 local,改后 Save 零泄漏);
			// 写 local 层(--local)用 Load 合并态(SaveLocal 只取 version/ai 两键)。
			// 事故: 旧版恒用 Load 合并态,不带 --local 时整个 local ai 块
			// (含个人端点 base_url)随 Save 写入待提交的团队 config.json。
			var cfg *config.Config
			if local {
				cfg, err = config.Load(repoRoot)
			} else {
				cfg, err = config.LoadBase(repoRoot)
			}
			if err != nil {
				return err
			}

			// 若无任何有效参数,打印用法提示(避免空 setup 静默无操作)
			if !cmd.Flags().Changed("base-url") && !cmd.Flags().Changed("model") &&
				!cmd.Flags().Changed("key-env") && !cmd.Flags().Changed("provider") &&
				!cmd.Flags().Changed("timeout") && !cmd.Flags().Changed("concurrency") &&
				!cmd.Flags().Changed("max-input") && !cmd.Flags().Changed("token-accounting") &&
				!cmd.Flags().Changed("prompt-snapshot") && !enable && !disable {
				return fmt.Errorf("%s", cliMessage("ai.setup.no_options"))
			}

			if enable && disable {
				return fmt.Errorf("%s", cliMessage("ai.setup.enable_conflict"))
			}

			// 仅覆盖用户显式传入的字段(Changed 判断),未传入的保持原值
			if cmd.Flags().Changed("base-url") {
				cfg.AI.BaseURL = baseURL
			}
			if cmd.Flags().Changed("model") {
				cfg.AI.Model = model
			}
			if cmd.Flags().Changed("key-env") {
				cfg.AI.APIKeyEnv = keyEnv
			}
			if cmd.Flags().Changed("provider") {
				cfg.AI.Provider = provider
			}
			if cmd.Flags().Changed("timeout") {
				cfg.AI.TimeoutSeconds = timeout
			}
			if cmd.Flags().Changed("concurrency") {
				cfg.AI.MaxConcurrency = concurrency
			}
			if cmd.Flags().Changed("max-input") {
				cfg.AI.MaxInputTokens = maxInput
			}
			if cmd.Flags().Changed("token-accounting") {
				cfg.AI.TokenAccounting = accounting
			}
			if cmd.Flags().Changed("prompt-snapshot") {
				cfg.AI.PromptSnapshot = snapshot
			}
			if enable {
				cfg.AI.Enabled = true
			}
			if disable {
				cfg.AI.Enabled = false
			}

			// 落盘: --local 写 config.local.json,否则写 config.json
			if local {
				if err := config.SaveLocal(repoRoot, cfg); err != nil {
					return err
				}
			} else {
				if err := config.Save(repoRoot, cfg); err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()
			target := cliMessage("ai.setup.team_config")
			if local {
				target = cliMessage("ai.setup.local_config")
			}
			fmt.Fprint(out, cliMessage("ai.setup.written", target))
			if cfg.AI.Enabled && cfg.AI.BaseURL == "" {
				fmt.Fprintln(out, cliMessage("ai.setup.enabled_no_url"))
			}
			fmt.Fprintln(out, cliMessage("ai.setup.next"))
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "", cliMessage("cli.flag.ai_base_url"))
	cmd.Flags().StringVar(&model, "model", "", cliMessage("cli.flag.ai_model"))
	cmd.Flags().StringVar(&keyEnv, "key-env", "", cliMessage("cli.flag.ai_key_env"))
	cmd.Flags().StringVar(&provider, "provider", "", cliMessage("cli.flag.ai_provider"))
	cmd.Flags().IntVar(&timeout, "timeout", 0, cliMessage("cli.flag.ai_timeout"))
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, cliMessage("cli.flag.ai_concurrency"))
	cmd.Flags().IntVar(&maxInput, "max-input", 0, cliMessage("cli.flag.ai_max_input"))
	cmd.Flags().StringVar(&accounting, "token-accounting", "", cliMessage("cli.flag.ai_accounting"))
	cmd.Flags().StringVar(&snapshot, "prompt-snapshot", "", cliMessage("cli.flag.ai_snapshot"))
	cmd.Flags().BoolVar(&enable, "enable", false, cliMessage("cli.flag.ai_enable"))
	cmd.Flags().BoolVar(&disable, "disable", false, cliMessage("cli.flag.ai_disable"))
	cmd.Flags().BoolVar(&local, "local", false, cliMessage("cli.flag.ai_local"))
	return cmd
}

// appendAITestLedger 为一次 ai test 落一条 ledger v2 事件(D26 分级计量)。
//   - 成功且端点返回 usage: token_source=exact,记端点精确值;
//   - 成功但端点未返回 usage: token_source=estimated,输入侧按探测消息、输出侧按响应文本
//     经 ledger.EstimateTokens 粗估;
//   - 失败: token_source=missing(无从得知,不编造),warnings_count=1。
//
// 端点仅以 EndpointHash 脱敏哈希入账,绝不落 URL 原文。
// Append 自身故障免疫(内部消化),本函数不影响命令结果。
func appendAITestLedger(
	root string,
	cfg *config.Config,
	request llm.CompletionRequest,
	res *llm.CompletionResult,
	callErr error,
	elapsed time.Duration,
) {
	ev := ledger.Event{
		Op:           "ai_test",
		Source:       ledger.SourceCLIAI,
		DurationMs:   elapsed.Milliseconds(),
		Model:        cfg.AI.Model,
		Provider:     cfg.AI.Provider,
		EndpointHash: ledger.EndpointHash(cfg.AI.BaseURL),
	}
	switch {
	case callErr != nil:
		// 失败: token 无从得知 —— 诚实标 missing,绝不编造
		ev.TokenSrc = ledger.TokenSourceMissing
		ev.WarningsCount = 1
	case res != nil && res.Usage.Source == llm.TokenSourceExact:
		// 端点返回精确 usage
		ev.TokenSrc = ledger.TokenSourceExact
		ev.InputTokens = res.Usage.InputTokens
		ev.OutputTokens = res.Usage.OutputTokens
	default:
		// 端点未返回 usage: 本地粗估兜底,诚实标 estimated
		ev.TokenSrc = ledger.TokenSourceEstimated
		var inText string
		for _, m := range request.Messages {
			inText += m.Content
		}
		ev.InputTokens = ledger.EstimateTokens(inText)
		if res != nil {
			ev.OutputTokens = ledger.EstimateTokens(res.Text)
		}
	}
	ledger.Append(root, cfg.LedgerEnabled, ev)
}

// newAITestCmd 构造 `aoci ai test`。
func newAITestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: cliMessage("cli.short.ai_test"),
		Long:  aiTestLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if cfg.AI.BaseURL == "" {
				return fmt.Errorf("%s", cliMessage("ai.test.base_url_missing"))
			}
			if cfg.AI.Model == "" {
				return fmt.Errorf("%s", cliMessage("ai.test.model_missing"))
			}

			// 翻译配置 + 读取环境变量密钥(唯一读密钥处)
			opts, keyMissing := configToLLMOptions(cfg)
			if keyMissing {
				return fmt.Errorf("%s", cliMessage(
					"ai.key_env_missing",
					cfg.AI.APIKeyEnv,
					cfg.AI.APIKeyEnv,
				))
			}

			client, err := llm.NewClient(opts)
			if err != nil {
				return err
			}

			fmt.Fprint(out, cliMessage("ai.test.start", cfg.AI.BaseURL, cfg.AI.Model))

			request, err := aiTestProbeRequest()
			if err != nil {
				return err
			}

			// 最小探测请求
			timeout := time.Duration(cfg.AI.TimeoutSeconds) * time.Second
			if cfg.AI.TimeoutSeconds <= 0 {
				timeout = llm.DefaultTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			start := time.Now()
			res, callErr := client.Complete(
				ctx,
				request,
			)
			elapsed := time.Since(start)

			// 成败均落账(D26: 失败调用的成本缺口正是 missing 枚举的设计场景)
			appendAITestLedger(repoRoot, cfg, request, res, callErr, elapsed)

			if callErr != nil {
				// 按错误类别给针对性提示
				fmt.Fprint(out, cliMessage("ai.test.failed", elapsed.Round(time.Millisecond)))
				printTestFailureHint(out, cfg, callErr)
				return callErr
			}

			fmt.Fprint(out, cliMessage("ai.test.succeeded", elapsed.Round(time.Millisecond)))
			fmt.Fprint(out, cliMessage("ai.test.response", truncateForDisplay(res.Text, 80)))
			fmt.Fprint(out, cliMessage("ai.test.finish_reason", orDash(res.FinishReason)))
			if res.Usage.Source == llm.TokenSourceExact {
				fmt.Fprint(out, cliMessage(
					"ai.test.tokens_exact",
					res.Usage.InputTokens,
					res.Usage.OutputTokens,
					res.Usage.TotalTokens,
				))
			} else {
				fmt.Fprintln(out, cliMessage("ai.test.tokens_estimated"))
			}
			return nil
		},
	}
}

// configToLLMOptions 把 AI 配置翻译为 llm.Options,并从环境变量读取真实密钥。
// 【唯一】读取密钥处(R19)。返回 keyMissing=true 表示配置要求密钥但环境变量为空。
// 内网免认证(APIKeyEnv 为空)时 APIKey 留空、keyMissing=false。
func configToLLMOptions(cfg *config.Config) (llm.Options, bool) {
	var apiKey string
	keyMissing := false
	if cfg.AI.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.AI.APIKeyEnv)
		if apiKey == "" {
			keyMissing = true
		}
	}

	timeout := time.Duration(cfg.AI.TimeoutSeconds) * time.Second
	if cfg.AI.TimeoutSeconds <= 0 {
		timeout = 0 // 交由 llm.NewClient 回退 DefaultTimeout
	}

	return llm.Options{
		BaseURL:        cfg.AI.BaseURL,
		Model:          cfg.AI.Model,
		APIKey:         apiKey,
		Timeout:        timeout,
		MaxInputTokens: cfg.AI.MaxInputTokens,
	}, keyMissing
}

// renderAIFailureHint 依据 llm.Error 类别产出可操作排查提示(P-12 单点实现)。
// ai test 与 index 命令组的 AI 错误路径共用 —— 引导文案绝不各写一份(D59)。
// 超时分支必须携带调参路径(实弹: 慢端点+批量起草超时,裸报错致 4 分钟排障);
// 非 llm.Error 原样返回错误文本。
func renderAIFailureHint(cfg *config.Config, err error) string {
	var e *llm.Error
	if !errors.As(err, &e) {
		return cliMessage("ai.failure.generic", localeSafeCLIDetail(err.Error()))
	}
	switch e.Kind {
	case llm.KindAuth:
		if cfg.AI.APIKeyEnv == "" {
			return cliMessage("ai.failure.auth_no_env")
		}
		return cliMessage("ai.failure.auth", cfg.AI.APIKeyEnv, localeSafeCLIDetail(e.Error()))
	case llm.KindNetwork:
		return cliMessage("ai.failure.network", cfg.AI.BaseURL, localeSafeCLIDetail(e.Error()))
	case llm.KindTimeout:
		cur := cliMessage("ai.failure.timeout_default")
		if cfg.AI.TimeoutSeconds > 0 {
			cur = cliMessage("ai.failure.timeout_configured", cfg.AI.TimeoutSeconds)
		}
		return cliMessage("ai.failure.timeout", cur, localeSafeCLIDetail(e.Error()))
	case llm.KindRateLimit:
		return cliMessage("ai.failure.rate_limit", localeSafeCLIDetail(e.Error()))
	case llm.KindServer:
		return cliMessage("ai.failure.server", localeSafeCLIDetail(e.Error()))
	default:
		return cliMessage("ai.failure.generic", localeSafeCLIDetail(e.Error()))
	}
}

// printTestFailureHint 依据错误类别打印可操作的排查提示(renderAIFailureHint 薄壳)。
func printTestFailureHint(out io.Writer, cfg *config.Config, err error) {
	hint := renderAIFailureHint(cfg, err)
	if !strings.HasSuffix(hint, "\n") {
		hint += "\n"
	}
	fmt.Fprint(out, hint)
}

// —— 小工具(仅本文件展示用)——

func orDash(s string) string {
	if s == "" {
		return cliMessage("ai.value.unset")
	}
	return s
}

func intOrDefault(n int, def string) string {
	if n <= 0 {
		return def
	}
	return fmt.Sprintf("%d", n)
}

func intOrUnlimited(n int) string {
	if n <= 0 {
		return cliMessage("ai.value.unlimited")
	}
	return fmt.Sprintf("%d", n)
}

func truncateForDisplay(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
