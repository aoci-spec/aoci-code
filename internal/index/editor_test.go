// Table-driven editor tests: replace exactly one / insertion placement / newline preservation
// Index entry: editor_test.go[Test.Edit.9.Tbl.M]
package index

import (
	"strings"
	"testing"
)

const editBase = "====头====\n===段 /r/x/===\na.go[T.T.5.T]: F:甲 | R:- | A:- | S:-\nb.go[T.T.5.T]: F:乙 | R:- | A:- | S:-\n====完====\n"

// TestReplaceExactlyOne, the three branches 0 hits / 1 hit / 2+ hits
func TestReplaceExactlyOne(t *testing.T) {
	old := "a.go[T.T.5.T]: F:甲 | R:- | A:- | S:-"
	// 1 hit: succeeds and touches only the target line
	out, err := ReplaceEntry(editBase, old, "a.go[T.T.5.T]: F:甲改 | R:- | A:- | S:-")
	if err != nil || !strings.Contains(out, "F:甲改") || !strings.Contains(out, "F:乙") {
		t.Fatalf("1处替换异常: err=%v", err)
	}
	// 0 hits
	if _, err := ReplaceEntry(editBase, "ghost.go[T.T.5.T]: F:- | R:- | A:- | S:-", "x"); err == nil || !strings.Contains(err.Error(), "0 处") {
		t.Fatalf("0处应报明确错误,得到: %v", err)
	}
	// 2+ hits
	dup := editBase + old + "\n"
	if _, err := ReplaceEntry(dup, old, "x"); err == nil || !strings.Contains(err.Error(), "2 处") {
		t.Fatalf("2+处应拒绝,得到: %v", err)
	}
	// A new line holding a newline should be refused
	if _, err := ReplaceEntry(editBase, old, "x\ny"); err == nil {
		t.Fatal("多行新条目应被拒")
	}
}

func TestReplaceEntryForPathDisambiguatesIdenticalLinesAcrossSections(t *testing.T) {
	old := "same.txt[X.T.5.T]: F:相同 | R:- | A:- | S:-"
	text := "===英文资源 /repo/textassets/en-US/===\n" + old + "\n" +
		"===中文资源 /repo/textassets/zh-CN/===\n" + old + "\n"
	newLine := "same.txt[X.T.5.T]: F:英文合同 | R:- | A:- | S:-"
	out, err := ReplaceEntryForPath(
		text,
		"/repo",
		"textassets/en-US/same.txt",
		old,
		newLine,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, newLine) != 1 || strings.Count(out, old) != 1 {
		t.Fatalf("path-bound replacement changed the wrong number of lines:\n%s", out)
	}
	if _, err := ReplaceEntryForPath(text, "/repo", "textassets/missing.txt", old, newLine); err == nil {
		t.Fatal("missing path must fail closed")
	}
	if _, err := ReplaceEntryForPath(text, "/repo", "textassets/en-US/same.txt", "stale", newLine); err == nil {
		t.Fatal("stale old Entry must fail closed")
	}
}

// TestInsertPlacement: in-section insertion / an empty section inserts after the header / no section appends at the end
func TestInsertPlacement(t *testing.T) {
	// The section has entries: insert after the last entry
	out, err := InsertEntry(editBase, "x/c.go", "c.go[T.T.5.T]: F:丙 | R:- | A:- | S:-", "/r")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	idxB, idxC := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "b.go[") {
			idxB = i
		}
		if strings.HasPrefix(l, "c.go[") {
			idxC = i
		}
	}
	if idxC != idxB+1 {
		t.Fatalf("应插在段内最后条目之后: b@%d c@%d", idxB, idxC)
	}
	// An empty section: insert after the section header
	empty := "===空段 /r/e/===\n====完====\n"
	out2, _ := InsertEntry(empty, "e/n.go", "n.go[T.T.5.T]: F:- | R:- | A:- | S:-", "/r")
	l2 := strings.Split(out2, "\n")
	if !strings.HasPrefix(l2[1], "n.go[") {
		t.Fatalf("空段应插段头后,得到第二行: %q", l2[1])
	}
	// No section: append a new section header at the end
	out3, _ := InsertEntry(editBase, "newdir/z.go", "z.go[T.T.5.T]: F:- | R:- | A:- | S:-", "/r")
	if !strings.Contains(out3, "===/r/newdir/===") {
		t.Fatal("无匹配段应文末追加新段头")
	}
}

// TestLineEndingPreserved: a CRLF file keeps its style after write-back (the false-diff defence)
func TestLineEndingPreserved(t *testing.T) {
	crlf := strings.ReplaceAll(editBase, "\n", "\r\n")
	old := "a.go[T.T.5.T]: F:甲 | R:- | A:- | S:-"
	out, err := ReplaceEntry(crlf, old, "a.go[T.T.5.T]: F:改 | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatal("CRLF 文件出现裸 LF: 换行风格未保留")
	}
	if DetectLineEnding(out) != "\r\n" {
		t.Fatal("回写后应仍为 CRLF")
	}
	// A file without a trailing newline must not gain one from nowhere after write-back
	noTrail := strings.TrimSuffix(editBase, "\n")
	out2, _ := ReplaceEntry(noTrail, old, "a.go[T.T.5.T]: F:改 | R:- | A:- | S:-")
	if strings.HasSuffix(out2, "\n") {
		t.Fatal("末尾换行有无应保持原样")
	}
}
