package textassets

const (
	// TemplateAgentsMD是安装到仓库根AGENTS.md中AOCI托管区块的模板。
	//
	// 模板当前没有动态占位符，但仍由hooks.RenderTemplate执行解析与Safety
	// 校验，保证它与其他仓库物化模板共享同一落盘安全边界。
	TemplateAgentsMD ID = "templates/AGENTS.md"

	// TemplateCodexMCPConfig is the project-level Codex MCP configuration.
	TemplateCodexMCPConfig ID = "templates/codex-mcp-config"

	// TemplateCodexCompactPrompt is the AOCI-managed Codex compaction prompt.
	TemplateCodexCompactPrompt ID = "templates/codex-compact-prompt"

	// TemplateCodexCompactHook reloads AOCI cognition after Codex compaction.
	TemplateCodexCompactHook ID = "templates/codex-compact-hook"

	// TemplateCodexCursorStubs is the manual integration reference for hosts
	// whose configuration cannot be installed safely by AOCI.
	TemplateCodexCursorStubs ID = "templates/codex-cursor-stubs"

	// TemplateClaudePretool is the thin Claude Code PreToolUse script.
	TemplateClaudePretool ID = "templates/claude-pretool"
)
