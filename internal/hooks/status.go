// 索引条目: status.go[Hook.Status.7.S]
// 职责: 提供 agent 接入状态的【只读】判据函数(doctor 与 installer 共用一致口径)。
//
// 单一事实源(原"暂态重复,消除计划"P1 已兑现,本文件不再持有任何判据逻辑副本):
//   - AGENTS 区块: 直接使用 installer.go 的 agentsBegin 常量 —— 写入端与判据端
//     是同一符号,不是两份声称一致的副本。初版曾自持手写常量 <!-- AOCI-BEGIN -->
//     与写入端实物 <!-- aoci:begin --> 失配,致 doctor 恒误报未装;教训: "同源"
//     必须编译期同符号,凭注释声称一致不作数;
//   - Claude MCP: 判据本体在此(解析后含 aoci 键),写入端 InstallClaudeMCP
//     幂等早退反向复用本函数;
//   - Claude hook: 委托 claude.go 的 claudeSettingsHasAociHook(与写入端幂等
//     同一实现,精确遍历 PreToolUse);旧"含 aoci 子串"简化版及其已知弱点
//     (aoci 以无关形式出现误报已装)随之消除;
//   - Codex: 委托 codex.go 的 hasCodexTable(与写入端幂等同一实现,逐行判定
//     注释行免疫,审查事故防线单点承载)。
//
// 判据取值原则: 任何读取/解析失败一律返回 false(视为未安装)—— 诊断场景宁可
// 报"未装"促使用户检查,绝不误报"已装"给出虚假安全感。
package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// IsAgentsBlockPresent 判断根目录 AGENTS.md 是否已含 AOCI 指令区块。
// 判据: 文件存在且包含起始标记(installer.go 的 agentsBegin,与写入端同一常量)。
func IsAgentsBlockPresent(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return false // 文件不存在或读取失败 = 未安装
	}
	return strings.Contains(string(data), agentsBegin)
}

// IsClaudeMCPInstalled 判断 .mcp.json 是否已配置 aoci MCP server。
// 判据: 文件存在、可解析为 JSON、且顶层 mcpServers 对象含 "aoci" 键。
// 本函数是该判据的唯一实现,InstallClaudeMCP 幂等早退复用本函数。
func IsClaudeMCPInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		return false
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false // JSON 损坏 = 视为未安装(不误报已装)
	}
	_, ok := doc.MCPServers["aoci"]
	return ok
}

// IsClaudeHookInstalled 判断 .claude/settings.json 是否已安装 aoci 的 PreToolUse hook。
// 判据: 委托 claudeSettingsHasAociHook(claude.go,与写入端幂等同一实现)——
// 精确遍历 hooks.PreToolUse 数组检查 command 是否引用 claude-pretool.sh。
func IsClaudeHookInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false // JSON 损坏 = 视为未安装(不误报已装)
	}
	return claudeSettingsHasAociHook(settings)
}

// IsCodexMCPInstalled 判断 .codex/config.toml 是否已配置 aoci MCP server。
// 判据: 委托 hasCodexTable(codex.go,与写入端幂等同一实现)—— 逐行 TrimSpace
// 后前缀匹配 [mcp_servers.aoci],注释行天然免疫(审查事故防线单点承载)。
func IsCodexMCPInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		return false
	}
	return hasCodexTable(string(data))
}
