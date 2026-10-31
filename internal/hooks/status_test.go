// Index entry: status_test.go[TST7S]
// Responsibility: verify the four read-only criterion functions, with emphasis on the codex criterion "excluding comment lines" (which has a review incident history).
//
//	Temporary files are built with t.TempDir, independent of a real project, fitting deterministic tests.
//
// The collusion incident lesson (keep this): the first version of this file built the AGENTS fixture with the hand-written marker AOCI-BEGIN,
// which happened to match the wrong constant in the code under test; test and code shared the same wrong assumption, and the green light hid
// the real bug "the criterion mismatched the writer's real marker (the template's aoci:begin), so doctor always misreported not installed";
// worse, the first version even wrote a regression case defending the wrong marker (a defence built on the wrong ground).
//
// The iron rule after the fix (delivered throughout this file): the POSITIVE fixtures of criterion tests must come from the writer's real output:
// the AGENTS block uses textassets.TemplateAgentsMD, and Claude MCP/hook and Codex MCP call
// InstallClaudeMCP/InstallClaudeHook/InstallCodexMCP to really write before the criterion is tested
// (the full writer→criterion chain, so any writer evolution is followed by the tests automatically and collusion is structurally impossible);
// the NEGATIVE and boundary fixtures (corrupt JSON / comment lines / no aoci) stay hand-written, since they simulate shapes
// the writer never produces and should be independent of the real output anyway.
package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes a file under dir by relative path (creating parent directories), a test helper.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// —— The AGENTS.md block criterion (positive fixtures from the writer's textassets asset) ——

