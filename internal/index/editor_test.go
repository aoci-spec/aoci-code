// 编辑器表驱动测试: 替换恰好1处/插入定位/换行保留
// 索引条目: editor_test.go[Test.Edit.9.Tbl.M]
package index

import (
	"strings"
	"testing"
)

const editBase = "====头====\n===段 /r/x/===\na.go[T.T.5.T]: F:甲 | R:- | A:- | S:-\nb.go[T.T.5.T]: F:乙 | R:- | A:- | S:-\n====完====\n"

// TestReplaceExactlyOne 0处/1处/2+处三分支
func TestReplaceExactlyOne(t *testing.T) {
	old := "a.go[T.T.5.T]: F:甲 | R:- | A:- | S:-"
	// 1 处: 成功且只动目标行
	out, err := ReplaceEntry(editBase, old, "a.go[T.T.5.T]: F:甲改 | R:- | A:- | S:-")
	if err != nil || !strings.Contains(out, "F:甲改") || !strings.Contains(out, "F:乙") {
		t.Fatalf("1处替换异常: err=%v", err)
	}
	// 0 处
	if _, err := ReplaceEntry(editBase, "ghost.go[T.T.5.T]: F:- | R:- | A:- | S:-", "x"); err == nil || !strings.Contains(err.Error(), "0 处") {
		t.Fatalf("0处应报明确错误,得到: %v", err)
	}
	// 2+ 处
	dup := editBase + old + "\n"
	if _, err := ReplaceEntry(dup, old, "x"); err == nil || !strings.Contains(err.Error(), "2 处") {
		t.Fatalf("2+处应拒绝,得到: %v", err)
	}
	// 新行含换行应拒绝
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

// TestInsertPlacement 段内插入/空段插段头后/无段文末追加
func TestInsertPlacement(t *testing.T) {
	// 段内有条目: 插最后条目后
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
	// 空段: 插段头后
	empty := "===空段 /r/e/===\n====完====\n"
	out2, _ := InsertEntry(empty, "e/n.go", "n.go[T.T.5.T]: F:- | R:- | A:- | S:-", "/r")
	l2 := strings.Split(out2, "\n")
	if !strings.HasPrefix(l2[1], "n.go[") {
		t.Fatalf("空段应插段头后,得到第二行: %q", l2[1])
	}
	// 无段: 文末追加新段头
	out3, _ := InsertEntry(editBase, "newdir/z.go", "z.go[T.T.5.T]: F:- | R:- | A:- | S:-", "/r")
	if !strings.Contains(out3, "===/r/newdir/===") {
		t.Fatal("无匹配段应文末追加新段头")
	}
}

// TestLineEndingPreserved CRLF 文件回写后风格不变(假 diff 防线)
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
	// 末尾无换行的文件回写后不得凭空多出换行
	noTrail := strings.TrimSuffix(editBase, "\n")
	out2, _ := ReplaceEntry(noTrail, old, "a.go[T.T.5.T]: F:改 | R:- | A:- | S:-")
	if strings.HasSuffix(out2, "\n") {
		t.Fatal("末尾换行有无应保持原样")
	}
}
