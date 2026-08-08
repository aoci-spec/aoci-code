// Codex 项目级 MCP 配置安装测试
// 索引条目: codex_test.go[TCX8TS]
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/aoci-spec/aoci-code/textassets"
)

func containsHan(value string) bool {
	for _, current := range value {
		if unicode.Is(unicode.Han, current) {
			return true
		}
	}
	return false
}

func TestEnglishHostIntegrationTemplatesContainNoChinese(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if _, err := InstallCodexMCP(root); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClaudeHook(root); err != nil {
		t.Fatal(err)
	}
	cursor, err := Install(root, "cursor", false)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, ".codex", "config.toml"),
		filepath.Join(root, ".aoci", "hooks", "claude-pretool.sh"),
	}
	combined := cursor
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		combined += string(data)
	}
	if containsHan(combined) {
		t.Fatalf("English host integration output contains Chinese text:\n%s", combined)
	}
	if !strings.Contains(combined, "[mcp_servers.aoci]") ||
		!strings.Contains(combined, "PreToolUse") ||
		!strings.Contains(combined, "codex mcp add aoci") {
		t.Fatalf("English host integration output lost protocol anchors:\n%s", combined)
	}
}

// TestInstallCodexMCPCreate 目标不存在: 新建并含本表与二进制路径
func TestInstallCodexMCPCreate(t *testing.T) {
	root := t.TempDir()
	msg, err := InstallCodexMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "已创建") {
		t.Fatalf("首次安装应为新建: %q", msg)
	}
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[mcp_servers.aoci]") || !strings.Contains(text, "--repo") {
		t.Fatalf("生成内容缺关键项: %q", text)
	}
}

// TestInstallCodexMCPAppend 既有配置: 追加本表且原内容字节级保真
func TestInstallCodexMCPAppend(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// 既有用户配置: 顶级键 + 另一 MCP server 表(模拟真实混合场景)
	existing := "model = \"o4-mini\"\n\n[mcp_servers.other]\ncommand = \"/usr/bin/other\"\nargs = [\"run\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	msg, err := InstallCodexMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "已追加") {
		t.Fatalf("既有文件应为追加: %q", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	text := string(data)
	// 原内容必须作为前缀原样保留(一字节不动)
	if !strings.HasPrefix(text, existing) {
		t.Fatal("既有配置未字节级保真")
	}
	if !strings.Contains(text, "[mcp_servers.aoci]") || !strings.Contains(text, "[mcp_servers.other]") {
		t.Fatal("追加后应同时含既有表与 aoci 表")
	}
}

// TestInstallCodexMCPIdempotent 已含本表: 跳过且文件零改动
func TestInstallCodexMCPIdempotent(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallCodexMCP(root); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	msg, err := InstallCodexMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "跳过") {
		t.Fatalf("重复安装应跳过: %q", msg)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if string(before) != string(after) {
		t.Fatal("幂等跳过不得改动文件")
	}
}

// TestInstallCodexMCPCommentedMarker 注释行免疫: 被注释的表头不得触发幂等跳过(审查回归)
func TestInstallCodexMCPCommentedMarker(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// 用户注释掉了曾经的 aoci 配置: 应视为"未安装",走追加路径
	existing := "# 旧配置已停用\n#[mcp_servers.aoci]\n#command = \"/old/aoci\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	msg, err := InstallCodexMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "已追加") {
		t.Fatalf("注释行不得误判为已安装,应走追加: %q", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	text := string(data)
	if !strings.HasPrefix(text, existing) {
		t.Fatal("既有注释内容未保真")
	}
	if !hasCodexTable(text) {
		t.Fatal("追加后应含真实的 aoci 表")
	}
}

// TestInstallViaEntry Install 编排层: codex 分支输出含 AGENTS 生效说明与 hook 事实说明
func TestInstallViaEntry(t *testing.T) {
	root := t.TempDir()
	out, err := Install(root, "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "AGENTS.md 纪律区块对 Codex 原生生效") {
		t.Fatalf("应说明 AGENTS.md 原生生效: %q", out)
	}
	if !strings.Contains(out, "hook 暂不安装") {
		t.Fatalf("--hooks 下应说明 hook 暂不装及理由: %q", out)
	}
	// cursor 仍为占位不写文件
	out2, err := Install(root, "cursor", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "占位") {
		t.Fatalf("cursor 应维持占位: %q", out2)
	}
	if _, serr := os.Stat(filepath.Join(root, ".cursor", "mcp.json")); serr == nil {
		t.Fatal("cursor 占位不得写文件")
	}
}
