// 索引迁移后的新目录段写入必须复用既有历史根，不能注入运行时个人路径。
package index

import (
	"strings"
	"testing"
)

func TestInsertEntryReusesHistoricalRootAfterRepositoryRelocation(t *testing.T) {
	text := "===/opt/aoci-code/===\n" +
		"go.mod[XMK5T]: F:定义模块 | R:- | A:- | S:-\n" +
		"===/opt/aoci-code/internal/index/===\n" +
		"parser.go[IPS9RM]: F:解析索引 | R:- | A:Parse | S:-\n"

	out, err := InsertEntry(
		text,
		"internal/service/runner.go",
		"runner.go[WWF9L]: F:恢复治理 | R:- | A:Resume | S:-",
		"/home/developer/relocated-clone",
	)
	if err != nil {
		t.Fatalf("迁移仓新增目录条目失败: %v", err)
	}
	if strings.Contains(out, "/home/developer/relocated-clone") {
		t.Fatalf("不得把运行时个人路径写入正式索引:\n%s", out)
	}
	if !strings.Contains(out, "===/opt/aoci-code/internal/service/===") {
		t.Fatalf("新目录段必须复用既有历史根:\n%s", out)
	}

	// 再迁移到完全不同的新克隆路径，全部旧条目和新增条目仍须可解析。
	doc, warnings := Parse(out)
	if len(warnings) != 0 {
		t.Fatalf("输出索引不应产生解析告警: %+v", warnings)
	}
	ResolveRelPaths(doc, "/tmp/another-fresh-clone")
	for _, rel := range []string{
		"go.mod",
		"internal/index/parser.go",
		"internal/service/runner.go",
	} {
		if FindEntry(doc, rel) == nil {
			t.Fatalf("新路径克隆后条目 %s 未解析", rel)
		}
	}
}

func TestInsertEntryRejectsInconsistentSectionRoots(t *testing.T) {
	text := "===/opt/aoci-code/===\n" +
		"go.mod[XMK5T]: F:定义模块 | R:- | A:- | S:-\n" +
		"===/home/developer/aoci-code/internal/fs/===\n" +
		"lock.go[WVU9M]: F:保护写入 | R:- | A:- | S:-\n"

	out, err := InsertEntry(
		text,
		"internal/service/runner.go",
		"runner.go[WWF9L]: F:恢复治理 | R:- | A:Resume | S:-",
		"/home/developer/aoci-code",
	)
	if err == nil {
		t.Fatalf("历史根不一致时必须fail-closed,实际输出:\n%s", out)
	}
	if !strings.Contains(err.Error(), "目录段历史根不一致") {
		t.Fatalf("错误必须明确指出历史根冲突: %v", err)
	}
}

func TestInsertEntryReusesWindowsHistoricalRootAfterRelocation(t *testing.T) {
	text := "===C:/work/aoci/===\n" +
		"go.mod[XMK5T]: F:定义模块 | R:- | A:- | S:-\n" +
		"===C:/work/aoci/internal/index/===\n" +
		"parser.go[IPS9RM]: F:解析索引 | R:- | A:Parse | S:-\n"

	out, err := InsertEntry(
		text,
		"internal/service/runner.go",
		"runner.go[WWF9L]: F:恢复治理 | R:- | A:Resume | S:-",
		"D:/fresh/aoci",
	)
	if err != nil {
		t.Fatalf("Windows迁移仓新增目录条目失败: %v", err)
	}
	if !strings.Contains(out, "===C:/work/aoci/internal/service/===") ||
		strings.Contains(out, "D:/fresh/aoci") {
		t.Fatalf("Windows新目录段未复用历史根:\n%s", out)
	}
	doc, _ := Parse(out)
	ResolveRelPaths(doc, "E:/another/aoci")
	if FindEntry(doc, "internal/service/runner.go") == nil {
		t.Fatal("Windows索引再次迁移后新增条目未解析")
	}
}
