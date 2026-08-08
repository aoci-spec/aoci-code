// header draft 编排: 仓库画像 → prompt 编译 → 端点调用 → 草稿落盘 → manifest + ledger 落账
// 索引条目: header.go[WWF8M]
//
// 定位(D23): workflow 是 AI 编排层中唯一允许 import internal/llm 的包。
// 但它不构造 Client —— llm.Client 由 CLI 层经 configToLLMOptions(全仓唯一读
// 环境变量密钥处,R19)构造后【注入】本包;本包不接触 os.Getenv、不感知密钥来源。
//
// draft-first(D27/R18): 本包唯一的落盘目标是 .aoci/drafts/<run_id>/ 草稿区;
// 绝不写 aoci.txt、绝不触碰基线 —— apply 是 CLI 层的显式独立动作。
//
// token 计量(D26): 端点返回 usage → exact 记精确值;未返回 → EstimateTokens
// 粗估标 estimated;调用失败 → missing 且照样落账(失败调用的成本缺口正是
// missing 枚举的设计场景)。翻译逻辑与 cli/ai.go 的 appendAITestLedger 同模式。
//
// 采样温度: 头部起草是强结构化任务,显式传低温 headerDraftTemperature 压
// 采样噪声 —— 不显式设定则用端点默认(常为 1.0),遵从度实验将无法归因;
// 实际值记入 manifest 作实验自变量。
//
// prompt 审计快照(cfg.AI.PromptSnapshot,枚举与 config 侧一致 redacted/full/none):
// config 侧定义 redacted 为"源码段脱敏为 SHA 指纹后入快照" —— header draft 的
// prompt 不含任何源码段(仅文件名/统计画像/现有头部),脱敏对象为空,故本场景
// redacted 与 full 行为一致: 快照全文落 <run_id>/prompt.txt 且 manifest 记
// PromptHash;两档的行为分化留给源码段真实出现的工序(v2.2 条目生成)。
// none = 零快照零摘要。空值不可达(config.Load 恒兜底 redacted,留防御分支);
// 其余未知取值按 none 处理并入 Warnings(不替 config 发明语义)。
// 快照落盘失败仅入 Warnings 不中断主流程 —— 审计件缺失不应毁掉草稿本体。
//
// 事实注入纪律(D37/R20): 画像全部来自当轮 inventory 快照与已索引条目路径,
// prompt 层已内嵌"禁止虚构画像之外内容"指令;本包不向 prompt 提供任何
// 记忆性/推断性事实。
package workflow

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/aoci-spec/aoci-code/internal/prompt"
)

// headerDraftTemperature 头部起草的显式采样温度(结构化任务低温压噪声)
const headerDraftTemperature = 0.2

// 画像渲染上限(防超大仓库把 prompt 撑爆;截断即警告,让人看见)
const (
	// maxProfileDirs 目录分布渲染上限(按文件数降序取前 N)
	maxProfileDirs = 30
	// maxProfileExts 扩展名分布渲染上限
	maxProfileExts = 20
	// maxProfileSamples 代表性文件样本上限(取未索引且非跳过项,字典序)
	maxProfileSamples = 40
)

// HeaderDraftResult 一次 header draft 的结果(供 CLI 层渲染输出)
type HeaderDraftResult struct {
	// RunID 本次草稿批次 ID(草稿位于 .aoci/drafts/<RunID>/header.txt)
	RunID string
	// HeaderText 清理后的草稿头部文本(已剥围栏;与落盘内容一致)
	HeaderText string
	// Usage 本次调用的 token 用量(Source 指明可信度,与 manifest/ledger 一致)
	Usage llm.TokenUsage
	// Warnings 非致命警告(结构预检未过/画像截断/length 截断/快照失败等),已同步进 manifest
	Warnings []string
}

