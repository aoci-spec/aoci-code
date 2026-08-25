// 条目起草编译器测试: 纪律锚点、事实注入、必填校验、确定性与更新模式。
package prompt

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func sampleEntryInput() EntryInput {
	return EntryInput{
		RelPath:          "internal/store/store.go",
		SourceText:       "package store\n// 内存KV\nfunc New() {}\n",
		HeaderText:       "#【系统】demo\n#A层级: M-Model\n#B模块: Store-存储层\n",
		SuggestedSection: "存储层",
		NeighborEntries: []string{
			"other.go[MC5S]: F:邻居样例 | R:- | A:- | S:-",
		},
	}
}

func TestEntrySystemDisciplineAnchors(t *testing.T) {
	system, _, err := BuildEntryMessages(sampleEntryInput())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, anchor := range []string{
		"恰好一行",
		"四段齐备",
		"不含任何目录前缀",
		"通常控制在10个字以内",
		"语义完整优先",
		"双重自问",
		"禁演进叙事",
		machinecontract.NumericText().SQuotaDefaultWithUnits,
		"只列跨对象强依赖",
		"code:path/to/file",
		"database://source/namespace/table",
		"只列当前文件对外提供的API",
		"内部函数",
		"禁止发明任何字典外符号",
		"禁用数字",
		"SrvC9(错误)",
		"A+B+C+[D]+E",
		"EG7T",
		"G-跨域通用",
		"Z-其他",
		"不得写入 S",
		"B=Q",
		"模型必须阅读",
		"路径与文件名只用于确定",
		"禁止依据AST",
		"批量脚本",
		"不得接受工具生成的语义草稿",
		"不得复制全部import",
		"禁止声称",
	} {
		if !strings.Contains(system, anchor) {
			t.Fatalf("system 缺少纪律锚点: %q", anchor)
		}
	}

	for _, forbidden := range []string{
		"负空间",
		"F必须小于10个字",
		"F: 小于10个字",
	} {
		if strings.Contains(system, forbidden) {
			t.Fatalf("Entry Prompt含已废止的严格合同: %q", forbidden)
		}
	}
}

func TestEntryUserEmbedsFacts(t *testing.T) {
	_, user, err := BuildEntryMessages(sampleEntryInput())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, fact := range []string{
		"internal/store/store.go",
		"package store",
		"B模块: Store-存储层",
		"归段建议",
		"存储层",
		"邻居样例",
		"源码原文 开始",
		"索引头部 开始",
	} {
		if !strings.Contains(user, fact) {
			t.Fatalf("user 缺少事实: %q", fact)
		}
	}
}

func TestEntryNeighborContextDoesNotDecideCurrentS(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	tests := []struct {
		locale    string
		required  []string
		forbidden []string
	}{
		{
			locale: textassets.DefaultLocale,
			required: []string{
				"relationship and consistency context only",
				"A neighboring S:- does not determine the current object's S",
				"decide it independently from current evidence",
				"keep S:- when no qualifying constraint exists",
			},
			forbidden: []string{"style reference", "style references"},
		},
		{
			locale: textassets.LegacyLocale,
			required: []string{
				"仅供关系与一致性上下文",
				"邻居的S:-不决定当前对象的S",
				"依据当前对象证据独立判断",
				"无合格约束时仍保持S:-",
			},
			forbidden: []string{"风格参照"},
		},
	}
	for _, current := range tests {
		t.Run(current.locale, func(t *testing.T) {
			if err := textassets.SetActiveLocale(current.locale); err != nil {
				t.Fatal(err)
			}
			_, user, err := BuildEntryMessages(sampleEntryInput())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(user, "other.go[MC5S]") {
				t.Fatalf("%s neighbor context lost the actual Entry", current.locale)
			}
			for _, anchor := range current.required {
				if !strings.Contains(user, anchor) {
					t.Fatalf("%s neighbor context lacks %q: %q", current.locale, anchor, user)
				}
			}
			for _, forbidden := range current.forbidden {
				if strings.Contains(user, forbidden) {
					t.Fatalf("%s neighbor context still treats Entries as %q", current.locale, forbidden)
				}
			}
		})
	}
}

