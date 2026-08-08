// AI端点最小探测Prompt资产的物理格式与字节合同测试。
package textassets

import (
	"strings"
	"testing"
)

func TestAIProbePromptAssetsKeepExactContent(
	t *testing.T,
) {
	tests := []struct {
		id       ID
		expected string
		anchors  []string
	}{
		{
			id:       PromptAIProbeSystem,
			expected: "你是连通性测试助手,请只回复 ok。",
			anchors: []string{
				"连通性测试助手",
				"只回复",
				"ok",
			},
		},
		{
			id:       PromptAIProbeUser,
			expected: "ok",
			anchors: []string{
				"ok",
			},
		},
	}

	for _, current := range tests {
		raw := MustLoad(
			LegacyLocale,
			current.id,
		)

		if !strings.HasSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"AI探测Prompt必须保留文件终止换行: %s",
				current.id,
			)
		}

		rendered := MustRender(
			LegacyLocale,
			current.id,
			nil,
		)

		if rendered != current.expected {
			t.Fatalf(
				"AI探测Prompt字节发生变化: id=%s want=%q got=%q",
				current.id,
				current.expected,
				rendered,
			)
		}

		if rendered != strings.TrimSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"AI探测Prompt渲染只允许移除一个文件尾换行: %s",
				current.id,
			)
		}

		for _, anchor := range current.anchors {
			if !strings.Contains(
				rendered,
				anchor,
			) {
				t.Fatalf(
					"AI探测Prompt缺少机器Token %q: %s",
					anchor,
					current.id,
				)
			}
		}
	}
}
