// Entry Prompt文件级策展收录模式测试。
package prompt

import (
	"strings"
	"testing"
)

func sampleEntryCurationInput() EntryInput {
	return EntryInput{
		RelPath:    "pkg/py.typed",
		HeaderText: "#A层级: X测试\n#B模块: RT根\n",
		Curation: &EntryCurationContext{
			Role:          "声明该Python包提供类型信息",
			Reason:        "文件内容虽为空，但其存在本身是包级类型协议标记",
			Confidence:    98,
			SourceSHA256:  strings.Repeat("a", 64),
			ProfileReason: "empty",
			Ext:           ".typed",
		},
	}
}

func TestEntryCurationModeAllowsMissingSource(
	t *testing.T,
) {
	systemText, userText, err :=
		BuildEntryMessages(
			sampleEntryCurationInput(),
		)
	if err != nil {
		t.Fatalf(
			"策展模式不应要求非空源码: %v",
			err,
		)
	}

	for _, anchor := range []string{
		"文件级策展收录模式纪律",
		"策展角色",
		"正文未注入",
		"声明该Python包提供类型信息",
		"存在本身是包级类型协议标记",
	} {
		if !strings.Contains(
			systemText+"\n"+userText,
			anchor,
		) {
			t.Fatalf(
				"策展Prompt缺少锚点%q:\n%s\n%s",
				anchor,
				systemText,
				userText,
			)
		}
	}

	if strings.Contains(
		systemText,
		"条目的唯一事实依据是下方注入的源码原文",
	) {
		t.Fatal(
			"策展模式不得继续注入普通源码唯一事实纪律",
		)
	}
}

func TestEntryCurationModeMayAlsoCarrySource(
	t *testing.T,
) {
	input := sampleEntryCurationInput()
	input.SourceText = "marker\n"

	_, userText, err :=
		BuildEntryMessages(input)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		userText,
		"源码原文 开始",
	) ||
		!strings.Contains(
			userText,
			"marker",
		) {
		t.Fatalf(
			"普通文本文件的有效include应同时注入源码和策展上下文:\n%s",
			userText,
		)
	}
}

func TestEntryCurationContextRequiredFields(
	t *testing.T,
) {
	input := sampleEntryCurationInput()
	input.Curation.Role = ""

	if _, _, err := BuildEntryMessages(
		input,
	); err == nil {
		t.Fatal(
			"策展角色为空必须拒绝",
		)
	}
}
