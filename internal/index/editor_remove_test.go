// RemoveEntry 原语表驱动测试(v2.8 P1)。
// 索引条目: editor_remove_test.go(待补录,随本批入册)
//
// 覆盖面: 恰好1处删除且其余行字节保真/0处报错(含重取提示)/2+处报错
// (重复污染)/CRLF风格与末尾换行双保持/无末尾换行保持/空目标与含换行目标拒。
// 纯文本级测试不落盘不造仓(同 editor_append_test.go 纪律)。
package index

import (
	"strings"
	"testing"
)

// rmFixture 三条目LF夹具(a/b/c各一行,b为删除靶)
const rmFixture = "#头部\n===/repo/===\na.go[XC9T]: F:甲 | R:- | A:- | S:-\nb.go[XC9T]: F:乙 | R:- | A:- | S:-\nc.go[XC9T]: F:丙 | R:- | A:- | S:-\n"

const rmTarget = "b.go[XC9T]: F:乙 | R:- | A:- | S:-"

// TestRemoveEntryExactlyOne 恰好1处删除成功,其余行字节保真且末尾换行保持。
func TestRemoveEntryExactlyOne(t *testing.T) {
	got, err := RemoveEntry(rmFixture, rmTarget)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	want := "#头部\n===/repo/===\na.go[XC9T]: F:甲 | R:- | A:- | S:-\nc.go[XC9T]: F:丙 | R:- | A:- | S:-\n"
	if got != want {
		t.Errorf("删除结果不符\n得到:\n%q\n期望:\n%q", got, want)
	}
}

// TestRemoveEntryTrimSpaceMatch 目标带首尾空白仍按TrimSpace匹配删除。
func TestRemoveEntryTrimSpaceMatch(t *testing.T) {
	got, err := RemoveEntry(rmFixture, "  "+rmTarget+"  ")
	if err != nil {
		t.Fatalf("带空白目标应匹配成功: %v", err)
	}
	if strings.Contains(got, "b.go") {
		t.Error("b.go 条目应已被删除")
	}
}

// TestRemoveEntryZeroHit 0处命中报错且含重取提示。
func TestRemoveEntryZeroHit(t *testing.T) {
	_, err := RemoveEntry(rmFixture, "x.go[XC9T]: F:不存在 | R:- | A:- | S:-")
	if err == nil {
		t.Fatal("0处命中应报错")
	}
	if !strings.Contains(err.Error(), "已被人工修改") {
		t.Errorf("错误信息应含重取提示,实得: %v", err)
	}
}

// TestRemoveEntryMultiHit 2+处命中报重复污染拒绝。
func TestRemoveEntryMultiHit(t *testing.T) {
	dup := rmFixture + rmTarget + "\n"
	_, err := RemoveEntry(dup, rmTarget)
	if err == nil {
		t.Fatal("2处命中应报错")
	}
	if !strings.Contains(err.Error(), "2 处") || !strings.Contains(err.Error(), "重复污染") {
		t.Errorf("错误信息应含处数与重复污染字样,实得: %v", err)
	}
}

// TestRemoveEntryCRLFPreserved CRLF文本删除后风格与末尾换行双保持,无裸LF混入。
func TestRemoveEntryCRLFPreserved(t *testing.T) {
	crlf := strings.ReplaceAll(rmFixture, "\n", "\r\n")
	got, err := RemoveEntry(crlf, rmTarget)
	if err != nil {
		t.Fatalf("CRLF删除失败: %v", err)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Error("输出混入裸LF(CRLF风格未保持)")
	}
	if !strings.HasSuffix(got, "\r\n") {
		t.Error("末尾CRLF换行未保持")
	}
	if strings.Contains(got, "b.go") {
		t.Error("b.go 条目应已被删除")
	}
}

// TestRemoveEntryNoTrailingNL 无末尾换行文本删除后仍无末尾换行。
func TestRemoveEntryNoTrailingNL(t *testing.T) {
	noTrail := strings.TrimRight(rmFixture, "\n")
	got, err := RemoveEntry(noTrail, rmTarget)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("原文无末尾换行,输出不应有")
	}
}

// TestRemoveEntryBadTarget 空目标与含换行目标均拒。
func TestRemoveEntryBadTarget(t *testing.T) {
	if _, err := RemoveEntry(rmFixture, "   "); err == nil {
		t.Error("空目标应拒")
	}
	if _, err := RemoveEntry(rmFixture, "a\nb"); err == nil {
		t.Error("含换行目标应拒")
	}
}

func TestRemoveEntryForPathDisambiguatesIdenticalLines(t *testing.T) {
	text := "===/repo/a/===\nsame.go[T.T.5.T]: F:same | R:- | A:- | S:-\n===/repo/b/===\nsame.go[T.T.5.T]: F:same | R:- | A:- | S:-\n"
	line := "same.go[T.T.5.T]: F:same | R:- | A:- | S:-"
	got, err := RemoveEntryForPath(text, "/repo", "a/same.go", line)
	if err != nil {
		t.Fatal(err)
	}
	document, _ := Parse(got)
	ResolveRelPaths(document, "/repo")
	if FindEntry(document, "a/same.go") != nil || FindEntry(document, "b/same.go") == nil {
		t.Fatalf("path-aware removal removed the wrong Entry: %s", got)
	}
}

func TestRemoveEntryPrunesOnlyPureEmptyDirectorySections(t *testing.T) {
	text := "#header\n" +
		"===Empty/repo/empty/===\n#comment-only\n\n" +
		"===Keep compatibility marker===\n#layout\n" +
		"===Independent/repo/independent/===\nformal boundary text\n" +
		"===Target/repo/target/===\nlast.go[IM7S]: F:last | R:- | A:- | S:-\n"
	got, err := RemoveEntry(text, "last.go[IM7S]: F:last | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "/repo/empty/") || strings.Contains(got, "/repo/target/") {
		t.Fatalf("pure empty section survived:\n%s", got)
	}
	for _, want := range []string{"#header", "===Keep compatibility marker===", "===Independent/repo/independent/===", "formal boundary text"} {
		if !strings.Contains(got, want) {
			t.Fatalf("required boundary %q was removed:\n%s", want, got)
		}
	}
}
