// Table-driven tests for the RemoveEntry primitive (v2.8 P1).
// Index entry: editor_remove_test.go (to be recorded, entering the index with this batch)
//
// Coverage: exactly 1 hit removed with the other lines byte-identical / 0 hits errors (with the re-fetch hint) / 2+ hits errors
// (duplicate pollution) / CRLF style and trailing newline both kept / no trailing newline kept / empty target and a target holding a newline refused.
// Pure text-level tests, no disk write and no repository (the same discipline as editor_append_test.go).
package index

import (
	"strings"
	"testing"
)

// rmFixture is a three-entry LF fixture (one line each for a/b/c, b being the removal target)
const rmFixture = "#头部\n===/repo/===\na.go[XC9T]: F:甲 | R:- | A:- | S:-\nb.go[XC9T]: F:乙 | R:- | A:- | S:-\nc.go[XC9T]: F:丙 | R:- | A:- | S:-\n"

const rmTarget = "b.go[XC9T]: F:乙 | R:- | A:- | S:-"

// TestRemoveEntryExactlyOne: exactly 1 hit removes successfully, the other lines stay byte-identical, and the trailing newline is kept.
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

// TestRemoveEntryTrimSpaceMatch: a target with surrounding whitespace still matches and removes by TrimSpace.
func TestRemoveEntryTrimSpaceMatch(t *testing.T) {
	got, err := RemoveEntry(rmFixture, "  "+rmTarget+"  ")
	if err != nil {
		t.Fatalf("带空白目标应匹配成功: %v", err)
	}
	if strings.Contains(got, "b.go") {
		t.Error("b.go 条目应已被删除")
	}
}

// TestRemoveEntryZeroHit: 0 hits errors with the re-fetch hint.
func TestRemoveEntryZeroHit(t *testing.T) {
	_, err := RemoveEntry(rmFixture, "x.go[XC9T]: F:不存在 | R:- | A:- | S:-")
	if err == nil {
		t.Fatal("0处命中应报错")
	}
	if !strings.Contains(err.Error(), "已被人工修改") {
		t.Errorf("错误信息应含重取提示,实得: %v", err)
	}
}

// TestRemoveEntryMultiHit: 2+ hits reports duplicate pollution and refuses.
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

// TestRemoveEntryCRLFPreserved: a CRLF text keeps both its style and its trailing newline after removal, with no bare LF mixed in.
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

// TestRemoveEntryNoTrailingNL: a text without a trailing newline still has none after removal.
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

// TestRemoveEntryBadTarget: an empty target and a target holding a newline are both refused.
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
