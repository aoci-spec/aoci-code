// MCP server 装配 / 工具注册 / 错误分类 / stdio 运行
// 索引条目: server.go[MSV9OS]
//
// 铁律: stdio 模式下 stdout 只出 JSON-RPC 协议流,一切日志走 stderr ——
// 任何 println 残留都会毒化协议流导致 client 静默断连(main.go 入口已固定 log→stderr)。
//
// 纪律:
//   - 九工具名固定(aoci_rules/aoci_overview/aoci_get_entries/aoci_search/
//     aoci_update_entry/aoci_report/aoci_remove_entry/aoci_header/aoci_maintain),
//     改名破坏 AGENTS.md 模板与实验可复现性;
//   - 每次工具调用现读 index/baseline/config,不做进程内缓存 —— 索引会被 MCP 外通道
//     (编辑器手改/git pull)修改,缓存视图违反「过期索引比没有更危险」第一戒律;
//   - 参数全 string/[]string,返回纯文本;
//   - handler 内 panic 经 guard 恢复: stderr 记录 + 返回 MCP error 结果,不崩进程;
//   - 错误响应含下一步建议,不含 Go stack trace。
//
// 错误模型(审查重构): Fail{Code,Msg,Hint} 为分类错误的唯一事实源,
// loadRepoCtx 原生返回 *Fail,MCP 侧经 failResult 渲染为错误文本,CLI 侧自行渲染 ——
// 单向"结构→文案",禁止任何"解析文案反推结构"的逆向耦合。
package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/aoci-spec/aoci-code/textassets"
)

// 错误分类码(Fail.Code 取值;进错误文案首行便于遥测与排查,亦为 CLI 退出码分流依据)
const (
	errNotInitialized               = "not_initialized"  // 索引不存在: 未 init
	errIndexInvalid                 = "index_invalid"    // 索引不可解析
	errPathUnsafe                   = "path_unsafe"      // 路径校验拒绝
	errWriteConflict                = "write_conflict"   // 替换定位 0处/2+处
	errVolumeReadOnly               = "volume_read_only" // unsupported Volume writes remain read-only
	errCrossVolumeWriteNotSupported = "cross_volume_write_not_supported"
	errCrossVolumeGuardRequired     = "cross_volume_guard_required"
	errImpactResolutionFailed       = "impact_resolution_failed"
	errCandidateInvalid             = "candidate_invalid"
	errCognitionSnapshotUnavailable = "cognition_snapshot_unavailable"
	errOnboardingInProgress         = "onboarding_in_progress"
	errOnboardingStateInvalid       = "onboarding_state_invalid"
	errBadArgs                      = "bad_args" // 参数不合法
	errInternal                     = "internal" // 兜底
)

// Fail 分类错误(分类码 + 可操作说明 + 下一步建议)—— 错误信息的结构化事实源,
// 供 MCP handler 与 CLI 调用方各自渲染;跨包比对请使用 Code 字符串字面量
// (取值即上方常量,属稳定契约,出现在对外错误文案首行)。
type Fail struct {
	Code                string
	Msg                 string
	Hint                string
	Findings            []cognition.RepairFinding
	Repairable          bool
	FormalWritesStarted bool
	GlobalStop          *GlobalStopFacts
	CodePlan            *codebatch.Plan
	OnboardingRoute     *onboarding.Route
}

// GlobalStopFacts describes an asset-level prerequisite failure that no
// candidate in the submitted batch can repair. It deliberately remains
// separate from cognition.RepairFinding so candidate indexes are never faked.
type GlobalStopFacts struct {
	AffectedAsset  string `json:"affected_asset"`
	Field          string `json:"field"`
	RuleCode       string `json:"rule_code"`
	Expected       string `json:"expected"`
	Actual         string `json:"actual"`
	Cause          string `json:"cause"`
	SafeNextAction string `json:"safe_next_action"`
}