func TestEntryRequiredFields(t *testing.T) {
	base := sampleEntryInput()

	for name, mutate := range map[string]func(*EntryInput){
		"空RelPath": func(in *EntryInput) {
			in.RelPath = " "
		},
		"空SourceText": func(in *EntryInput) {
			in.SourceText = ""
		},
		"空HeaderText": func(in *EntryInput) {
			in.HeaderText = "\t"
		},
	} {
		in := base
		mutate(&in)
		if _, _, err := BuildEntryMessages(in); err == nil {
			t.Fatalf("%s应报错", name)
		}
	}
}

func TestEntryOptionalBranches(t *testing.T) {
	in := sampleEntryInput()
	in.SuggestedSection = ""
	in.NeighborEntries = nil

	_, user, err := BuildEntryMessages(in)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(user, "归段建议") ||
		strings.Contains(user, "样例") {
		t.Fatal("可选段为空时不应出现对应区块")
	}
}

func TestEntryDeterministic(t *testing.T) {
	in := sampleEntryInput()
	systemOne, userOne, errOne := BuildEntryMessages(in)
	systemTwo, userTwo, errTwo := BuildEntryMessages(in)

	if errOne != nil || errTwo != nil {
		t.Fatalf("编译失败: %v %v", errOne, errTwo)
	}
	if systemOne != systemTwo || userOne != userTwo {
		t.Fatal("同输入必得同输出")
	}
}

func TestEntryUpdateModeInjection(t *testing.T) {
	in := sampleEntryInput()
	in.OldEntry = "store.go[MStore5S]: F:旧职责 | R:- | A:New | S:非线程安全"

	system, user, err := BuildEntryMessages(in)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, anchor := range []string{
		"更新模式纪律",
		"仍然成立的 S 约束",
		"不盲目沿用旧标签",
	} {
		if !strings.Contains(system, anchor) {
			t.Fatalf("更新模式system缺少锚点: %q", anchor)
		}
	}

	for _, fact := range []string{
		"现有条目 开始",
		"MStore5S",
		"非线程安全",
	} {
		if !strings.Contains(user, fact) {
			t.Fatalf("更新模式user缺少事实: %q", fact)
		}
	}
}

func TestEntryUpdateUsesActiveLocaleWithoutTranslatingOldEntriesInBulk(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	tests := []struct {
		locale   string
		oldEntry string
		anchor   string
	}{
		{textassets.DefaultLocale, "store.go[MStore5S]: F:旧职责 | R:- | A:New | S:-", "configured project Locale en-US"},
		{textassets.LegacyLocale, "store.go[MStore5S]: F:Old responsibility | R:- | A:New | S:-", "配置参数指定的项目Locale zh-CN"},
	}
	for _, current := range tests {
		t.Run(current.locale, func(t *testing.T) {
			if err := textassets.SetActiveLocale(current.locale); err != nil {
				t.Fatal(err)
			}
			input := sampleEntryInput()
			input.OldEntry = current.oldEntry
			system, user, err := BuildEntryMessages(input)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(system, current.anchor) || !strings.Contains(user, current.oldEntry) {
				t.Fatalf("%s prompt lost active-Locale or existing-Entry evidence: system=%q user=%q", current.locale, system, user)
			}
		})
	}
}

func TestEntryBuildModeUnchanged(t *testing.T) {
	system, user, err := BuildEntryMessages(sampleEntryInput())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, forbidden := range []string{
		"更新模式",
		"现有条目",
	} {
		if strings.Contains(system, forbidden) ||
			strings.Contains(user, forbidden) {
			t.Fatalf("build模式不应含更新模式痕迹: %q", forbidden)
		}
	}
}
