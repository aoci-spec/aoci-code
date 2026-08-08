package textassets

import (
	"strings"
	"testing"
)

func TestDatabaseAuthoringPromptKeepsModelOnlyFRASBoundaryInBothLocales(t *testing.T) {
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		prompt := MustRender(locale, PromptDatabaseEntryAuthoring, nil)
		for _, token := range []string{
			"F", "R", "A", "S", "Table Evidence", "batch_id", "candidate_id",
		} {
			if !strings.Contains(prompt, token) {
				t.Fatalf("%s Database authoring prompt lacks %q", locale, token)
			}
		}
		if strings.Contains(prompt, "generate a draft from the table name") ||
			strings.Contains(prompt, "根据表名生成草稿") {
			t.Fatalf("%s prompt permits program-authored semantics", locale)
		}
	}

	english := MustRender(DefaultLocale, PromptDatabaseEntryAuthoring, nil)
	chinese := MustRender(LegacyLocale, PromptDatabaseEntryAuthoring, nil)
	for _, required := range []string{"does not generate, prefill, rewrite, shorten, or repair semantics", "not every foreign key", "not inferable"} {
		if !strings.Contains(english, required) {
			t.Fatalf("English Database authoring contract lacks %q", required)
		}
	}
	for _, required := range []string{"不生成、预填、重写、缩短或修复语义", "不罗列所有外键", "无法由表名"} {
		if !strings.Contains(chinese, required) {
			t.Fatalf("Chinese Database authoring contract lacks %q", required)
		}
	}
}
