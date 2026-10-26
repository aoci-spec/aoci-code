// loadIndexForCLI 的 RelPath 填充防线(entries diff 假新增缺陷防再犯,2026-07-10)。
// header apply 闸门链路测试随 Legacy 写作面一起退役(0.2.0)。
// 索引条目: index_test.go(待补录)
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func TestLoadIndexForCLI_ResolvesRelPaths(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	idx := "#旧头部第一行\n#旧头部第二行\n===段" + rootSlash + "/===\nf.go[XC5T]: F:x | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load 失败: %v", err)
	}
	cmd := newHeaderShowCmd() // 任取一个 cobra 命令载体,仅为提供 ErrOrStderr
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	doc, idxPath, err := loadIndexForCLI(cmd, root, cfg)
	if err != nil {
		t.Fatalf("loadIndexForCLI 失败: %v", err)
	}
	if idxPath != filepath.Join(root, "aoci.txt") {
		t.Errorf("索引路径不符: %q", idxPath)
	}

	hit := index.FindEntry(doc, "f.go")
	if hit == nil {
		t.Fatal("FindEntry(f.go) 应命中 —— RelPath 未被填充(ResolveRelPaths 被漏掉,假新增缺陷复发)")
	}
	if !strings.Contains(hit.FullLine, "f.go[XC5T]") {
		t.Errorf("命中条目 FullLine 不符: %q", hit.FullLine)
	}
}
