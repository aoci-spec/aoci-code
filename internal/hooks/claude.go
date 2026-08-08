// Claude Code 适配: 项目级 MCP 配置(.mcp.json)与 PreToolUse hook(.claude/settings.json)
// 索引条目: claude.go[Hook.Hook.8.S]
//
// 纪律:
//   - JSON 合并写入,绝不覆盖用户既有 servers/hooks;写前 BackupThenWrite 备份;
//   - 幂等判据单一事实源: MCP 侧早退复用判据端 IsClaudeMCPInstalled(status.go),
//     hook 侧写入端与判据端共用 claudeSettingsHasAociHook(本文件)—— 写入端与
//     判据端绝不各持一份逻辑副本(status.go 判据失配事故的教训);
//   - hook 挂 Edit|Write|MultiEdit 的 PreToolUse,shell 脚本零逻辑只 exec aoci;
//   - 卸载(二期)= 删 aoci 键/aoci hook 项,不碰用户其余配置。
package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// claudeSettingsHasAociHook 判断已解析的 .claude/settings.json 是否含 aoci 的
// PreToolUse hook(精确遍历: hooks.PreToolUse 任一项的 hooks[].command 含
// "claude-pretool.sh")。写入端 InstallClaudeHook 的幂等判断与判据端
// IsClaudeHookInstalled(status.go)共用本函数 —— 单一事实源,任一侧改动
// 另一侧自动跟随;精确遍历同时消除旧"含 aoci 子串"简化判据的已知弱点
// (aoci 以无关形式出现被误报已装)。
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

// InstallClaudeMCP 合并写入项目级 .mcp.json 的 mcpServers.aoci
func InstallClaudeMCP(root string) (string, error) {
	path := filepath.Join(root, ".mcp.json")
	data := NewTplData(root)

	// 幂等早退: 复用判据端 IsClaudeMCPInstalled(单一事实源)。
	// 损坏 JSON 时判据按原则返 false,由下方解析路径给出可操作报错,行为不变。
	if IsClaudeMCPInstalled(root) {
		return hookMessage("hook.claude_mcp_current"), nil
	}

	// 读取既有配置(缺失=空对象)
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

// InstallClaudeHook 写入 hook 脚本(.aoci/hooks/claude-pretool.sh)并
// 合并注册到项目级 .claude/settings.json 的 hooks.PreToolUse
func InstallClaudeHook(root string) (string, error) {
	data := NewTplData(root)

	// 1) 落 hook 脚本(渲染产物过 safety 后写入,加执行权限)
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

	// 2) 合并注册 .claude/settings.json
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	settings := map[string]any{}
	if raw, rerr := os.ReadFile(settingsPath); rerr == nil {
		if jerr := json.Unmarshal(raw, &settings); jerr != nil {
			return "", errors.New(hookMessage("hook.claude_settings_invalid", jerr))
		}
	}

	// 幂等: 判据与 IsClaudeHookInstalled 共用同一实现(单一事实源)
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
