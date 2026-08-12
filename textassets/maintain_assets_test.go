// Maintain文本资产的机器Token和物理格式保护测试。
package textassets

import (
	"strings"
	"testing"
)

// TestMaintainToolDescriptionKeepsGovernanceFields锁定Description中的治理字段。
//
// aoci_maintain是MCP Tool.Name结构字段，不属于可本地化Description正文，
// 因此不能要求Description重复工具名，也不能为满足测试改变既有用户输出。
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

func TestMaintainDictionaryMessagesKeepRecoveryCommands(
	t *testing.T,
) {
	unparseable := MustRender(
		LegacyLocale,
		ContractMaintainDictionaryUnparseable,
		nil,
	)

	for _, token := range []string{
		"头部字典不可解析",
		"#A层级",
		"aoci二进制版本",
		"agent不要",
	} {
		if !strings.Contains(
			unparseable,
			token,
		) {
			t.Fatalf(
				"不可解析字典说明缺少%q:\n%s",
				token,
				unparseable,
			)
		}
	}

	missing := MustRender(
		LegacyLocale,
		ContractMaintainDictionaryMissing,
		nil,
	)

	for _, token := range []string{
		"头部字典未建立",
		"A层级/B模块",
		"aoci index header draft",
		"diff",
		"apply",
		"agent不要",
	} {
		if !strings.Contains(
			missing,
			token,
		) {
			t.Fatalf(
				"缺失字典说明缺少%q:\n%s",
				token,
				missing,
			)
		}
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
		})
	}
}
