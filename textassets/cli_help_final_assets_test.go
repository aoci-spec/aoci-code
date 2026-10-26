// R65-04B最终CLI Long Help资产的字节、换行与机器Token合同测试。
package textassets

import (
	"strings"
	"testing"
)

func TestCLIHelpFinalAssetsKeepExactContent(
	t *testing.T,
) {
	tests := []struct {
		id       ID
		expected string
		tokens   []string
	}{
		{
			id: ContractHelpIndexScoreLong,
			expected: "对当前索引做九维度质量评分:\n" +
				"format/coverage/freshness/squota/dict/token/agent_ready/escale/tagparse。\n" +
				"tagparse 用于暴露标签不可解析 Warning,不改变 check 放行策略。",
			tokens: []string{
				"format/coverage/freshness/squota/dict/token/agent_ready/escale/tagparse",
				"tagparse",
				"Warning",
			},
		},
		{
			id: ContractHelpIndexAgentLong,
			expected: "为Codex、Claude Code等宿主Agent提供确定性任务协议。\n" +
				"plan返回带哈希的事实计划；guide返回阶段化动作；stage接入Entries候选；curation治理特殊文件语义裁决。",
			tokens: []string{
				"Codex",
				"Claude Code",
				"plan",
				"guide",
				"stage",
				"curation",
			},
		},
		{
			id: ContractHelpUpdateEntryLong,
			expected: "对指定文件写入完整单行条目: 已有条目整行替换,无则插入所属目录段。\n" +
				"条目使用共享的本地、Impact与S 字段Validator校验；--json下可修的Preview或Apply拒绝返回repair_required及与MCP相同的结构化Finding，只修retry_scope并保持其他候选不变。\n" +
				"正式写入必须用 --source-sha256 绑定 aoci_maintain 返回的源码指纹；内部复用原子批次、Baseline完整性与重放防线。",
			tokens: []string{
				"完整单行条目",
				"整行替换",
				"S 字段",
				"拒绝",
				"repair_required",
				"retry_scope",
				"--source-sha256",
				"Baseline",
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
				"CLI Help资产必须保留文件终止换行: %s",
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
				"CLI Help资产字节发生变化: id=%s want=%q got=%q",
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
				"CLI Help渲染只允许移除一个文件尾换行: %s",
				current.id,
			)
		}

		for _, token := range current.tokens {
			if !strings.Contains(
				rendered,
				token,
			) {
				t.Fatalf(
					"CLI Help资产缺少机器Token %q: %s",
					token,
					current.id,
				)
			}
		}
	}
}

func TestCLIHelpFinalManifestIsValid(
	t *testing.T,
) {
	if err := Validate(); err != nil {
		t.Fatalf(
			"最终CLI Help Manifest校验失败: %v",
			err,
		)
	}
}
