package safety

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func publicTextScriptPath() string {
	return filepath.Join(
		"..",
		"..",
		"scripts",
		"check-public-text.sh",
	)
}

// requirePublicTextBash返回当前环境可执行的Bash路径。
//
// 公开文案脚本属于Linux CI和类Unix开发环境中的发布闸门。Windows运行时
// 不依赖该脚本；当本机没有Bash时跳过脚本执行测试，但仍保留静态扫描边界测试。
func requirePublicTextBash(
	t *testing.T,
) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script execution is covered by Linux CI; Windows keeps the static contract tests")
	}

	bashPath, err := exec.LookPath(
		"bash",
	)
	if err != nil {
		t.Skip(
			"当前环境没有Bash；公开文案脚本执行由Linux CI覆盖",
		)
	}

	return bashPath
}

func TestPublicTextScriptDelegatesMachineContractsToGo(
	t *testing.T,
) {
	data, err := os.ReadFile(
		publicTextScriptPath(),
	)
	if err != nil {
		t.Fatalf(
			"read public text script: %v",
			err,
		)
	}

	text := string(data)

	for _, anchor := range []string{
		"exec \"$GO_BIN\" run ./internal/safetycmd",
		"GO_BIN=",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"public text script is missing scan boundary %q",
				anchor,
			)
		}
	}

	for _, forbidden := range []string{"SUB_TERMS", "WORD_TERMS"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Shell兼容入口不得保留机器集合%q", forbidden)
		}
	}
	for _, term := range machinecontract.PublicTextTerms() {
		if strings.Contains(text, term.Text) {
			t.Fatalf("Shell兼容入口重复了机器词表项%q", term.Text)
		}
	}
}

func TestPublicTextFilesOwnsDefaultScanBoundary(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := PublicTextFiles(repoRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, candidate := range files {
		relative, relErr := filepath.Rel(repoRoot, candidate)
		if relErr != nil {
			t.Fatal(relErr)
		}
		seen[filepath.ToSlash(relative)] = true
		for _, denied := range []string{"experiments/", "research/", "spec/private/"} {
			if strings.HasPrefix(filepath.ToSlash(relative), denied) {
				t.Fatalf("非公开资产不得进入生产公开文案扫描集: %s", relative)
			}
		}
	}
	for _, required := range []string{
		"README.md",
		"docs/windows-host-agent.md",
		"textassets/en-US/templates/claude-pretool.sh.tmpl",
		"textassets/en-US/templates/codex-cursor-stubs.txt.tmpl",
		"textassets/en-US/templates/codex-mcp.toml.tmpl",
		"textassets/zh-CN/templates/claude-pretool.sh.tmpl",
		"textassets/zh-CN/templates/codex-cursor-stubs.txt.tmpl",
		"textassets/zh-CN/templates/codex-mcp.toml.tmpl",
		"textassets/zh-CN/contracts/runtime-rules.txt",
		"spec/public/aoci-index-format-v1.txt",
		"spec/public/aoci-mcp-runtime-v1.txt",
		"internal/mcptools/server.go",
	} {
		if !seen[required] {
			t.Errorf("默认公开文案扫描集缺少%s", required)
		}
	}
}

func TestPublicTextFilesExcludePrivateSpecBoundary(t *testing.T) {
	root := t.TempDir()
	publicPath := filepath.Join(root, "spec", "public", "runtime.txt")
	privatePath := filepath.Join(root, "spec", "private", "design.txt")
	for path, content := range map[string]string{
		publicPath:  "public runtime contract\n",
		privatePath: "private design review\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := PublicTextFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Clean(files[0]) != filepath.Clean(publicPath) {
		t.Fatalf("public scan crossed spec boundary: %#v", files)
	}
	for _, explicit := range [][]string{{privatePath}, {publicPath, privatePath}} {
		files, err = PublicTextFiles(root, explicit)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) > 1 || len(files) == 1 && filepath.Clean(files[0]) != filepath.Clean(publicPath) {
			t.Fatalf("explicit scan crossed spec boundary: %#v", files)
		}
	}
}

func TestPublicTextScriptScansExplicitFiles(
	t *testing.T,
) {
	bashPath := requirePublicTextBash(
		t,
	)
	root := t.TempDir()

	cleanPath := filepath.Join(
		root,
		"clean.txt",
	)
	blockedPath := filepath.Join(
		root,
		"blocked.txt",
	)

	if err := os.WriteFile(
		cleanPath,
		[]byte(
			"AOCI provides governed repository cognition.\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		blockedPath,
		[]byte(
			"This text claims zero defects.\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cleanCommand := exec.Command(
		bashPath,
		publicTextScriptPath(),
		cleanPath,
	)

	cleanOutput, cleanErr :=
		cleanCommand.CombinedOutput()

	if cleanErr != nil {
		t.Fatalf(
			"clean public text should pass: %v\n%s",
			cleanErr,
			cleanOutput,
		)
	}

	blockedCommand := exec.Command(
		bashPath,
		publicTextScriptPath(),
		blockedPath,
	)

	blockedOutput, blockedErr :=
		blockedCommand.CombinedOutput()

	if blockedErr == nil {
		t.Fatalf(
			"forbidden public claim should fail:\n%s",
			blockedOutput,
		)
	}

	if !strings.Contains(
		string(blockedOutput),
		"zero defects",
	) {
		t.Fatalf(
			"failure output should identify the forbidden claim:\n%s",
			blockedOutput,
		)
	}
}

func TestPublicTextScriptDefaultScanPasses(
	t *testing.T,
) {
	bashPath := requirePublicTextBash(
		t,
	)

	command := exec.Command(
		bashPath,
		publicTextScriptPath(),
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"default public text scan should pass: %v\n%s",
			err,
			output,
		)
	}

	text := string(output)

	if !strings.Contains(
		text,
		"safety: 全部干净",
	) {
		t.Fatalf(
			"default scan did not report success:\n%s",
			text,
		)
	}
}
