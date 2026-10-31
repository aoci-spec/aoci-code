// Claude Code adapter: the project-level MCP configuration (.mcp.json) and the PreToolUse hook (.claude/settings.json)
// Index entry: claude.go[Hook.Hook.8.S]
//
// Discipline:
//   - JSON merge writes, never overwriting the user's existing servers/hooks; BackupThenWrite backs up before writing;
//   - a single source of truth for the idempotence criterion: the MCP side's early return reuses the criterion-side IsClaudeMCPInstalled (status.go),
//     and the hook side's writer and criterion share claudeSettingsHasAociHook (this file); the writer and the
//     criterion never each hold a copy of the logic (the lesson of the status.go criterion mismatch incident);
//   - the hook hangs on the PreToolUse of Edit|Write|MultiEdit, and the shell script holds zero logic and only execs aoci;
//   - uninstall (phase two) = remove the aoci key / the aoci hook item, leaving the rest of the user's configuration alone.
package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// claudeSettingsHasAociHook reports whether a parsed .claude/settings.json holds aoci's
// PreToolUse hook (an exact walk: some item of hooks.PreToolUse has a hooks[].command holding
// "claude-pretool.sh"). The writer InstallClaudeHook's idempotence check and the criterion
// IsClaudeHookInstalled (status.go) share this function: a single source of truth, so a change on either side
// follows on the other automatically; the exact walk also removes the known weakness of the old "contains the aoci substring"
// shortcut (aoci appearing in an unrelated form was misreported as installed).
func claudeSettingsHasAociHook(settings map[string]any) bool {
	hooksObj, _ := settings["hooks"].(map[string]any)
	if hooksObj == nil {
		return false
	}
	pre, _ := hooksObj["PreToolUse"].([]any)
	for _, item := range pre {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hs, ok := m["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range hs {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmdStr, ok := hm["command"].(string); ok && strings.Contains(cmdStr, "claude-pretool.sh") {
				return true
			}
		}
	}
	return false
}

// InstallClaudeMCP merge-writes mcpServers.aoci into the project-level .mcp.json
func InstallClaudeMCP(root string) (string, error) {
	path := filepath.Join(root, ".mcp.json")
	data := NewTplData(root)

	// Idempotent early return: reuse the criterion-side IsClaudeMCPInstalled (single source of truth).
	// On corrupt JSON the criterion returns false by principle, and the parsing path below gives the actionable error; behaviour unchanged.
	if IsClaudeMCPInstalled(root) {
		return hookMessage("hook.claude_mcp_current"), nil
	}

	// Read the existing configuration (missing = an empty object)
	cfg := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if jerr := json.Unmarshal(raw, &cfg); jerr != nil {
			return "", errors.New(hookMessage("hook.claude_mcp_invalid", jerr))
		}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["aoci"] = map[string]any{
		"command": data.BinPath,
		"args":    []string{"--repo", data.RepoRoot, "mcp"},
	}
	cfg["mcpServers"] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := BackupThenWrite(path, append(out, '\n')); err != nil {
		return "", err
	}
	return hookMessage("hook.claude_mcp_written"), nil
}

// InstallClaudeHook writes the hook script (.aoci/hooks/claude-pretool.sh) and
// merge-registers it into hooks.PreToolUse of the project-level .claude/settings.json
func InstallClaudeHook(root string) (string, error) {
	data := NewTplData(root)

	// 1) Land the hook script (the rendered output passes safety before writing, then gets the execute bit)
	script, err := renderLocaleTemplate(
		"claude-pretool.sh.tmpl",
		textassets.TemplateClaudePretool,
		data,
	)
	if err != nil {
		return "", err
	}
	scriptPath := filepath.Join(root, ".aoci", "hooks", "claude-pretool.sh")
	if err := BackupThenWrite(scriptPath, []byte(script)); err != nil {
		return "", err
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return "", err
	}

	// 2) Merge-register .claude/settings.json
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	settings := map[string]any{}
	if raw, rerr := os.ReadFile(settingsPath); rerr == nil {
		if jerr := json.Unmarshal(raw, &settings); jerr != nil {
			return "", errors.New(hookMessage("hook.claude_settings_invalid", jerr))
		}
	}

	// Idempotence: the criterion shares one implementation with IsClaudeHookInstalled (single source of truth)
	if claudeSettingsHasAociHook(settings) {
		return hookMessage("hook.claude_hook_current"), nil
	}

	hooksObj, _ := settings["hooks"].(map[string]any)
	if hooksObj == nil {
		hooksObj = map[string]any{}
	}
	pre, _ := hooksObj["PreToolUse"].([]any)
	pre = append(pre, map[string]any{
		"matcher": "Edit|Write|MultiEdit",
		"hooks": []any{
			map[string]any{"type": "command", "command": scriptPath},
		},
	})
	hooksObj["PreToolUse"] = pre
	settings["hooks"] = hooksObj

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := BackupThenWrite(settingsPath, append(out, '\n')); err != nil {
		return "", err
	}
	return hookMessage("hook.claude_hook_installed", scriptPath), nil
}