// textResult 纯文本成功结果
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// errResult 纯文本错误结果(IsError=true;含分类码与下一步建议)
func errResult(code, msg, hint string) *mcp.CallToolResult {
	text := "[" + code + "] " + msg
	if hint != "" {
		text += "\n" + mcpMessage("mcp.error.hint_prefix") + hint
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// failResult Fail → MCP 错误结果的唯一渲染点
func failResult(f *Fail) *mcp.CallToolResult {
	if f != nil && f.OnboardingRoute != nil {
		return onboardingRouteResult(f.OnboardingRoute, true)
	}
	return errResult(f.Code, f.Msg, f.Hint)
}

// onboardingRouteResult mirrors the versioned route into both MCP structured
// content and text content. Hosts that do not yet expose structuredContent can
// therefore follow the same machine facts without changing the nine-tool input
// schemas or the tools/list contract.
func onboardingRouteResult(route *onboarding.Route, isError bool) *mcp.CallToolResult {
	data, err := json.Marshal(route)
	if err != nil {
		return errResult(errInternal, mcpMessage("mcp.error.internal_recovered"), "")
	}
	return &mcp.CallToolResult{
		IsError:           isError,
		StructuredContent: route,
		Content: []mcp.Content{&mcp.TextContent{
			Text: string(data) + "\n",
		}},
	}
}

// activeFreshRouteGuardResult is the fail-closed pre-input/pre-write guard for
// MCP write orchestration. Its three outcomes are: an active Fresh route, a
// Recovery/state-inspection error result, or nil when no onboarding state owns
// the call. No caller may treat inspection failure as "no active route".
func activeFreshRouteGuardResult(root string) *mcp.CallToolResult {
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return errResult(
			errCognitionSnapshotUnavailable,
			mcpMessage("overview.delivery.recovery_inspection_failed", localeSafeMCPDetail(err.Error())),
			"",
		)
	}
	if len(pending) != 0 {
		return errResult(
			errCognitionSnapshotUnavailable,
			mcpMessage("overview.delivery.pending_recovery", pending[0].Filename),
			mcpMessage("overview.delivery.pending_recovery_hint"),
		)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return errResult(errIndexInvalid, mcpMessage("mcp.error.invalid_config", localeSafeMCPDetail(err.Error())), mcpMessage("mcp.error.invalid_config_hint"))
	}
	executable, err := os.Executable()
	if err != nil {
		return errResult(errInternal, mcpMessage("mcp.error.internal_recovered"), "")
	}
	paths := config.AOCIPaths(root, cfg.IndexPath)
	route, active, err := onboarding.InspectActiveFreshRoute(root, paths.IndexPath, executable)
	if err != nil {
		if errors.Is(err, onboarding.ErrRouteRecoveryPending) ||
			errors.Is(err, onboarding.ErrRouteRecoveryInspection) {
			return errResult(errCognitionSnapshotUnavailable, localeSafeMCPDetail(err.Error()), "")
		}
		return errResult(errOnboardingStateInvalid, localeSafeMCPDetail(err.Error()), "")
	}
	if !active {
		return nil
	}
	return onboardingRouteResult(route, true)
}

// guard 包裹 handler 主体: panic 恢复为 MCP error 结果,stderr 记录,进程不崩
func guard(fn func() *mcp.CallToolResult) (res *mcp.CallToolResult) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, mcpMessage("mcp.error.panic_recovered_log"))
			res = errResult(
				errInternal,
				mcpMessage("mcp.error.internal_recovered"),
				mcpMessage("mcp.error.internal_recovery"),
			)
		}
	}()
	return fn()
}

// repoCtx 一次工具调用的仓库上下文(每次调用现读,用后即弃)
type repoCtx struct {
	cfg   *config.Config
	paths config.Paths
	text  string          // 索引原文
	doc   *index.Document // 已 ResolveRelPaths
	bl    *baseline.Baseline
}

type cognitionRepoCtx struct {
	cfg   *config.Config
	paths config.Paths
	set   *cognition.Set
	bl    *baseline.Baseline
}

func (loaded *cognitionRepoCtx) legacyRepo() *repoCtx {
	return &repoCtx{cfg: loaded.cfg, paths: loaded.paths, text: string(loaded.set.Root.Raw), doc: loaded.set.Root.Document, bl: loaded.bl}
}

