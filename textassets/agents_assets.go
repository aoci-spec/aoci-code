package textassets

const (
	// TemplateAgentsMD is the template of the AOCI-managed block installed into the repository root AGENTS.md.
	//
	// The template has no dynamic placeholder today, but it is still parsed and Safety-checked by hooks.RenderTemplate,
	// so it shares the same write safety boundary as the other repository materialization templates.
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
