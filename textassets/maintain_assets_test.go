// Machine Token and physical format protection tests of the Maintain text assets.
package textassets

import (
	"strings"
	"testing"
)

// TestMaintainToolDescriptionKeepsGovernanceFields locks the governance fields in the Description.
//
// aoci_maintain is the MCP Tool.Name structural field, not part of the localizable Description body,
// so the Description cannot be required to repeat the tool name, nor may the existing user output change to satisfy the test.
func TestMaintainToolDescriptionKeepsGovernanceFields(
	t *testing.T,
) {
	description := MustRender(
		LegacyLocale,
		ContractMaintainToolDescription,
		nil,
	)

	for _, token := range []string{
		"最终稳定状态",
		"format-only",
		"repair_required",
		"stopped",
		"纯只读任务",
		"都不要再次调用",
		"intent=cognition_optimization",
		"仅Volumes v1 Code",
		"完整替换Entry或原样完整Entry",
		"禁止字段Patch",
		"程序绝不生成语义或S",
		"不压缩Entry文本",
		"不降低C",
	} {
		if !strings.Contains(
			description,
			token,
		) {
			t.Fatalf(
				"Maintain工具说明缺少%q:\n%s",
				token,
				description,
			)
		}
	}

	if strings.Contains(
		description,
		"\n",
	) {
		t.Fatalf(
			"MCP工具Description必须保持单行: %q",
			description,
		)
	}
}

func TestMaintainFinalApplyActionsRequireBilingualTerminalProof(t *testing.T) {
	tests := []struct {
		locale    string
		id        ID
		duplicate bool
	}{
		{DefaultLocale, ContractMaintainActionApplyFinalProof, false},
		{LegacyLocale, ContractMaintainActionApplyFinalProof, false},
		{DefaultLocale, ContractMaintainActionApplyDuplicateFinalProof, true},
		{LegacyLocale, ContractMaintainActionApplyDuplicateFinalProof, true},
	}
	for _, current := range tests {
		t.Run(current.locale+"/"+string(current.id), func(t *testing.T) {
			action := MustRender(current.locale, current.id, nil)
			for _, token := range []string{"remaining=0", "aoci_maintain", "Verify", "Aggregate Check", "Guide", "next_action=none"} {
				if !strings.Contains(action, token) {
					t.Fatalf("terminal proof action is missing %q: %q", token, action)
				}
			}
			formalWriteToken := "formal cognition write"
			if current.locale == LegacyLocale {
				formalWriteToken = "正式认知写入"
			}
			if !strings.Contains(action, formalWriteToken) {
				t.Fatalf("terminal proof action must distinguish formal cognition writes from audit writes: %q", action)
			}
			if current.duplicate != (strings.Contains(action, "zero formal writes") || strings.Contains(action, "正式写入为0")) {
				t.Fatalf("duplicate action has the wrong write fact: duplicate=%t action=%q", current.duplicate, action)
			}
			// A first submission whose Entries are unchanged reaches this action
			// too, so it says the Entries were in place, never that the batch
			// was repeated.
			if current.duplicate && !strings.Contains(action, "already in place") && !strings.Contains(action, "已在位") {
				t.Fatalf("duplicate action must say the Entries were already in place: %q", action)
			}
			if strings.Contains(action, "repeated") || strings.Contains(action, "重复的") {
				t.Fatalf("final apply action must not call the batch repeated: %q", action)
			}
		})
	}
}
