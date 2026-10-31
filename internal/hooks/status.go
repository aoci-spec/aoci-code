// Index entry: status.go[Hook.Status.7.S]
// Responsibility: the READ-ONLY criterion functions for agent integration status (doctor and installer share one reading).
//
// Single source of truth (the former "transient duplication, elimination plan" P1 is delivered; this file holds no copy of criterion logic any more):
//   - the AGENTS block: uses installer.go's agentsBegin constant directly; the writer and the criterion
//     are the same symbol, not two copies claiming to agree. The first version held its own hand-written constant <!-- AOCI-BEGIN -->,
//     mismatching the writer's real <!-- aoci:begin -->, so doctor always misreported not installed; the lesson: "same source"
//     must be the same symbol at compile time, and a comment claiming agreement does not count;
//   - Claude MCP: the criterion itself lives here (the parsed file holds the aoci key), and the writer InstallClaudeMCP's
//     idempotent early return reuses this function in the other direction;
//   - Claude hook: delegated to claude.go's claudeSettingsHasAociHook (the same implementation as the writer's idempotence,
//     an exact walk of PreToolUse); the old "contains the aoci substring" shortcut and its known weakness
//     (aoci appearing in an unrelated form misreported as installed) are gone with it;
//   - Codex: delegated to codex.go's hasCodexTable (the same implementation as the writer's idempotence, judged line by line,
//     immune to comment lines, the single carrier of the review-incident defence).
//
// Criterion value principle: any read/parse failure returns false (treated as not installed); in a diagnostic scenario it is better
// to report "not installed" and prompt the user to check than to misreport "installed" and give false comfort.
package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// IsAgentsBlockPresent reports whether the root AGENTS.md already holds the AOCI instruction block.
// Criterion: the file exists and holds the begin marker (installer.go's agentsBegin, the same constant as the writer).
func IsAgentsBlockPresent(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return false // the file is missing or unreadable = not installed
	}
	return strings.Contains(string(data), agentsBegin)
}

// IsClaudeMCPInstalled reports whether .mcp.json already configures the aoci MCP server.
// Criterion: the file exists, parses as JSON, and its top-level mcpServers object holds the "aoci" key.
// This function is the single implementation of that criterion; InstallClaudeMCP's idempotent early return reuses it.
func IsClaudeMCPInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		return false
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false // corrupt JSON = treated as not installed (never misreported as installed)
	}
	_, ok := doc.MCPServers["aoci"]
	return ok
}

// IsClaudeHookInstalled reports whether .claude/settings.json already holds aoci's PreToolUse hook.
// Criterion: delegated to claudeSettingsHasAociHook (claude.go, the same implementation as the writer's idempotence),
// an exact walk of the hooks.PreToolUse array checking whether command references claude-pretool.sh.
func IsClaudeHookInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false // corrupt JSON = treated as not installed (never misreported as installed)
	}
	return claudeSettingsHasAociHook(settings)
}

// IsCodexMCPInstalled reports whether .codex/config.toml already configures the aoci MCP server.
// Criterion: delegated to hasCodexTable (codex.go, the same implementation as the writer's idempotence), recognizing the complete table header and
// the equivalent quoted-key spelling, immune to comment lines (the single carrier of the review-incident defence).
func IsCodexMCPInstalled(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		return false
	}
	return hasCodexTable(string(data))
}

// IsOpenCodeMCPInstalled reports whether the project-level opencode.json holds an OpenCode V1 mcp.aoci configuration
// bound exactly to the current absolute executable/repository.
// JSONC, V2, a conflict, or a parse failure all return false; the writer reuses the same Prepare criterion.
func IsOpenCodeMCPInstalled(root string) bool {
	plan, err := prepareOpenCodeMCP(root)
	return err == nil && plan.Current
}
