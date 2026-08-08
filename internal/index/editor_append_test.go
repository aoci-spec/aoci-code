// InsertEntry 追加分支的文末注释区感知防回归测试
// 索引条目: editor_append_test.go(待补录)
//
// 背景(2026-07-09 P0-2): 原追加分支把新段甩到负空间 #说明区与 #代码索引完毕
// 闭合标记之后,破坏三分法结构。本文件锁定修复后的落点语义。
package index

import (
	"strings"
	"testing"
)

// TestInsertEntry_NewSectionBeforeTrailingComments 核心防回归:
// 新段必须落在文末 # 注释区(负空间说明+闭合标记)之前。
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
	// 闭合标记仍须是最后一个非空行
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
	// 新条目须紧随新段头
	if headerIdx+1 >= len(lines) || !strings.HasPrefix(lines[headerIdx+1], "x.go[") {
		t.Errorf("新条目未紧随新段头")
	}
}

// TestInsertEntry_NewSectionAtEndWhenNoTrailingComments 文末无注释区时行为不变:追加到末尾。
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
	// 末三行应为: 空行 / 新段头 / 新条目
	n := len(lines)
	if n < 3 || !strings.Contains(lines[n-2], "===/repo/newdir/===") || !strings.HasPrefix(lines[n-1], "x.go[") {
		t.Errorf("无注释区时应文末追加,输出:\n%s", out)
	}
}

// TestInsertEntry_AllCommentDocFallback 防御1: 零目录段文档(init 骨架)保持旧行为,
// 绝不把新段插到头部规范区之前。
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
	// 首行必须仍是头部注释(新段不得顶到规范区之上)
	if !strings.HasPrefix(lines[0], "#====骨架索引") {
		t.Errorf("零段文档的头部被顶开,首行: %q", lines[0])
	}
	// 新段头应在头部之后
	found := false
	for i, l := range lines {
		if strings.Contains(l, "===/repo/newdir/===") {
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

// TestInsertEntry_CRLFPreservedOnAppend 追加路径保持 CRLF 换行风格。
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
