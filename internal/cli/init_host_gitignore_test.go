// init 写出的宿主配置带本机绝对路径, 提交后别人的 init 只按 key 存在与否判定,
// 会幂等早退、静默空转。更硬的约束是时机: Managed Scope 的角色在首次 scan 时定
// 下, 此后 scan --force 不能推进, 摘除要走覆盖缩减的 Scope Change 审批 —— 所以
// 忽略必须由 init 在 scan 之前写好。这里钉死写入规则与那条时机。
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		if out, err := command.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"),
		[]byte("package src\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func TestInitIgnoresTheHostConfigItJustWrote(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	ignore := readGitignore(t, root)
	if !strings.Contains(ignore, hostConfigIgnoreMarker) {
		t.Fatalf("init must open a marked block it owns:\n%s", ignore)
	}
	if !strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("the written host config must be ignored:\n%s", ignore)
	}

	// 时机才是重点: scan 之后它不能是受管对象。
	var out, errOut bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); code != ExitOK {
		t.Fatalf("scan failed: %d %s %s", code, out.String(), errOut.String())
	}
	var baselineDoc struct {
		Files map[string]struct {
			Role string `json:"role"`
		} `json:"files"`
	}
	raw, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &baselineDoc); err != nil {
		t.Fatal(err)
	}
	if _, present := baselineDoc.Files[".codex/config.toml"]; present {
		t.Fatal("a machine-bound host config must never enter Managed Scope")
	}
	if role := baselineDoc.Files[".gitignore"].Role; role != "index" {
		t.Fatalf(".gitignore is ordinary tracked project content, got role %q", role)
	}
}

func TestInitAppendsToAnExistingGitignoreWithoutTouchingIt(t *testing.T) {
	root := initGitRepo(t)
	original := "# maintainer owned\nbuild/\n*.log\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	ignore := readGitignore(t, root)
	if !strings.HasPrefix(ignore, original) {
		t.Fatalf("maintainer content must be preserved byte-for-byte:\n%s", ignore)
	}
	if !strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("the host config must still be appended:\n%s", ignore)
	}
}

func TestInitNeverOverridesAnAlreadyTrackedHostConfig(t *testing.T) {
	root := initGitRepo(t)
	// 维护者已经明确决定提交这个文件: init 不替他改主意。
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte("# theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".codex/config.toml"}, {"commit", "-qm", "keep"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readGitignore(t, root), ".codex/config.toml") {
		t.Fatal("a tracked path must not be ignored behind the maintainer's back")
	}
}

func TestInitHostGitignoreIsIdempotentAndRespectsExistingCoverage(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	first := readGitignore(t, root)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if second := readGitignore(t, root); second != first {
		t.Fatalf("a repeated init must not rewrite the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// Counted as exact lines, not as a substring: the host block also carries a
	// distinct `.codex/config.toml.backup.*` pattern, which contains the path but
	// is not a duplicate entry for it. The property under test is that one path
	// is never written twice.
	exact := 0
	for _, line := range strings.Split(first, "\n") {
		if strings.TrimSpace(line) == ".codex/config.toml" {
			exact++
		}
	}
	if exact != 1 {
		t.Fatalf("the path must appear exactly once, got %d:\n%s", exact, first)
	}
	if !strings.Contains(first, ".codex/config.toml.backup.*") {
		t.Fatalf("the host block does not cover the machine-bound backup the installer writes:\n%s", first)
	}

	// 目录形式的既有覆盖也算数, 不重复写。
	other := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(other, ".gitignore"), []byte(".codex/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, other, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if ignore := readGitignore(t, other); strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("an existing directory rule already covers it:\n%s", ignore)
	}
}

func TestInitWithoutAgentWritesNoHostIgnore(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if ignore := readGitignore(t, root); strings.Contains(ignore, hostConfigIgnoreMarker) {
		t.Fatalf("no host configuration was written, so nothing may be ignored:\n%s", ignore)
	}
}
