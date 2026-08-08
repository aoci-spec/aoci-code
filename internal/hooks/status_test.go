// 索引条目: status_test.go[TST7S]
// 职责: 验证四个只读判据函数;重点覆盖 codex 判据"排除注释行"(有审查事故史)。
//
//	用 t.TempDir 造临时文件,不依赖真实项目,契合确定性测试。
//
// 共谋事故教训(务必保留): 本文件初版 AGENTS 用例用手写标记 AOCI-BEGIN 造夹具,
// 与被测代码的错误常量恰好一致 —— 测试与代码共享同一错误假设,绿灯掩盖了
// "判据与写入端实物(模板 aoci:begin)失配、doctor 恒误报未装"的真 bug;
// 更有甚者,初版还写了一条防回归用例去保卫错误标记(防御修在错误阵地上)。
//
// 修正后铁律(本文件全面兑现): 判据类测试的【正例】夹具必须取材写入端实物 ——
// AGENTS 区块用textassets.TemplateAgentsMD,Claude MCP/hook与Codex MCP直接调用
// InstallClaudeMCP/InstallClaudeHook/InstallCodexMCP 真实落盘后再测判据
// (写入端→判据端全链路,写入端任何演化测试自动跟随,共谋结构性不可能);
// 【反例与边界】夹具(JSON 损坏/注释行/不含 aoci)保持手写 —— 它们模拟的是
// 写入端从不产出的形态,本就该独立于实物。
package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile 在 dir 下按相对路径写文件(自动建父目录),测试辅助。
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

// —— AGENTS.md 区块判据(正例夹具取材写入端textassets资产)——

