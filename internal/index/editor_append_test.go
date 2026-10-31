// Regression tests for the trailing-comment-area awareness of the InsertEntry append branch
// Index entry: editor_append_test.go (to be recorded)
//
// Background (2026-07-09 P0-2): the original append branch threw the new section behind the negative-space # explanation area and the #代码索引完毕
// closing marker, breaking the three-way structure. This file locks the landing semantics after the fix.
package index

import (
	"strings"
	"testing"
)

// TestInsertEntry_NewSectionBeforeTrailingComments, the core regression guard:
// a new section must land before the trailing # comment area (the negative-space explanation plus the closing marker).
func TestInsertEntry_NewSectionBeforeTrailingComments(t *testing.T) {
	text := strings.Join([]string{
		"#====测试索引====",
		"#头部规范",
		"===配置索引/repo/===",
		"a.go[XAA9T]: F:入口 | R:- | A:- | S:测试",
		"",
		"#===负空间区===",
		"#delete_feature: 禁区说明",
		"#代码索引完毕",
	}, "\n") + "\n"

	out, err := InsertEntry(text, "newdir/x.go", "x.go[XBB7T]: F:新 | R:- | A:- | S:测试", "/repo")
	if err != nil {
		t.Fatalf("InsertEntry 失败: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

	headerIdx, negIdx, closeIdx := -1, -1, -1
	for i, l := range lines {
		switch {
		case strings.Contains(l, "===/repo/newdir/==="):
			headerIdx = i
		case strings.HasPrefix(l, "#===负空间区"):
			negIdx = i
		case strings.TrimSpace(l) == "#代码索引完毕":
			closeIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatalf("未找到新段头,输出:\n%s", out)
	}
	if negIdx == -1 || closeIdx == -1 {
		t.Fatalf("文末注释区丢失,输出:\n%s", out)
	}
	if headerIdx > negIdx {
		t.Errorf("新段头(行%d)落在负空间区(行%d)之后 —— 原 bug 复发", headerIdx, negIdx)
	}
	// The closing marker must still be the last non-empty line
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			last = strings.TrimSpace(lines[i])
			break
		}
	}
	if last != "#代码索引完毕" {
		t.Errorf("闭合标记不再是末尾,实为 %q", last)
	}
	// The new entry must follow the new section header directly
	if headerIdx+1 >= len(lines) || !strings.HasPrefix(lines[headerIdx+1], "x.go[") {
		t.Errorf("新条目未紧随新段头")
	}
}

// TestInsertEntry_NewSectionAtEndWhenNoTrailingComments: with no trailing comment area the behaviour is unchanged, appending at the end.
func TestInsertEntry_NewSectionAtEndWhenNoTrailingComments(t *testing.T) {
	text := strings.Join([]string{
		"===配置索引/repo/===",
		"a.go[XAA9T]: F:入口 | R:- | A:- | S:测试",
	}, "\n") + "\n"

	out, err := InsertEntry(text, "newdir/x.go", "x.go[XBB7T]: F:新 | R:- | A:- | S:测试", "/repo")
	if err != nil {
		t.Fatalf("InsertEntry 失败: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	// The last three lines should be: blank / new section header / new entry
	n := len(lines)
	if n < 3 || !strings.Contains(lines[n-2], "===/repo/newdir/===") || !strings.HasPrefix(lines[n-1], "x.go[") {
		t.Errorf("无注释区时应文末追加,输出:\n%s", out)
	}
}

// TestInsertEntry_AllCommentDocFallback, defence 1: a zero-directory-section document (an init skeleton) keeps the old behaviour and
// never inserts the new section before the header rules area.
func TestInsertEntry_AllCommentDocFallback(t *testing.T) {
	text := strings.Join([]string{
		"#====骨架索引====",
		"#头部规范甲",
		"#头部规范乙",
	}, "\n") + "\n"

	out, err := InsertEntry(text, "newdir/x.go", "x.go[XBB7T]: F:新 | R:- | A:- | S:测试", "/repo")
	if err != nil {
		t.Fatalf("InsertEntry 失败: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	// The first line must still be a header comment (the new section must not be pushed above the rules area)
	if !strings.HasPrefix(lines[0], "#====骨架索引") {
		t.Errorf("零段文档的头部被顶开,首行: %q", lines[0])
	}
	// The new section header should come after the header
	found := false
	for i, l := range lines {
		if strings.Contains(l, "==="+NeutralRepositoryRoot+"/newdir/===") {
			if i < 3 {
				t.Errorf("新段头(行%d)插到了头部规范区内", i)
			}
			found = true
		}
	}
	if !found {
		t.Error("未找到新段头")
	}
}

// TestInsertEntry_CRLFPreservedOnAppend: the append path keeps the CRLF newline style.
func TestInsertEntry_CRLFPreservedOnAppend(t *testing.T) {
	text := "===配置索引/repo/===\r\na.go[XAA9T]: F:入口 | R:- | A:- | S:测试\r\n#尾注\r\n"
	out, err := InsertEntry(text, "newdir/x.go", "x.go[XBB7T]: F:新 | R:- | A:- | S:测试", "/repo")
	if err != nil {
		t.Fatalf("InsertEntry 失败: %v", err)
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("CRLF 风格未保留")
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Error("输出混入了裸 LF(风格不一致)")
	}
}
