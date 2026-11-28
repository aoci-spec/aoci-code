package machinecontract

import "testing"

func TestLexicalContractsAreUniqueAndReturnedAsCopies(t *testing.T) {
	publicTerms := PublicTextTerms()
	if len(publicTerms) == 0 {
		t.Fatal("公开文案机器词表不得为空")
	}
	seenPublic := map[string]bool{}
	for _, term := range publicTerms {
		if term.Text == "" || seenPublic[term.Text] {
			t.Fatalf("公开文案机器词表含空项或重复项: %+v", term)
		}
		seenPublic[term.Text] = true
		if term.Kind != TextTermForbidden && term.Kind != TextTermOverclaim {
			t.Fatalf("公开文案机器词表分类非法: %+v", term)
		}
		if term.Mode != TextMatchSubstringFold && term.Mode != TextMatchWordExact {
			t.Fatalf("公开文案机器词表匹配模式非法: %+v", term)
		}
	}
	publicTerms[0].Text = "mutated"
	if PublicTextTerms()[0].Text == "mutated" {
		t.Fatal("调用方不得修改公开文案机器词表包状态")
	}

	evolutionTerms := EvolutionNarrativeTerms()
	if len(evolutionTerms) == 0 {
		t.Fatal("演进叙事机器词表不得为空")
	}
	seenEvolution := map[string]bool{}
	for _, term := range evolutionTerms {
		if term == "" || seenEvolution[term] {
			t.Fatalf("演进叙事机器词表含空项或重复项: %q", term)
		}
		if term == "改为" {
			t.Fatalf("裸动词 %q 不应留在演进叙事词表中（会误报运行时状态转换）", term)
		}
		seenEvolution[term] = true
	}
	for _, required := range []string{"本次修改", "现在改", "原来是", "此前是", "之前是"} {
		if !seenEvolution[required] {
			t.Fatalf("演进叙事机器词表缺少带时间锚点的词项: %q", required)
		}
	}
	evolutionTerms[0] = "mutated"
	if EvolutionNarrativeTerms()[0] == "mutated" {
		t.Fatal("调用方不得修改演进叙事机器词表包状态")
	}
}
