// 逗号标签裁决差分用例(操作者裁决 2026-07-13: 非标降级)
// 索引条目待补: types_tags_test.go
//
// 独立成文缘由: ParseTags 既有测试散在 parser_test.go,本批次未现读该文件,
// 按协作纪律(未现读的文件不改)新建本文件承载裁决用例;用例聚焦裁决语义
// 本身,不与既有形态用例重复。
package index

import (
	"strings"
	"testing"
)

// TestParseTagsCommaNonStandard 逗号形态判非标: httpx-rerun 实弹形态与变体
// 均须返回空 map —— 任一切出非空 map 即第三态复活(dict 闸报语义垃圾违规
// 而 P-15 不触发)。
func TestParseTagsCommaNonStandard(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"httpx-rerun实弹形态", "I,CL,C8,EL"},
		{"逗号混点分", "I.CL,C8.EL"},
		{"全角逗号", "I，CL，C8，EL"},
		{"单个逗号混紧凑", "IC8,T"},
	}
	for _, c := range cases {
		if got := ParseTags(c.raw); len(got) != 0 {
			t.Fatalf("%s: 逗号形态应判非标返回空 map,实得 %v", c.name, got)
		}
	}
}

// TestParseTagsCommaTriggersP15 判决用例: 逗号标签条目经校验器必产出
// P-15"标签不可解析"警告(空 map 自动归入既有警告链,双闸跳过可见)。
func TestParseTagsCommaTriggersP15(t *testing.T) {
	line := "f.go[I,CL,C8,EL]: F:x | R:- | A:- | S:-"
	vs := ValidateEntryLine("f.go", line)
	if HasError(vs) {
		t.Fatalf("逗号标签应为警告级非硬拒(非标降级裁决): %+v", vs)
	}
	found := false
	for _, v := range vs {
		if v.Level == LevelWarning && strings.Contains(v.Msg, "标签不可解析") {
			found = true
		}
	}
	if !found {
		t.Fatalf("逗号标签应触发 P-15 不可解析警告: %+v", vs)
	}
}

// TestParseTagsLegitFormsUnaffected 回归防线: 合法两形态零受影响。
func TestParseTagsLegitFormsUnaffected(t *testing.T) {
	if got := ParseTags("WA9JM"); got["A"] != "W" || got["B"] != "A" || got["C"] != "9" || got["D"] != "J" || got["E"] != "M" {
		t.Fatalf("紧凑合法形态解析回归: %v", got)
	}
	if got := ParseTags("Index.Types.9.S"); got["A"] != "Index" || got["C"] != "9" || got["E"] != "S" {
		t.Fatalf("点分合法形态解析回归: %v", got)
	}
}