// RunHeaderDraft 执行一次 header draft 全链路。
// client 由 CLI 层构造注入(密钥纪律见包注释);cfg 提供 AI 参数与排除规则;
// doc 为当前索引解析结果(读文件归调用方)。
// 成败均落 ledger(op=header_draft,source=cli_ai);仅成功时产生草稿与 manifest。
func RunHeaderDraft(ctx context.Context, root string, cfg *config.Config, doc *index.Document, client *llm.Client) (*HeaderDraftResult, error) {
	if client == nil {
		return nil, errors.New("llm 客户端未注入(内部错误)")
	}
	start := time.Now()

	// —— 1. 仓库画像(确定性): inventory 差集 + 已索引条目路径的并集统计 ——
	in, profileWarns, err := buildHeaderInput(root, cfg, doc)
	if err != nil {
		return nil, fmt.Errorf("构建仓库画像失败: %w", err)
	}

	// —— 2. prompt 编译(确定性纯函数)——
	sysText, userText, err := prompt.BuildHeaderMessages(in)
	if err != nil {
		return nil, fmt.Errorf("编译 prompt 失败: %w", err)
	}

	// —— 3. 端点调用(唯一的网络动作;只触达用户配置端点;显式低温)——
	temp := headerDraftTemperature
	res, callErr := client.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: sysText},
			{Role: "user", Content: userText},
		},
		Temperature: &temp,
	})
	elapsed := time.Since(start)

	// 成败均落账的事件骨架(D26)
	ev := ledger.Event{
		Op:           "header_draft",
		Source:       ledger.SourceCLIAI,
		DurationMs:   elapsed.Milliseconds(),
		Model:        cfg.AI.Model,
		Provider:     cfg.AI.Provider,
		EndpointHash: ledger.EndpointHash(cfg.AI.BaseURL),
	}
	if callErr != nil {
		// 失败: token 无从得知 —— 诚实标 missing,照样落账后报错
		ev.TokenSrc = ledger.TokenSourceMissing
		ev.WarningsCount = 1
		ledger.Append(root, cfg.LedgerEnabled, ev)
		return nil, fmt.Errorf("端点调用失败: %w", callErr)
	}

	// —— 4. 草稿清理与结构预检 ——
	headerText := index.StripFences(res.Text)
	warnings := append([]string{}, profileWarns...)
	if strings.TrimSpace(headerText) == "" {
		// 空草稿视为失败: token 已消耗,如实落账后报错
		fillTokenFields(&ev, res, sysText+userText)
		ev.WarningsCount = len(warnings) + 1
		ledger.Append(root, cfg.LedgerEnabled, ev)
		return nil, errors.New("端点返回空草稿(清理围栏后无内容)")
	}
	// 结构预检: 警告不阻断落草稿 —— 草稿区允许带病(让人看到模型原始产出),
	// apply 阶段才硬拒(闸门在 CLI 层)。
	if ln, msg := index.ValidateHeaderText(headerText); ln > 0 {
		warnings = append(warnings, fmt.Sprintf("结构预检未过(apply 将被拒绝,需人工修正): 第 %d 行: %s", ln, msg))
	}
	if res.FinishReason == "length" {
		warnings = append(warnings, "端点以 length 截断结束: 草稿可能不完整,建议人工检查或调大端点输出上限")
	}

	// —— 5. 草稿落盘 + prompt 审计(redacted/full/none) + manifest + 落账 ——
	runID, err := draft.NewRun(root)
	if err != nil {
		return nil, err
	}
	if err := draft.WriteFile(root, runID, draft.HeaderFileName, []byte(headerText+"\n")); err != nil {
		return nil, fmt.Errorf("草稿落盘失败: %w", err)
	}

	files := []string{draft.HeaderFileName}
	promptHash := ""
	switch cfg.AI.PromptSnapshot {
	case "redacted", "full", "":
		// header prompt 无源码段,redacted 脱敏对象为空 ≡ full;空值防御同 redacted
		// (config.Load 恒兜底 redacted,空值实际不可达)
		promptHash = ledger.EndpointHash(sysText + userText)
		snapshot := "===== system =====\n" + sysText + "\n\n===== user =====\n" + userText + "\n"
		if werr := draft.WriteFile(root, runID, draft.PromptFileName, []byte(snapshot)); werr != nil {
			// 审计件缺失不毁草稿本体,以警告可见
			warnings = append(warnings, "prompt 审计快照落盘失败(草稿本体不受影响): "+werr.Error())
		} else {
			files = append(files, draft.PromptFileName)
		}
	case "none":
		// 零快照零摘要
	default:
		warnings = append(warnings, fmt.Sprintf("prompt_snapshot 取值未知(%q,期望 redacted/full/none): 按 none 处理零痕迹", cfg.AI.PromptSnapshot))
	}

	fillTokenFields(&ev, res, sysText+userText)
	ev.DraftRunID = runID
	ev.WarningsCount = len(warnings)

	tempForManifest := headerDraftTemperature
	m := &draft.Manifest{
		RunID: runID, Kind: draft.KindHeader,
		Model: cfg.AI.Model, Provider: cfg.AI.Provider,
		EndpointHash: ledger.EndpointHash(cfg.AI.BaseURL),
		Temperature:  &tempForManifest,
		PromptHash:   promptHash,
		InputTokens:  ev.InputTokens, OutputTokens: ev.OutputTokens,
		TokenSource: ev.TokenSrc,
		Warnings:    warnings,
		Files:       files,
	}
	if err := draft.SaveManifest(root, m); err != nil {
		return nil, fmt.Errorf("manifest 落盘失败: %w", err)
	}
	ledger.Append(root, cfg.LedgerEnabled, ev)

	return &HeaderDraftResult{
		RunID: runID, HeaderText: headerText,
		Usage:    res.Usage,
		Warnings: warnings,
	}, nil
}

