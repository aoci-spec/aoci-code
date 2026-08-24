// Codex 适配: 项目级 MCP 配置写入(.codex/config.toml)
// 索引条目: codex.go[HCX8JS]
//
// 设计约束:
//   - 只写项目级 .codex/config.toml(Codex 的 codex mcp add 命令只写用户级,
//     项目级配置官方要求手工编辑 TOML —— 本函数即该手工步骤的自动化);
//   - 不引入 TOML 解析依赖: 采用"表追加"策略 —— 在 TOML 语法下,文件末尾追加
//     一个不重复的 [table] 是恒安全操作;已存在 [mcp_servers.aoci] 即跳过(幂等),
//     其余任何形态的既有内容一字节不动,复杂合并场景不做猜测;
//   - 幂等判据 = 逐行 TrimSpace 后以 "[mcp_servers.aoci]" 为前缀的非注释行
//     (审查修正: 早期的全文子串检测会被注释行 #[mcp_servers.aoci] 误判为已安装);
//   - 写前经 BackupThenWrite 备份;渲染产物先过 safety 闸门(RenderTemplate 内置);
//   - Codex 仅对"受信任项目"加载项目级配置,信任动作归用户在 Codex 侧完成,
//     本函数在返回文案中提示;
//   - 可选 context-compaction 接入由顶级 compact_prompt 与
//     SessionStart(source=compact) hook 组成;compact_prompt 必须位于首个 TOML
//     table 之前,所以安装时前置 AOCI 托管片段,hook array-of-tables 则安全追加;
//   - 用户已有非 AOCI 托管的 compact_prompt 或
//     experimental_compact_prompt_file 时失败关闭,绝不覆盖用户的压缩策略。
package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	// codexMarker 是 MCP 配置的幂等判据表头。
	codexMarker = "[mcp_servers.aoci]"

	// 以下 marker 是 AOCI 托管 Codex 压缩配置的稳定所有权边界。
	// 用户自定义 compact_prompt 没有 prompt marker,安装器必须失败关闭。
	codexCompactPromptMarker = "# aoci:codex-compact-prompt:v1"
	codexCompactHookMarker   = "# aoci:codex-compact-hook:v1"
	codexUTF8BOM             = "\ufeff"
)

type codexCompactPromptState struct {
	Managed          bool
	Custom           bool
	ExperimentalFile bool
}

// hasCodexTable 判断 TOML 文本是否已含 aoci 表(逐行判定,注释行免疫)。
// 判据: 某行去除首尾空白后以 [mcp_servers.aoci] 开头 ——
// TOML 表头行允许尾随注释(如 "[mcp_servers.aoci] # 由 aoci 写入"),故用前缀而非全等;
// 以 # 开头的注释行天然不满足前缀条件,不会误判。
func hasCodexTable(text string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), codexMarker) {
			return true
		}
	}
	return false
}

// codexTopLevelKey 返回一条简单 TOML 赋值行的键名。Codex 的两个压缩键
// 都是顶级标量;一旦遇到首个 table header,后续赋值属于该 table,不再算顶级。
func codexTopLevelKey(line string) string {
	index := strings.IndexByte(line, '=')
	if index < 0 {
		return ""
	}
	key := strings.TrimSpace(line[:index])
	if len(key) >= 2 && ((key[0] == '"' && key[len(key)-1] == '"') ||
		(key[0] == '\'' && key[len(key)-1] == '\'')) {
		key = key[1 : len(key)-1]
	}
	return key
}

// inspectCodexCompactPrompt 只检查首个 TOML table 之前的顶级键。
// AOCI marker 必须紧邻其托管 compact_prompt(中间只允许空行或注释),避免一个
// 游离 marker 把用户自定义提示误认成 AOCI 资产。
func inspectCodexCompactPrompt(text string) codexCompactPromptState {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	state := codexCompactPromptState{}
	atRoot := true
	managedMarkerPending := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if line == codexCompactPromptMarker {
				managedMarkerPending = true
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			atRoot = false
			managedMarkerPending = false
			continue
		}
		if !atRoot {
			continue
		}

		switch codexTopLevelKey(line) {
		case "compact_prompt":
			if managedMarkerPending {
				state.Managed = true
			} else {
				state.Custom = true
			}
		case "experimental_compact_prompt_file":
			state.ExperimentalFile = true
		}
		managedMarkerPending = false
	}
	return state
}

func containsCodexManagedSnippet(text, snippet string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	snippet = strings.TrimRight(snippet, "\n")
	return snippet != "" && strings.Contains(text, snippet)
}

func hasCodexCompactCommand(text string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := codexTopLevelKey(line)
		if (key == "command" || key == "command_windows") &&
			strings.Contains(line, "hook codex-compact") {
			return true
		}
	}
	return false
}

func validateCodexCompactionText(text string) error {
	state := inspectCodexCompactPrompt(text)
	if state.ExperimentalFile {
		return errors.New(hookMessage("hook.codex_compact_file_conflict"))
	}
	if state.Custom {
		return errors.New(hookMessage("hook.codex_compact_prompt_conflict"))
	}
	return nil
}

