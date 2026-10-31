// Differential cases for the comma-tag decision (operator decision 2026-07-13: non-standard downgrade)
// Index entry pending: types_tags_test.go
//
// Why a separate file: the existing ParseTags tests are scattered in parser_test.go, which this batch had not read;
// under the collaboration discipline (never change a file not read) this new file carries the decision cases; they focus on the decision
// semantics themselves and do not repeat the existing form cases.
package index

import (
	"strings"
	"testing"
)

// TestParseTagsCommaNonStandard: the comma form is judged non-standard; the httpx-rerun live shape and its variants
// must all return an empty map; any one cut into a non-empty map revives the third state (the dict gate reports a semantically garbage violation
// while P-15 does not fire).
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

// TestParseTagsCommaTriggersP15, the verdict case: a comma-tag entry through the validator must produce the
// P-15 "unparseable tag" warning (the empty map joins the existing warning chain automatically, making the skip of both gates visible).
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

// TestParseTagsLegitFormsUnaffected, the regression guard: the two legal forms are unaffected.
func TestParseTagsLegitFormsUnaffected(t *testing.T) {
	if got := ParseTags("WA9JM"); got["A"] != "W" || got["B"] != "A" || got["C"] != "9" || got["D"] != "J" || got["E"] != "M" {
		t.Fatalf("紧凑合法形态解析回归: %v", got)
	}
	if got := ParseTags("Index.Types.9.S"); got["A"] != "Index" || got["C"] != "9" || got["E"] != "S" {
		t.Fatalf("点分合法形态解析回归: %v", got)
	}
}