// loadCognitionCtx is the only physical-layout loader used by MCP callers.
// Legacy is adapted to CognitionSet; Volumes v1 never falls through to the
// monolithic parser.
func loadCognitionCtx(root string) (*cognitionRepoCtx, *Fail) {
	if pending, err := cognitiontxn.Pending(root); err != nil {
		return nil, &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage(
			"overview.delivery.recovery_inspection_failed", localeSafeMCPDetail(err.Error()),
		)}
	} else if len(pending) > 0 {
		return nil, &Fail{
			Code: errCognitionSnapshotUnavailable,
			Msg:  mcpMessage("overview.delivery.pending_recovery", pending[0].Filename),
			Hint: mcpMessage("overview.delivery.pending_recovery_hint"),
		}
	}
	readOnlyCfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.invalid_config", localeSafeMCPDetail(err.Error())), Hint: mcpMessage("mcp.error.invalid_config_hint")}
	}
	readOnlyPaths := config.AOCIPaths(root, readOnlyCfg.IndexPath)
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return nil, &Fail{Code: errInternal, Msg: mcpMessage("mcp.error.internal_recovered")}
	}
	route, active, routeErr := onboarding.InspectActiveFreshRoute(root, readOnlyPaths.IndexPath, executable)
	if routeErr != nil {
		if errors.Is(routeErr, onboarding.ErrRouteRecoveryPending) ||
			errors.Is(routeErr, onboarding.ErrRouteRecoveryInspection) {
			return nil, &Fail{Code: errCognitionSnapshotUnavailable, Msg: localeSafeMCPDetail(routeErr.Error())}
		}
		return nil, &Fail{Code: errOnboardingStateInvalid, Msg: localeSafeMCPDetail(routeErr.Error())}
	}
	if active {
		return nil, &Fail{Code: errOnboardingInProgress, Msg: errOnboardingInProgress, OnboardingRoute: route}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.invalid_config", localeSafeMCPDetail(err.Error())), Hint: mcpMessage("mcp.error.invalid_config_hint")}
	}
	paths := config.AOCIPaths(root, cfg.IndexPath)
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &Fail{Code: errNotInitialized, Msg: mcpMessage("mcp.error.index_missing", paths.IndexPath), Hint: mcpMessage("mcp.error.index_missing_hint")}
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return nil, &Fail{Code: errInternal, Msg: mcpMessage("mcp.error.index_read", localeSafeMCPDetail(err.Error()))}
		}
		fail := &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.cognition_invalid", localeSafeMCPDetail(err.Error())), Hint: mcpMessage("mcp.error.cognition_invalid_hint")}
		var validationErr *cognition.ValidationError
		if errors.As(err, &validationErr) {
			for _, finding := range validationErr.Findings {
				if finding.AssetID != cognition.ScopeMeta || !strings.HasPrefix(finding.Code, "meta_tag_dictionary_") {
					continue
				}
				fail.GlobalStop = &GlobalStopFacts{
					AffectedAsset: "aoci.meta.txt", Field: "tag_dictionary", RuleCode: finding.Code,
					Expected:       "one_parseable_conflict_free_A_B_C_optional_D_E_dictionary_per_enabled_object_volume",
					Actual:         "asset=meta;rule_code=" + finding.Code + ";detail=" + finding.Message,
					Cause:          mcpMessage("mcp.error.meta_tag_dictionary_unavailable", finding.Code, localeSafeMCPDetail(finding.Message)),
					SafeNextAction: mcpMessage("mcp.error.meta_tag_dictionary_repair_action"),
				}
				break
			}
		}
		return nil, fail
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic && len(set.Root.Document.Sections) == 0 {
		return nil, &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.no_sections"), Hint: mcpMessage("mcp.error.no_sections_hint", paths.IndexPath)}
	}
	bl, _, _ := baseline.Load(root)
	return &cognitionRepoCtx{cfg: cfg, paths: paths, set: set, bl: bl}, nil
}

// loadRepoCtx 现读仓库上下文。失败返回 (nil, *Fail)。
func loadRepoCtx(root string) (*repoCtx, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return nil, &Fail{Code: errVolumeReadOnly, Msg: mcpMessage("mcp.error.volume_read_only"), Hint: mcpMessage("mcp.error.volume_read_only_hint")}
	}
	return loaded.legacyRepo(), nil
}

func mcpMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", key)
	}
	return value
}

func localeSafeMCPDetail(detail string) string {
	hasHan := strings.ContainsFunc(detail, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
	hasASCII := strings.ContainsFunc(detail, func(character rune) bool {
		return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	})
	if (textassets.ActiveLocale() == textassets.DefaultLocale && hasHan) ||
		(textassets.ActiveLocale() == textassets.LegacyLocale && !hasHan && hasASCII) {
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			return mcpMessage("mcp.error.localized_detail_with_facts", facts)
		}
		return mcpMessage("mcp.error.localized_detail_unavailable")
	}
	return detail
}

// RunStdio 启动 stdio MCP server(阻塞至 ctx 取消或对端关闭)。
// root 在启动时定死(来自 aoci mcp 的工作目录或 --repo);version 经 CLI 注入。
func RunStdio(ctx context.Context, root, version string) error {
	srv, err := newMCPServer(root, version)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// newMCPServer使用同一启动版本装配协议握手与运行时工具。
// version只能来自RunStdio调用方，禁止在工具层从Git、PATH、索引或配置反推。
func newMCPServer(root, version string) (*mcp.Server, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	if err := textassets.SetActiveLocale(cfg.Locale); err != nil {
		return nil, err
	}
	if err := textassets.ValidateRuntime(); err != nil {
		return nil, fmt.Errorf("%s", mcpMessage(
			"mcp.catalog_invalid",
			localeSafeMCPDetail(err.Error()),
		))
	}
	descriptions, err := loadMCPToolDescriptions()
	if err != nil {
		return nil, err
	}
	inputSchemas, err := loadMCPInputSchemas()
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: machinecontract.MCPServerName, Version: version}, nil)
	refreshSession := newCognitionRefreshSession()
	registerReadTools(srv, root, version, descriptions, inputSchemas, refreshSession)
	registerWriteTools(srv, root, version, descriptions, inputSchemas, refreshSession)
	registerRemoveTool(srv, root, descriptions, inputSchemas)
	registerHeaderTool(srv, root, descriptions)
	registerMaintainTool(srv, root, version, descriptions, inputSchemas, refreshSession)
	return srv, nil
}
