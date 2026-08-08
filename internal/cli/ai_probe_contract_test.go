// Doctor与AI Test共用端点探测Prompt及完整请求的合同测试。
package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func TestAITestProbeMessagesUseTextAssets(
	t *testing.T,
) {
	messages, err := aiTestProbeMessages()
	if err != nil {
		t.Fatal(err)
	}

	if len(messages) != 2 {
		t.Fatalf(
			"AI探测消息数量错误: want=2 got=%d",
			len(messages),
		)
	}

	if messages[0].Role != "system" {
		t.Fatalf(
			"AI探测首条角色错误: %q",
			messages[0].Role,
		)
	}

	expectedSystem := textassets.MustRender(
		textassets.LegacyLocale,
		textassets.PromptAIProbeSystem,
		nil,
	)

	if messages[0].Content != expectedSystem {
		t.Fatalf(
			"AI探测system消息未使用文本资产: want=%q got=%q",
			expectedSystem,
			messages[0].Content,
		)
	}

	if messages[1].Role != "user" {
		t.Fatalf(
			"AI探测第二条角色错误: %q",
			messages[1].Role,
		)
	}

	expectedUser := textassets.MustRender(
		textassets.LegacyLocale,
		textassets.PromptAIProbeUser,
		nil,
	)

	if messages[1].Content != expectedUser {
		t.Fatalf(
			"AI探测user消息未使用文本资产: want=%q got=%q",
			expectedUser,
			messages[1].Content,
		)
	}
}

func TestAITestProbeRequestIsSharedAndFresh(
	t *testing.T,
) {
	first, err := aiTestProbeRequest()
	if err != nil {
		t.Fatal(err)
	}

	if first.MaxTokens != 16 {
		t.Fatalf(
			"AI探测MaxTokens错误: want=16 got=%d",
			first.MaxTokens,
		)
	}

	if len(first.Messages) != 2 {
		t.Fatalf(
			"AI探测请求消息数量错误: want=2 got=%d",
			len(first.Messages),
		)
	}

	first.Messages[0].Content = "mutated"

	second, err := aiTestProbeRequest()
	if err != nil {
		t.Fatal(err)
	}

	if second.Messages[0].Content == "mutated" {
		t.Fatal(
			"AI探测请求必须每次返回独立消息切片",
		)
	}

	expectedSystem := textassets.MustRender(
		textassets.LegacyLocale,
		textassets.PromptAIProbeSystem,
		nil,
	)

	if second.Messages[0].Content != expectedSystem {
		t.Fatalf(
			"AI探测请求system消息漂移: want=%q got=%q",
			expectedSystem,
			second.Messages[0].Content,
		)
	}
}