func appendCodexSnippet(text, content string) string {
	content = strings.TrimRight(content, "\n") + "\n"
	if text == "" {
		return content
	}
	sep := "\n"
	if !strings.HasSuffix(text, "\n") {
		sep = "\n\n"
	}
	return text + sep + content
}

func prependCodexTopLevel(text, content string) string {
	content = strings.TrimRight(content, "\n") + "\n"
	if text == "" {
		return content
	}
	if strings.HasPrefix(text, codexUTF8BOM) {
		return codexUTF8BOM + content + "\n" + strings.TrimPrefix(text, codexUTF8BOM)
	}
	return content + "\n" + text
}

func renderCodexCompactionSnippets(root string) (string, string, error) {
	data := NewTplData(root)
	prompt, err := renderLocaleTemplate(
		"codex-compact-prompt.toml.tmpl",
		textassets.TemplateCodexCompactPrompt,
		data,
	)
	if err != nil {
		return "", "", err
	}
	hook, err := renderLocaleTemplate(
		"codex-compact-hook.toml.tmpl",
		textassets.TemplateCodexCompactHook,
		data,
	)
	if err != nil {
		return "", "", err
	}
	return prompt, hook, nil
}

func validateCodexManagedSnippets(text, prompt, hook string) error {
	if err := validateCodexCompactionText(text); err != nil {
		return err
	}
	state := inspectCodexCompactPrompt(text)
	if state.Managed && !containsCodexManagedSnippet(text, prompt) {
		return errors.New(hookMessage("hook.codex_compact_managed_prompt_conflict"))
	}
	hasHook := containsCodexManagedSnippet(text, hook)
	hasHookMarker := strings.Contains(
		strings.TrimPrefix(text, codexUTF8BOM),
		codexCompactHookMarker,
	)
	if !hasHook && (hasHookMarker || hasCodexCompactCommand(text)) {
		return errors.New(hookMessage("hook.codex_compact_hook_conflict"))
	}
	return nil
}

// ValidateCodexCompactionInstall 是 --hooks 的零写入冲突与资产预检。
// installer 必须在写 MCP 表之前调用它,保证用户自定义压缩策略冲突时仓库零改动。
func ValidateCodexCompactionInstall(root string) error {
	path := filepath.Join(root, ".codex", "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	prompt, hook, err := renderCodexCompactionSnippets(root)
	if err != nil {
		return err
	}
	if err := validateCodexManagedSnippets(text, prompt, hook); err != nil {
		return err
	}
	return nil
}

// InstallCodexCompaction 在同一个 .codex/config.toml 中安装 AOCI 托管的
// compact_prompt 与同步 SessionStart(source=compact) hook。用户既有字节原样
// 保留;缺失的顶级 prompt 前置、hook 片段后置;两项均已存在时严格零写入。
func InstallCodexCompaction(root string) (string, error) {
	path := filepath.Join(root, ".codex", "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	text := string(existing)
	prompt, hook, err := renderCodexCompactionSnippets(root)
	if err != nil {
		return "", err
	}
	if err := validateCodexManagedSnippets(text, prompt, hook); err != nil {
		return "", err
	}

	state := inspectCodexCompactPrompt(text)
	hasHook := containsCodexManagedSnippet(text, hook)
	if state.Managed && hasHook {
		return hookMessage("hook.codex_compact_current"), nil
	}

	if !state.Managed {
		text = prependCodexTopLevel(text, prompt)
	}
	if !hasHook {
		text = appendCodexSnippet(text, hook)
	}

	if writeErr := BackupThenWrite(path, []byte(text)); writeErr != nil {
		return "", writeErr
	}
	return hookMessage("hook.codex_compact_installed"), nil
}

// InstallCodexMCP 写入项目级 .codex/config.toml 的 [mcp_servers.aoci] 表。
// 文件不存在则创建;存在且无本表则文末追加;已含本表则跳过。
// 返回面向用户的结果说明。
func InstallCodexMCP(root string) (string, error) {
	data := NewTplData(root)

	// 渲染 TOML 片段(RenderTemplate 内置 safety 扫描,命中禁区词拒绝落盘)
	content, err := renderLocaleTemplate(
		"codex-mcp.toml.tmpl",
		textassets.TemplateCodexMCPConfig,
		data,
	)
	if err != nil {
		return "", err
	}
	content = strings.TrimRight(content, "\n") + "\n"

	path := filepath.Join(root, ".codex", "config.toml")
	existing, rerr := os.ReadFile(path)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			return "", rerr
		}
		// 目标不存在: 新建(BackupThenWrite 会自动创建 .codex 目录)
		if werr := BackupThenWrite(path, []byte(content)); werr != nil {
			return "", werr
		}
		return hookMessage("hook.codex_created"), nil
	}

	text := string(existing)
	// 幂等: 已含本表即跳过,不做任何猜测性合并
	if hasCodexTable(text) {
		return hookMessage("hook.codex_current"), nil
	}

	// 追加: TOML 语法下文末追加不重复的表恒安全;既有内容一字节不动
	if werr := BackupThenWrite(path, []byte(appendCodexSnippet(text, content))); werr != nil {
		return "", werr
	}
	return hookMessage("hook.codex_appended"), nil
}
