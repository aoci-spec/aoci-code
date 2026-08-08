// A2 存量仓排除缺项提示判决测试(missingTechnicalExcludeDirs 纯函数级)。
//
// 判决:
//  1. 显式短清单 → 缺项列表含新默认技术产物项;
//  2. exclude_dirs 键未声明 → 零缺项(Load 回填默认,直接对比会误报的防线);
//  3. 显式清单已含全部默认项 → 零缺项;
//  4. config.json 不存在 → 零缺项(不重复报告)。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func writeDoctorHintConfig(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(
		filepath.Join(root, ".aoci"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		config.FilePath(root), []byte(content), 0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestMissingTechnicalExcludeDirs(t *testing.T) {
	// 判决1: 显式短清单 → 报缺项
	root := t.TempDir()
	writeDoctorHintConfig(t, root,
		`{"version":1,"exclude_dirs":["node_modules"]}`+"\n")
	missing := missingTechnicalExcludeDirs(root)
	found := map[string]bool{}
	for _, m := range missing {
		found[m] = true
	}
	for _, want := range []string{".ruff_cache", ".tox"} {
		if !found[want] {
			t.Fatalf("显式短清单应报缺项 %s: %v", want, missing)
		}
	}

	// 判决2: 键未声明 → 零缺项(误报防线)
	root2 := t.TempDir()
	writeDoctorHintConfig(t, root2, `{"version":1}`+"\n")
	if got := missingTechnicalExcludeDirs(root2); len(got) != 0 {
		t.Fatalf("键未声明时不得报缺项(Load 已回填默认): %v", got)
	}

	// 判决3: 显式清单已含全部默认项 → 零缺项
	root3 := t.TempDir()
	cfg := legacyTestConfig()
	if err := os.MkdirAll(
		filepath.Join(root3, ".aoci"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root3, cfg); err != nil {
		t.Fatal(err)
	}
	if got := missingTechnicalExcludeDirs(root3); len(got) != 0 {
		t.Fatalf("全量默认落盘不得报缺项: %v", got)
	}

	// 判决4: config.json 不存在 → 零缺项
	root4 := t.TempDir()
	if got := missingTechnicalExcludeDirs(root4); len(got) != 0 {
		t.Fatalf("无配置文件不得报缺项: %v", got)
	}
}