// fillTokenFields 按 D26 填充 Event 的 token 字段(与 cli/ai.go 同模式的翻译):
// 端点 usage=exact 记精确值;否则本地粗估标 estimated(输入按发送文本,输出按响应文本)。
func fillTokenFields(ev *ledger.Event, res *llm.CompletionResult, sentText string) {
	if res != nil && res.Usage.Source == llm.TokenSourceExact {
		ev.TokenSrc = ledger.TokenSourceExact
		ev.InputTokens = res.Usage.InputTokens
		ev.OutputTokens = res.Usage.OutputTokens
		return
	}
	ev.TokenSrc = ledger.TokenSourceEstimated
	ev.InputTokens = ledger.EstimateTokens(sentText)
	if res != nil {
		ev.OutputTokens = ledger.EstimateTokens(res.Text)
	}
}

// buildHeaderInput 构建 prompt.HeaderInput(确定性)。
// 唯一数据源 = indexgen.BuildInventory(与 scan/verify 同口径遍历):
//   - DiskTotal 为文件总数;
//   - Items(未索引差集,BuildInventory 已就地填充 doc 内条目 RelPath)与
//     已索引条目路径取并集,聚合目录/扩展名分布;
//   - 样本取未索引且非跳过项(模型最需要"看见"的正是索引还没覆盖的部分)。
//
// 路径统一用 path 包(正斜杠语义)处理 —— RelPath 恒为正斜杠形态。
func buildHeaderInput(root string, cfg *config.Config, doc *index.Document) (prompt.HeaderInput, []string, error) {
	var warns []string

	inv, err := indexgen.BuildInventory(root, cfg, doc)
	if err != nil {
		return prompt.HeaderInput{}, nil, err
	}

	dirCount := map[string]int{}
	extCount := map[string]int{}
	addPath := func(rel string) {
		if rel == "" || strings.HasSuffix(rel, "/") {
			return // 目录条目不参与文件统计
		}
		d := path.Dir(rel)
		if d == "" {
			d = "."
		}
		dirCount[d]++
		ext := strings.ToLower(path.Ext(rel))
		if ext == "" {
			ext = "(无)"
		}
		extCount[ext]++
	}

	// 并集聚合: 已索引条目路径 ∪ inventory 未索引差集(seen 去重)
	seen := map[string]bool{}
	for _, sec := range doc.Sections {
		for _, e := range sec.Entries {
			if e.RelPath != "" && !seen[e.RelPath] {
				seen[e.RelPath] = true
				addPath(e.RelPath)
			}
		}
	}
	var samples []string
	for _, it := range inv.Items {
		if !seen[it.RelPath] {
			seen[it.RelPath] = true
			addPath(it.RelPath)
		}
		if it.SkipReason == "" {
			samples = append(samples, it.RelPath)
		}
	}

	dirs := sortCounts(dirCount)
	exts := sortCounts(extCount)
	if len(dirs) > maxProfileDirs {
		warns = append(warns, fmt.Sprintf("目录分布超 %d 项,已按文件数取前 %d(画像截断)", maxProfileDirs, maxProfileDirs))
		dirs = dirs[:maxProfileDirs]
	}
	if len(exts) > maxProfileExts {
		warns = append(warns, fmt.Sprintf("扩展名分布超 %d 项,已截断", maxProfileExts))
		exts = exts[:maxProfileExts]
	}
	sort.Strings(samples)
	if len(samples) > maxProfileSamples {
		samples = samples[:maxProfileSamples]
	}

	// 现有头部原文: 经 ExtractHeader 从 doc.RawText 提取(Parse 已存全文,
	// 与解析侧对"头部"的边界判定同源 —— headerBoundary 共用同一对正则)
	currentHeader, _ := index.ExtractHeader(doc.RawText)

	// 项目名以仓库根目录名兜底(prompt 层空值回退"未命名项目",此处尽量给真值)
	rootSlash := strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")
	name := path.Base(rootSlash)

	pd := make([]prompt.DirCount, len(dirs))
	for i, c := range dirs {
		pd[i] = prompt.DirCount{Dir: c.key, Count: c.n}
	}
	pe := make([]prompt.ExtCount, len(exts))
	for i, c := range exts {
		pe[i] = prompt.ExtCount{Ext: c.key, Count: c.n}
	}

	return prompt.HeaderInput{
		ProjectName:   name,
		RepoRootSlash: rootSlash + "/",
		CurrentHeader: currentHeader,
		TotalFiles:    inv.DiskTotal,
		Dirs:          pd,
		Exts:          pe,
		SampleFiles:   samples,
	}, warns, nil
}

// kv 计数聚合的中间结构
type kv struct {
	key string
	n   int
}

// sortCounts 计数 map → 按数量降序、同数量按键字典序的稳定切片(确定性输出)
func sortCounts(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, n := range m {
		out = append(out, kv{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].key < out[j].key
	})
	return out
}
