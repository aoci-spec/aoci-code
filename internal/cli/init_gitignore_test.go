// aoci init生成.aoci/.gitignore的资产边界测试。
package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func legacyGitignoreAsset(t *testing.T) string {
	t.Helper()
	value, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateAOCIGitignore)
	if err != nil {
		t.Fatalf("load legacy gitignore asset: %v", err)
	}
	return value
}

// TestInitCreatesRuntimeGitignore验证首次init生成精确白名单内容。
func TestInitCreatesRuntimeGitignore(
	t *testing.T,
) {
	root := t.TempDir()

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf(
			"init失败: %v",
			err,
		)
	}

	path := filepath.Join(
		root,
		".aoci",
		".gitignore",
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(
			"读取.aoci/.gitignore失败: %v",
			err,
		)
	}

	if string(data) != legacyGitignoreAsset(t) {
		t.Fatalf(
			".aoci/.gitignore内容不符:\n%s",
			data,
		)
	}
}

// TestInitPreservesExistingRuntimeGitignore验证维护者文件绝不覆盖。
func TestInitPreservesExistingRuntimeGitignore(
	t *testing.T,
) {
	root := t.TempDir()
	path := filepath.Join(
		root,
		".aoci",
		".gitignore",
	)

	if err := os.MkdirAll(
		filepath.Dir(path),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	customContent :=
		"# 维护者自定义边界\ncustom-runtime/\n"

	if err := os.WriteFile(
		path,
		[]byte(customContent),
		0o640,
	); err != nil {
		t.Fatal(err)
	}

	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf(
			"init失败: %v",
			err,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != customContent {
		t.Fatalf(
			"已有.aoci/.gitignore被覆盖:\n%s",
			data,
		)
	}

	if infoAfter.Mode().Perm() !=
		infoBefore.Mode().Perm() {
		t.Fatalf(
			"跳过已有文件时权限不得变化: before=%o after=%o",
			infoBefore.Mode().Perm(),
			infoAfter.Mode().Perm(),
		)
	}
}

// TestInitRuntimeGitignoreGitBehavior使用真实Git锁定默认拒绝和正式资产白名单。
func TestInitRuntimeGitignoreGitBehavior(
	t *testing.T,
) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("测试环境无git可执行文件")
	}

	root := t.TempDir()

	command := exec.Command(
		gitPath,
		"-C",
		root,
		"init",
		"--quiet",
	)
	if output, runErr :=
		command.CombinedOutput(); runErr != nil {
		t.Fatalf(
			"git init失败: %v\n%s",
			runErr,
			output,
		)
	}

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf(
			"init失败: %v",
			err,
		)
	}

	runtimeFiles := map[string]string{
		".aoci/ledger.jsonl":                 "{}\n",
		".aoci/reports.jsonl":                "{}\n",
		".aoci/lock":                         "lock\n",
		".aoci/baseline.json.bak":            "{}\n",
		".aoci/unknown-runtime.bin":          "runtime\n",
		".aoci/verify_history/result.txt":    "verify\n",
		".aoci/drafts/run/manifest.json":     "{}\n",
		".aoci/hooks/pretool.sh":             "# hook\n",
		".aoci/tmp/intermediate-result.json": "{}\n",
	}

	for relativePath, content := range runtimeFiles {
		writeInitGitignoreFixture(
			t,
			root,
			relativePath,
			content,
		)

		assertGitIgnoreState(
			t,
			gitPath,
			root,
			relativePath,
			true,
		)
	}

	formalFiles := map[string]string{
		".aoci/.gitignore":    legacyGitignoreAsset(t),
		".aoci/config.json":   "{}\n",
		".aoci/baseline.json": "{}\n",
		".aoci/curation.json": "{}\n",
	}

	for relativePath, content := range formalFiles {
		writeInitGitignoreFixture(
			t,
			root,
			relativePath,
			content,
		)

		assertGitIgnoreState(
			t,
			gitPath,
			root,
			relativePath,
			false,
		)
	}
}

func writeInitGitignoreFixture(
	t *testing.T,
	root string,
	relativePath string,
	content string,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(
			relativePath,
		),
	)

	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		absolutePath,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func assertGitIgnoreState(
	t *testing.T,
	gitPath string,
	root string,
	relativePath string,
	wantIgnored bool,
) {
	t.Helper()

	command := exec.Command(
		gitPath,
		"-C",
		root,
		"check-ignore",
		"-q",
		"--",
		filepath.ToSlash(relativePath),
	)

	err := command.Run()
	ignored := err == nil

	if err != nil {
		var exitError *exec.ExitError

		if !errors.As(
			err,
			&exitError,
		) ||
			exitError.ExitCode() != 1 {
			t.Fatalf(
				"git check-ignore异常 %s: %v",
				relativePath,
				err,
			)
		}
	}

	if ignored != wantIgnored {
		t.Fatalf(
			"Git忽略状态不符 %s: got=%v want=%v",
			relativePath,
			ignored,
			wantIgnored,
		)
	}
}
