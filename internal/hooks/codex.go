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
//   - hook 不在此安装: Codex 的 PreToolUse 对文件编辑(apply_patch)与 MCP 调用的
//     拦截尚不稳定(上游 openai/codex#16732),而文件编辑拦截恰是 aoci hook 的
//     核心场景,待上游稳定后再点亮(pretool 内核 agent 无关,届时只加适配层)。
package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// codexMarker 幂等判据表头
const codexMarker = "[mcp_servers.aoci]"

// hasCodexTable 判断 TOML 文本是否已含 aoci 表(逐行判定,注释行免疫)。
// 判据: 某行去除首尾空白后以 [mcp_servers.aoci] 开头 ——
// TOML 表头行允许尾随注释(如 "[mcp_servers.aoci] # 由 aoci 写入"),故用前缀而非全等;
// 以 # 开头的注释行天然不满足前缀条件,不会误判。
func hasCodexTable(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), codexMarker) {
			return true
		}
	}
	return false
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
	sep := "\n"
	if !strings.HasSuffix(text, "\n") {
		sep = "\n\n"
	} else if !strings.HasSuffix(text, "\n\n") {
		sep = "\n"
	}
	if werr := BackupThenWrite(path, []byte(text+sep+content)); werr != nil {
		return "", werr
	}
	return hookMessage("hook.codex_appended"), nil
}
