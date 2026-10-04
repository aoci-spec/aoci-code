// WorkBuddy 机器级用户 MCP 配置安装测试
// 索引条目: workbuddy_test.go[Hook.WorkBuddyTest.7.S]
//
// 承重判据(每条都有对应的"故意违规"用例):
//   - 幂等: 同一仓库连装两次,第二次必须早退且不改动文件字节;
//   - 键名退让: 已有 aoci 键绑别的仓库时,必须新增 aoci-<项目名> 而不是覆盖 ——
//     该文件被所有项目共享,覆盖会静默改掉别的仓库的接入;
//   - 人工边界: 退让键也被别的仓库占用时报错,不猜、不覆盖;
//   - 不毁坏既有配置: 既有 servers、既有顶层字段、损坏的 JSON 都必须原样保留;
//   - 判据不含糊: 别的仓库的 aoci 条目不算"本仓库已装";文件损坏判未装。
package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// isolateHome 把用户家目录指向临时目录,使测试不碰真实的 ~/.workbuddy。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// readWorkBuddyConfig 读回测试写入的机器级 MCP 文件。
func readWorkBuddyConfig(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".workbuddy", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatal("mcpServers 缺失")
	}
	return servers
}

func serverKeys(servers map[string]any) []string {
	out := make([]string, 0, len(servers))
	for k := range servers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestInstallWorkBuddyMCPWritesServerAndIsIdempotent(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()

	if _, err := InstallWorkBuddyMCP(root); err != nil {
		t.Fatal(err)
	}
	servers := readWorkBuddyConfig(t, home)
	if _, ok := servers[workbuddyServerKey]; !ok {
		t.Fatalf("aoci 键未写入: %v", serverKeys(servers))
	}
	if !IsWorkBuddyMCPInstalled(root) {
		t.Fatal("写入后判据仍为未安装")
	}

	// 幂等: 第二次必须早退, 且不得改动文件字节。
	path := filepath.Join(home, ".workbuddy", "mcp.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallWorkBuddyMCP(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("第二次安装改动了文件, 幂等被破坏")
	}
}

func TestInstallWorkBuddyMCPKeepsForeignAociEntry(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	other := t.TempDir()

	// 先装另一个仓库, 占据默认 aoci 键。
	if _, err := InstallWorkBuddyMCP(other); err != nil {
		t.Fatal(err)
	}
	foreign, err := json.Marshal(readWorkBuddyConfig(t, home)[workbuddyServerKey])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := InstallWorkBuddyMCP(root); err != nil {
		t.Fatal(err)
	}
	servers := readWorkBuddyConfig(t, home)

	// 别人的条目必须逐字未动。
	kept, err := json.Marshal(servers[workbuddyServerKey])
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(foreign) {
		t.Fatalf("别人的 aoci 条目被改写:\n before %s\n after  %s", foreign, kept)
	}
	// 本仓库拿到自己的退让键, 判据只对本仓库为真。
	scoped := workbuddyServerKey + "-" + workbuddyKeySuffix(filepath.Base(root))
	if _, ok := servers[scoped]; !ok {
		t.Fatalf("未写入退让键 %q: %v", scoped, serverKeys(servers))
	}
	if !IsWorkBuddyMCPInstalled(root) {
		t.Fatal("退让键写入后判据仍为未安装")
	}
}

func TestWorkBuddyTargetKeyRejectsOccupiedScopedKey(t *testing.T) {
	isolateHome(t)
	first := t.TempDir()
	if _, err := InstallWorkBuddyMCP(first); err != nil {
		t.Fatal(err)
	}
	// 造出第二个仓库: 默认键已被占, 于是落到 aoci-<目录名>。
	second := t.TempDir()
	if _, err := InstallWorkBuddyMCP(second); err != nil {
		t.Fatal(err)
	}
	scoped := workbuddyServerKey + "-" + workbuddyKeySuffix(filepath.Base(second))
	if !IsWorkBuddyMCPInstalled(second) {
		t.Fatalf("第二个仓库未写入退让键 %q", scoped)
	}
	// 第三个仓库目录名与第二个相同 ⇒ 退让键撞车 ⇒ 必须报错交人工。
	collide := filepath.Join(t.TempDir(), filepath.Base(second))
	if err := os.MkdirAll(collide, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := InstallWorkBuddyMCP(collide)
	if err == nil {
		t.Fatal("退让键被别的仓库占用时必须报错, 绝不覆盖")
	}
	if !strings.Contains(err.Error(), scoped) {
		t.Fatalf("报错文案应点明冲突键名 %q, 实际: %v", scoped, err)
	}
}

func TestInstallWorkBuddyMCPRejectsBrokenJSON(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	dir := filepath.Join(home, ".workbuddy")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallWorkBuddyMCP(root); err == nil {
		t.Fatal("损坏的 JSON 必须报错, 绝不覆盖用户的坏文件")
	}
	raw, err := os.ReadFile(broken)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{not json" {
		t.Fatalf("坏文件被改写: %q", string(raw))
	}
}

func TestInstallWorkBuddyMCPPreservesExistingConfig(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	dir := filepath.Join(home, ".workbuddy")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mcp.json")
	seed := `{"mcpServers":{"other":{"command":"node","args":["x.js"]}},"note":"keep me"}`
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallWorkBuddyMCP(root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("既有 server 被丢弃: %v", serverKeys(servers))
	}
	if doc["note"] != "keep me" {
		t.Fatalf("既有顶层字段被丢弃: %v", doc)
	}
}

func TestIsWorkBuddyMCPInstalledRejectsOtherRepoAndBadFile(t *testing.T) {
	home := isolateHome(t)
	installed := t.TempDir()
	if _, err := InstallWorkBuddyMCP(installed); err != nil {
		t.Fatal(err)
	}
	if IsWorkBuddyMCPInstalled(t.TempDir()) {
		t.Fatal("别的仓库不得被判为已安装")
	}
	// 判据原则: 任何解析失败一律 false(宁可报未装, 不给虚假安全感)。
	if err := os.WriteFile(filepath.Join(home, ".workbuddy", "mcp.json"), []byte("[[["), 0600); err != nil {
		t.Fatal(err)
	}
	if IsWorkBuddyMCPInstalled(installed) {
		t.Fatal("损坏文件必须判为未安装, 不误报已装")
	}
}

func TestDetectAndDispatchWorkBuddy(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()

	// WorkBuddy 在项目内没有任何配置文件, Detect 只能查用户家目录。
	if err := os.MkdirAll(filepath.Join(home, ".workbuddy"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".workbuddy", "mcp.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if !containsString(Detect(root), "workbuddy") {
		t.Fatalf("Detect 未发现 workbuddy: %v", Detect(root))
	}
	// 走 Install 分发; withHooks=true 必须被静默忽略而不是报错或假装安装。
	if _, err := Install(root, "workbuddy", true); err != nil {
		t.Fatal(err)
	}
	if !IsWorkBuddyMCPInstalled(root) {
		t.Fatal("Install 分发未写入配置")
	}
	// 仓库内不得留下任何宿主配置文件。
	for _, name := range []string{".mcp.json", ".workbuddy", ".codex", "opencode.json", ".claude"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Fatalf("仓库内不该出现 %s", name)
		}
	}
}

func TestWorkBuddyKeySuffixSanitizes(t *testing.T) {
	cases := map[string]string{
		"MindInit":      "mindinit",
		"My Project":    "my-project",
		"a.b_c":         "a-b-c",
		"///":           "repo",
		"项目":            "repo",
		"trailing-":     "trailing",
		"-lead-and-end": "lead-and-end",
	}
	for in, want := range cases {
		if got := workbuddyKeySuffix(in); got != want {
			t.Fatalf("workbuddyKeySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureAgentsBlockAtPrependPutsBlockFirst(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	original := "# 项目标题\n\n第一段。\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentsBlockAt(root, AgentsBlockPrepend); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, agentsBegin) {
		t.Fatalf("区块未落在文首(宿主只注入开头一段, 落文末读不到):\n%.120s", text)
	}
	if !strings.Contains(text, original) {
		t.Fatal("原有内容被丢弃")
	}
	// 幂等: 已有区块时再调用只整块替换, 位置不得变动。
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentsBlockAt(root, AgentsBlockPrepend); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("已有区块时重复调用改动了文件")
	}
}

func TestEnsureAgentsBlockAtAppendStaysAtEnd(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	original := "# 标题\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	// 默认落位必须仍是文末追加 —— 不得让 workbuddy 适配改变其它宿主行为。
	if _, err := EnsureAgentsBlockAt(root, AgentsBlockAppend); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), original) {
		t.Fatal("默认落位必须仍是文末追加")
	}
	if !strings.Contains(string(raw), agentsBegin) {
		t.Fatal("区块缺失")
	}
}