func TestIsAgentsBlockPresent(t *testing.T) {
	// No file → false
	dir := t.TempDir()
	if IsAgentsBlockPresent(dir) {
		t.Error("无 AGENTS.md 时应返回 false")
	}
	// A file without the marker → false
	dir2 := t.TempDir()
	writeFile(t, dir2, "AGENTS.md", "# 项目说明\n无 aoci 区块\n")
	if IsAgentsBlockPresent(dir2) {
		t.Error("AGENTS.md 无 aoci 区块标记时应返回 false")
	}
	// The real template output as the fixture → true (the cross-function consistency assertion between writer and criterion, the core of the collusion defence)
	dir3 := t.TempDir()
	writeFile(t, dir3, "AGENTS.md", "# 前言\n\n"+loadAgentsAssetForTest(t))
	if !IsAgentsBlockPresent(dir3) {
		t.Error("含写入端模板实物区块时应返回 true(判据与textassets资产标记失配)")
	}
	// The stronger full chain: EnsureAgentsBlock really writes → the criterion must recognize it
	dir5 := t.TempDir()
	if _, err := EnsureAgentsBlock(dir5); err != nil {
		t.Fatalf("EnsureAgentsBlock 落盘失败: %v", err)
	}
	if !IsAgentsBlockPresent(dir5) {
		t.Error("EnsureAgentsBlock 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// [Regression guard] the historical wrong marker AOCI-BEGIN (upper case, hyphen) → false:
	// that shape was never produced by any writer, and the first criterion's misuse of it made doctor always report not installed;
	// this case locks "the criterion must not be changed back to the wrong marker".
	dir4 := t.TempDir()
	writeFile(t, dir4, "AGENTS.md", "<!-- AOCI-BEGIN -->\n内容\n<!-- AOCI-END -->\n")
	if IsAgentsBlockPresent(dir4) {
		t.Error("历史错误标记 AOCI-BEGIN 不应被识别(真实标记是模板中的 aoci:begin)")
	}
}

// —— The Claude MCP (.mcp.json) criterion (positive cases go through a real InstallClaudeMCP write) ——

func TestIsClaudeMCPInstalled(t *testing.T) {
	// No file → false
	if IsClaudeMCPInstalled(t.TempDir()) {
		t.Error("无 .mcp.json 时应返回 false")
	}
	// Full-chain positive: InstallClaudeMCP really writes → the criterion must recognize it
	dir := t.TempDir()
	if _, err := InstallClaudeMCP(dir); err != nil {
		t.Fatalf("InstallClaudeMCP 落盘失败: %v", err)
	}
	if !IsClaudeMCPInstalled(dir) {
		t.Error("InstallClaudeMCP 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// Idempotent early return (the writer reuses the criterion): a second install should report skipped without error
	if msg, err := InstallClaudeMCP(dir); err != nil || msg == "" {
		t.Errorf("二次安装应幂等跳过: msg=%q err=%v", msg, err)
	}
	// Full-chain extension: after a merge install over existing servers, the existing key and the aoci key coexist and the criterion recognizes it
	dir4 := t.TempDir()
	writeFile(t, dir4, ".mcp.json", `{"mcpServers":{"other":{"command":"x"}}}`)
	if _, err := InstallClaudeMCP(dir4); err != nil {
		t.Fatalf("InstallClaudeMCP 合并落盘失败: %v", err)
	}
	if !IsClaudeMCPInstalled(dir4) {
		t.Error("合并安装后判据应识别 aoci 键")
	}
	// mcpServers present but no aoci → false (boundary, hand-written)
	dir2 := t.TempDir()
	writeFile(t, dir2, ".mcp.json", `{"mcpServers":{"other":{"command":"x"}}}`)
	if IsClaudeMCPInstalled(dir2) {
		t.Error("mcpServers 无 aoci 键时应返回 false")
	}
	// Corrupt JSON → false (never misreported as installed; negative, hand-written)
	dir3 := t.TempDir()
	writeFile(t, dir3, ".mcp.json", `{not valid json`)
	if IsClaudeMCPInstalled(dir3) {
		t.Error("JSON 损坏时应返回 false,不得误报已装")
	}
}

// —— The Claude hook (.claude/settings.json) criterion (positive cases go through a real InstallClaudeHook write;
//    the criterion is now an exact walk, sharing claudeSettingsHasAociHook with the writer's idempotence) ——

func TestIsClaudeHookInstalled(t *testing.T) {
	// No file → false
	if IsClaudeHookInstalled(t.TempDir()) {
		t.Error("无 .claude/settings.json 时应返回 false")
	}
	// Full-chain positive: InstallClaudeHook really writes (script + settings registration) → the criterion must recognize it
	dir := t.TempDir()
	if _, err := InstallClaudeHook(dir); err != nil {
		t.Fatalf("InstallClaudeHook 落盘失败: %v", err)
	}
	if !IsClaudeHookInstalled(dir) {
		t.Error("InstallClaudeHook 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// No aoci → false (boundary, hand-written)
	dir2 := t.TempDir()
	writeFile(t, dir2, ".claude/settings.json", `{"hooks":{"PreToolUse":[]}}`)
	if IsClaudeHookInstalled(dir2) {
		t.Error("settings.json 不含 aoci 时应返回 false")
	}
	// [Weakness elimination assertion] aoci appearing in an unrelated form → false:
	// the old "contains the aoci substring" shortcut misreported installed here (a documented known weakness),
	// and the exact-walk criterion must not accept it; this case locks the weakness from reviving.
	dir3 := t.TempDir()
	writeFile(t, dir3, ".claude/settings.json", `{"note":"aoci mentioned but no hook","hooks":{}}`)
	if IsClaudeHookInstalled(dir3) {
		t.Error("aoci 以无关形式出现不应被判为已装(旧子串判据弱点,已由精确遍历消除)")
	}
	// Corrupt JSON → false (never misreported as installed; a new boundary now that the exact criterion parses)
	dir4 := t.TempDir()
	writeFile(t, dir4, ".claude/settings.json", `{broken`)
	if IsClaudeHookInstalled(dir4) {
		t.Error("settings.json 损坏时应返回 false,不得误报已装")
	}
}

// —— The Codex MCP (.codex/config.toml) criterion: positive cases go through a real InstallCodexMCP write,
//    negative cases focus on excluding comment lines (the review incident history) ——

func TestIsCodexMCPInstalled(t *testing.T) {
	// No file → false
	if IsCodexMCPInstalled(t.TempDir()) {
		t.Error("无 .codex/config.toml 时应返回 false")
	}
	// Full-chain positive: InstallCodexMCP really writes (template rendering + creation) → the criterion must recognize it
	dir := t.TempDir()
	if _, err := InstallCodexMCP(dir); err != nil {
		t.Fatalf("InstallCodexMCP 落盘失败: %v", err)
	}
	if !IsCodexMCPInstalled(dir) {
		t.Error("InstallCodexMCP 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// Full-chain extension: after an append install over existing TOML content, the existing content is kept and the criterion recognizes it
	dir6 := t.TempDir()
	writeFile(t, dir6, ".codex/config.toml", "[mcp_servers.other]\ncommand = \"x\"\n")
	if _, err := InstallCodexMCP(dir6); err != nil {
		t.Fatalf("InstallCodexMCP 追加落盘失败: %v", err)
	}
	if !IsCodexMCPInstalled(dir6) {
		t.Error("追加安装后判据应识别 aoci 表")
	}
	// A table header with leading whitespace (still recognized after TrimSpace; boundary, hand-written: the writer's output is always flush left,
	// and this shape simulates a user's manual indentation) → true
	dir2 := t.TempDir()
	writeFile(t, dir2, ".codex/config.toml", "   [mcp_servers.aoci]\n")
	if !IsCodexMCPInstalled(dir2) {
		t.Error("带前导空白的表头 TrimSpace 后应识别为已装")
	}
	// [Key regression guard] only the comment line #[mcp_servers.aoci] → false (the core case of the review incident; negative, hand-written)
	dir3 := t.TempDir()
	writeFile(t, dir3, ".codex/config.toml", "# [mcp_servers.aoci] 这是注释不算安装\nother = 1\n")
	if IsCodexMCPInstalled(dir3) {
		t.Error("注释行 #[mcp_servers.aoci] 绝不能被判为已装(审查修正的核心)")
	}
	// A comment hugging the hash with no space, #[mcp_servers.aoci] → false (negative, hand-written)
	dir4 := t.TempDir()
	writeFile(t, dir4, ".codex/config.toml", "#[mcp_servers.aoci]\n")
	if IsCodexMCPInstalled(dir4) {
		t.Error("紧贴井号的注释 #[mcp_servers.aoci] 也绝不能判为已装")
	}
	// No aoci table header → false (boundary, hand-written)
	dir5 := t.TempDir()
	writeFile(t, dir5, ".codex/config.toml", "[mcp_servers.other]\ncommand = \"x\"\n")
	if IsCodexMCPInstalled(dir5) {
		t.Error("仅含其他 server 表头时应返回 false")
	}
}
