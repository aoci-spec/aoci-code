// check 对 tagparse Warning 的放行策略回归测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func TestCheckTagParseWarningDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n" +
		"f.go[UAU8]: F:目标文件 | R:- | A:- | S:-\n"

	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package f\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("测试快照不应产生警告: %v", warnings)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}

	doc, parseWarnings := index.Parse(indexText)
	if len(parseWarnings) != 0 {
		t.Fatalf("测试索引不应产生解析器警告: %+v", parseWarnings)
	}
	score, err := indexgen.BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	tagparse := dimByName(score, "tagparse")
	if tagparse.Bad != 1 || len(tagparse.Samples) != 1 || tagparse.Samples[0] != "f.go" {
		t.Fatalf("tagparse 应可见1条 f.go,实得 %+v", tagparse)
	}

	out, err := runCheck(t, root)
	if err != nil {
		t.Fatalf("仅有 tagparse Warning 时 check 应保持 exit=0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ 可提交") {
		t.Fatalf("仅有 tagparse Warning 时应仍报可提交:\n%s", out)
	}
}
