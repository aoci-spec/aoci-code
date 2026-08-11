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
			id: ContractHelpAILong,
			expected: "aoci ai 管理可选的 AI 增强层。\n\n" +
				"AI 增强层用于批量生成/更新/压缩索引,默认关闭 —— 不配置时 aoci 为纯离线确定性工具。\n" +
				"数据主权: 启用后源码仅发送到你显式配置的端点(公有云或内网自部署),不经过任何第三方。\n" +
				"密钥安全: 配置中只保存环境变量【名称】,真实密钥仅从环境变量读取,绝不落盘。",
			tokens: []string{
				"AI 增强层",
				"纯离线确定性工具",
				"数据主权",
				"绝不落盘",
			},
		},
		{
			id: ContractHelpAISetupLong,
			expected: "写入 AI 增强层配置。\n\n" +
				"安全: 本命令【不接受】传入密钥值,只接受 --key-env(存放密钥的环境变量名称)。\n" +
				"真实密钥请通过环境变量提供(如 export AOCI_AI_KEY=sk-xxx),配置中只记变量名。\n" +
				"--local 写入 config.local.json(仅本机、不进 Git);默认写入 config.json(团队共享)。",
			tokens: []string{
				"--key-env",
				"AOCI_AI_KEY",
				"--local",
				"config.local.json",
			},
		},
		{
			id: ContractHelpAITestLong,
			expected: "读取配置与环境变量密钥,向你配置的端点发起一次最小探测请求,报告连通性与用量。\n" +
				"数据主权: 仅向 base_url 指定端点发送,不触达任何其他地址。",
			tokens: []string{
				"最小探测请求",
				"数据主权",
				"base_url",
			},
		},
		{
			id: ContractHelpIndexBuildLong,
			expected: "对目标文件调用用户配置端点起草单行Entry。\n" +
				"--missing读取正式curation.json: 普通Actionable和有效include进入生成，exclude、Pending与技术跳过在AI Client构造前收敛。\n" +
				"显式--paths可选择普通文件，但特殊文件仍必须有有效include决策。",
			tokens: []string{
				"--missing",
				"curation.json",
				"Actionable",
				"Pending",
				"--paths",
			},
		},
		{
			id: ContractHelpHeaderDraftLong,
			expected: "读取当前索引头部与仓库画像，向你配置的端点发起一次起草请求。\n" +
				"产出落.aoci/drafts/<run_id>/header.txt，经diff人工确认后由apply写入。\n" +
				"数据主权: 仅向base_url指定端点发送；发送内容为现有头部与文件统计画像，不含源码正文。",
			tokens: []string{
				".aoci/drafts/<run_id>/header.txt",
				"diff",
				"apply",
				"不含源码正文",
			},
		},
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
			id: ContractHelpIndexAgentHeaderLong,
			expected: "宿主Agent通常先根据header_required计划调查项目并生成候选，再用header stage写入标准草稿。\n" +
				"已对齐仓库可以显式提交intent=semantic_refresh；同一Plan、Diff、P-23、CAS、Apply与Baseline闸门仍然不可绕过。\n" +
				"后续批准停点由团队automation.mode决定。",
			tokens: []string{
				"header_required",
				"header stage",
				"semantic_refresh",
				"automation.mode",
			},
		},
		{
			id: ContractHelpUpdateEntryLong,
			expected: "对指定文件写入完整单行条目: 已有条目整行替换,无则插入所属目录段。\n" +
				"条目使用共享的本地、Impact与S 字段Validator校验；--json下可修的Preview或Apply拒绝返回repair_required及与MCP相同的结构化Finding，只修retry_scope并保持其他候选不变。\n" +
				"正式写入必须用 --source-sha256 绑定 aoci_maintain 返回的源码指纹；内部复用原子批次、Baseline完整性与重放防线。\n" +
				"--entry与--stdin只能选择一种。标准输入沿用现有16 MiB Entries请求上限；超限会在任何正式写入或Recovery写入前失败。",
			tokens: []string{
				"完整单行条目",
				"整行替换",
				"S 字段",
				"拒绝",
				"repair_required",
				"retry_scope",
				"--source-sha256",
				"Baseline",
				"--stdin",
				"16 MiB",
			},
		},
		{
			id: ContractHelpEntriesCheckLong,
			expected: "对当前草稿内存快照运行与apply同源的格式、字典、配额和E档位判据，并执行不阻断的R关系轻量检查。\n" +
				"不修改正式索引或基线，但会把草稿SHA-256与校验摘要追加到\n" +
				"manifest.reviews。摘要计算失败时拒绝形成审阅记录。\n" +
				"全净或仅Warning时exit 0，存在apply必拒项时exit 2；Check通过后必须继续Diff，替换冲突仍由Apply时点兜底。",
			tokens: []string{
				"manifest.reviews",
				"SHA-256",
				"exit 0",
				"exit 2",
				"Diff",
				"Apply",
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