func TestIsAgentsBlockPresent(t *testing.T) {
	// 无文件 → false
	dir := t.TempDir()
	if IsAgentsBlockPresent(dir) {
		t.Error("无 AGENTS.md 时应返回 false")
	}
	// 有文件但无标记 → false
	dir2 := t.TempDir()
	writeFile(t, dir2, "AGENTS.md", "# 项目说明\n无 aoci 区块\n")
	if IsAgentsBlockPresent(dir2) {
		t.Error("AGENTS.md 无 aoci 区块标记时应返回 false")
	}
	// 真实模板实物作为夹具 → true(写入端与判据端的跨函数一致性断言,共谋防线核心)
	dir3 := t.TempDir()
	writeFile(t, dir3, "AGENTS.md", "# 前言\n\n"+loadAgentsAssetForTest(t))
	if !IsAgentsBlockPresent(dir3) {
		t.Error("含写入端模板实物区块时应返回 true(判据与textassets资产标记失配)")
	}
	// 更强的全链路: EnsureAgentsBlock 真实落盘 → 判据必须识别
	dir5 := t.TempDir()
	if _, err := EnsureAgentsBlock(dir5); err != nil {
		t.Fatalf("EnsureAgentsBlock 落盘失败: %v", err)
	}
	if !IsAgentsBlockPresent(dir5) {
		t.Error("EnsureAgentsBlock 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// 【防回归】历史错误标记 AOCI-BEGIN(大写连字符)→ false:
	// 该形态从未被任何写入端产出,初版判据误用它致 doctor 恒报未装;
	// 本用例锁死"不得把判据改回错误标记"。
	dir4 := t.TempDir()
	writeFile(t, dir4, "AGENTS.md", "<!-- AOCI-BEGIN -->\n内容\n<!-- AOCI-END -->\n")
	if IsAgentsBlockPresent(dir4) {
		t.Error("历史错误标记 AOCI-BEGIN 不应被识别(真实标记是模板中的 aoci:begin)")
	}
}

// —— Claude MCP(.mcp.json)判据(正例走 InstallClaudeMCP 真实落盘)——

func TestIsClaudeMCPInstalled(t *testing.T) {
	// 无文件 → false
	if IsClaudeMCPInstalled(t.TempDir()) {
		t.Error("无 .mcp.json 时应返回 false")
	}
	// 全链路正例: InstallClaudeMCP 真实落盘 → 判据必须识别
	dir := t.TempDir()
	if _, err := InstallClaudeMCP(dir); err != nil {
		t.Fatalf("InstallClaudeMCP 落盘失败: %v", err)
	}
	if !IsClaudeMCPInstalled(dir) {
		t.Error("InstallClaudeMCP 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// 幂等早退(写入端复用判据端): 二次安装应报跳过且不报错
	if msg, err := InstallClaudeMCP(dir); err != nil || msg == "" {
		t.Errorf("二次安装应幂等跳过: msg=%q err=%v", msg, err)
	}
	// 全链路增强: 既有 servers 上合并安装后,既有键与 aoci 键并存且判据识别
	dir4 := t.TempDir()
	writeFile(t, dir4, ".mcp.json", `{"mcpServers":{"other":{"command":"x"}}}`)
	if _, err := InstallClaudeMCP(dir4); err != nil {
		t.Fatalf("InstallClaudeMCP 合并落盘失败: %v", err)
	}
	if !IsClaudeMCPInstalled(dir4) {
		t.Error("合并安装后判据应识别 aoci 键")
	}
	// 有 mcpServers 但无 aoci → false(边界,手写)
	dir2 := t.TempDir()
	writeFile(t, dir2, ".mcp.json", `{"mcpServers":{"other":{"command":"x"}}}`)
	if IsClaudeMCPInstalled(dir2) {
		t.Error("mcpServers 无 aoci 键时应返回 false")
	}
	// JSON 损坏 → false(不误报已装;反例,手写)
	dir3 := t.TempDir()
	writeFile(t, dir3, ".mcp.json", `{not valid json`)
	if IsClaudeMCPInstalled(dir3) {
		t.Error("JSON 损坏时应返回 false,不得误报已装")
	}
}

// —— Claude hook(.claude/settings.json)判据(正例走 InstallClaudeHook 真实落盘;
//    判据已升级为精确遍历,与写入端幂等共用 claudeSettingsHasAociHook)——

func TestIsClaudeHookInstalled(t *testing.T) {
	// 无文件 → false
	if IsClaudeHookInstalled(t.TempDir()) {
		t.Error("无 .claude/settings.json 时应返回 false")
	}
	// 全链路正例: InstallClaudeHook 真实落盘(脚本+settings 注册)→ 判据必须识别
	dir := t.TempDir()
	if _, err := InstallClaudeHook(dir); err != nil {
		t.Fatalf("InstallClaudeHook 落盘失败: %v", err)
	}
	if !IsClaudeHookInstalled(dir) {
		t.Error("InstallClaudeHook 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// 不含 aoci → false(边界,手写)
	dir2 := t.TempDir()
	writeFile(t, dir2, ".claude/settings.json", `{"hooks":{"PreToolUse":[]}}`)
	if IsClaudeHookInstalled(dir2) {
		t.Error("settings.json 不含 aoci 时应返回 false")
	}
	// 【弱点消除断言】aoci 以无关形式出现 → false:
	// 旧"含 aoci 子串"简化判据在此误报已装(文档在案的已知弱点),
	// 精确遍历判据必须不认 —— 本用例锁死弱点不复活。
	dir3 := t.TempDir()
	writeFile(t, dir3, ".claude/settings.json", `{"note":"aoci mentioned but no hook","hooks":{}}`)
	if IsClaudeHookInstalled(dir3) {
		t.Error("aoci 以无关形式出现不应被判为已装(旧子串判据弱点,已由精确遍历消除)")
	}
	// JSON 损坏 → false(不误报已装;精确判据引入解析环节后的新边界)
	dir4 := t.TempDir()
	writeFile(t, dir4, ".claude/settings.json", `{broken`)
	if IsClaudeHookInstalled(dir4) {
		t.Error("settings.json 损坏时应返回 false,不得误报已装")
	}
}

// —— Codex MCP(.codex/config.toml)判据:正例走 InstallCodexMCP 真实落盘,
//    反例重点测排除注释行(审查事故史)——

func TestIsCodexMCPInstalled(t *testing.T) {
	// 无文件 → false
	if IsCodexMCPInstalled(t.TempDir()) {
		t.Error("无 .codex/config.toml 时应返回 false")
	}
	// 全链路正例: InstallCodexMCP 真实落盘(渲染模板+新建)→ 判据必须识别
	dir := t.TempDir()
	if _, err := InstallCodexMCP(dir); err != nil {
		t.Fatalf("InstallCodexMCP 落盘失败: %v", err)
	}
	if !IsCodexMCPInstalled(dir) {
		t.Error("InstallCodexMCP 真实落盘产物应被判据识别(写入端→判据端全链路)")
	}
	// 全链路增强: 既有 TOML 内容上追加安装后,既有内容保真且判据识别
	dir6 := t.TempDir()
	writeFile(t, dir6, ".codex/config.toml", "[mcp_servers.other]\ncommand = \"x\"\n")
	if _, err := InstallCodexMCP(dir6); err != nil {
		t.Fatalf("InstallCodexMCP 追加落盘失败: %v", err)
	}
	if !IsCodexMCPInstalled(dir6) {
		t.Error("追加安装后判据应识别 aoci 表")
	}
	// 带前导空白的表头(TrimSpace 后仍应识别;边界,手写 —— 写入端产物恒顶格,
	// 此形态模拟用户手工缩进编辑)→ true
	dir2 := t.TempDir()
	writeFile(t, dir2, ".codex/config.toml", "   [mcp_servers.aoci]\n")
	if !IsCodexMCPInstalled(dir2) {
		t.Error("带前导空白的表头 TrimSpace 后应识别为已装")
	}
	// 【关键防回归】仅有注释行 #[mcp_servers.aoci] → false(审查事故的核心用例;反例,手写)
	dir3 := t.TempDir()
	writeFile(t, dir3, ".codex/config.toml", "# [mcp_servers.aoci] 这是注释不算安装\nother = 1\n")
	if IsCodexMCPInstalled(dir3) {
		t.Error("注释行 #[mcp_servers.aoci] 绝不能被判为已装(审查修正的核心)")
	}
	// 紧贴井号无空格的注释 #[mcp_servers.aoci] → false(反例,手写)
	dir4 := t.TempDir()
	writeFile(t, dir4, ".codex/config.toml", "#[mcp_servers.aoci]\n")
	if IsCodexMCPInstalled(dir4) {
		t.Error("紧贴井号的注释 #[mcp_servers.aoci] 也绝不能判为已装")
	}
	// 无 aoci 表头 → false(边界,手写)
	dir5 := t.TempDir()
	writeFile(t, dir5, ".codex/config.toml", "[mcp_servers.other]\ncommand = \"x\"\n")
	if IsCodexMCPInstalled(dir5) {
		t.Error("仅含其他 server 表头时应返回 false")
	}
}
