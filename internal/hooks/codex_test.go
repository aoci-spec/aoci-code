// Codex 项目级 MCP 配置安装测试
// 索引条目: codex_test.go[TCX8TS]
package hooks

import (
	"os"
	"path/filepath"
	"strconv"
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
	if _, err := Install(root, "codex", true); err != nil {
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
		!strings.Contains(combined, "compact_prompt") ||
		!strings.Contains(combined, "[[hooks.SessionStart]]") ||
		!strings.Contains(combined, "hook codex-compact") ||
		!strings.Contains(combined, "PreToolUse") ||
		!strings.Contains(combined, "codex mcp add aoci") {
		t.Fatalf("English host integration output lost protocol anchors:\n%s", combined)
	}
}

func TestInstallCodexCompactionPreservesBytesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	existing := "model = \"o4-mini\"\n\n[mcp_servers.other]\ncommand = \"/usr/bin/other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	msg, err := Install(root, "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "/hooks") {
		t.Fatalf("install result must explain Codex hook trust: %q", msg)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(first)
	if !strings.Contains(text, existing) {
		t.Fatal("existing Codex configuration bytes were not preserved contiguously")
	}
	promptIndex := strings.Index(text, codexCompactPromptMarker)
	existingIndex := strings.Index(text, existing)
	hookIndex := strings.Index(text, codexCompactHookMarker)
	if promptIndex < 0 || existingIndex < 0 || hookIndex < 0 ||
		!(promptIndex < existingIndex && existingIndex < hookIndex) {
		t.Fatalf("managed prompt must precede existing bytes and hook must follow them:\n%s", text)
	}
	defaultPrompt := `You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.`
	templateData := NewTplData(root)
	for _, anchor := range []string{
		defaultPrompt,
		"cognition_receipt",
		"[[hooks.SessionStart]]",
		`matcher = "^compact$"`,
		"command = " + strconv.Quote(templateData.CodexCompactCommand),
		"command_windows = " + strconv.Quote(templateData.CodexCompactCommandWindows),
	} {
		if !strings.Contains(text, anchor) {
			t.Fatalf("installed Codex compaction configuration lacks %q:\n%s", anchor, text)
		}
	}

	secondMsg, err := Install(root, "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondMsg, "/hooks") {
		t.Fatalf("idempotent result must retain trust guidance: %q", secondMsg)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("repeated Codex compaction installation changed config.toml")
	}
	if strings.Count(string(second), codexCompactPromptMarker) != 1 ||
		strings.Count(string(second), codexCompactHookMarker) != 1 {
		t.Fatalf("managed Codex compaction snippets were duplicated:\n%s", second)
	}
}

func TestInstallCodexCompactionConflictsAreAtomic(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		key      string
	}{
		{
			name:     "custom compact prompt",
			existing: "model = \"o4-mini\"\ncompact_prompt = \"keep mine\"\n",
			key:      "compact_prompt",
		},
		{
			name:     "experimental prompt file",
			existing: "experimental_compact_prompt_file = \"/tmp/mine.txt\"\n",
			key:      "experimental_compact_prompt_file",
		},
		{
			name:     "BOM custom compact prompt",
			existing: codexUTF8BOM + "compact_prompt = \"keep mine\"\n",
			key:      "compact_prompt",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".codex")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(path, []byte(tc.existing), 0644); err != nil {
				t.Fatal(err)
			}

			if _, err := Install(root, "codex", true); err == nil ||
				!strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected %s conflict, got %v", tc.key, err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tc.existing {
				t.Fatalf("conflicting install changed config.toml:\n%s", after)
			}
			backups, err := filepath.Glob(path + ".backup.*")
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 0 {
				t.Fatalf("conflicting install created backups despite zero-write preflight: %v", backups)
			}
			if strings.Contains(string(after), codexMarker) {
				t.Fatal("MCP configuration was written before compaction conflict detection")
			}
		})
	}
}

func TestInstallCodexCompactionRejectsIncompleteManagedHook(t *testing.T) {
	for _, removePrefix := range []string{"command_windows =", codexCompactHookMarker} {
		t.Run(removePrefix, func(t *testing.T) {
			root := t.TempDir()
			if _, err := Install(root, "codex", true); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".codex", "config.toml")
			existing, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(existing), "\n")
			filtered := lines[:0]
			for _, line := range lines {
				if !strings.HasPrefix(strings.TrimSpace(line), removePrefix) {
					filtered = append(filtered, line)
				}
			}
			broken := strings.Join(filtered, "\n")
			if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := Install(root, "codex", true); err == nil ||
				!strings.Contains(err.Error(), "SessionStart(compact)") {
				t.Fatalf("expected incomplete managed hook conflict, got %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != broken {
				t.Fatal("incomplete managed hook conflict changed config.toml")
			}
		})
	}
}

func TestInstallCodexCompactionPreservesUTF8BOM(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(codexUTF8BOM+"model = \"o4-mini\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "codex", true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), codexUTF8BOM+codexCompactPromptMarker) ||
		strings.Count(string(after), codexUTF8BOM) != 1 {
		t.Fatalf("UTF-8 BOM was not preserved at byte zero:\n%q", after)
	}
}

func TestCodexCompactCommandsQuoteHostPaths(t *testing.T) {
	if got, want := quotePOSIXShellArgument("repo'$(touch injected)"),
		`'repo'"'"'$(touch injected)'`; got != want {
		t.Fatalf("POSIX hook argument is not safely quoted: got %q want %q", got, want)
	}
	if got, want := quotePowerShellArgument("repo'$env:TEMP"),
		`'repo''$env:TEMP'`; got != want {
		t.Fatalf("PowerShell hook argument is not safely quoted: got %q want %q", got, want)
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
	if !strings.Contains(out, "/hooks") {
		t.Fatalf("--hooks 下应提示审查并信任 Codex hook: %q", out)
	}
	codexConfig, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexConfig), codexCompactPromptMarker) ||
		!strings.Contains(string(codexConfig), codexCompactHookMarker) {
		t.Fatalf("--hooks 应安装 Codex 压缩提示与 SessionStart hook:\n%s", codexConfig)
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
