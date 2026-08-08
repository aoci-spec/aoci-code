// AGENTS模板中的仓库认知、模型语义、使用范围和收尾合同测试。
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

var agentsCognitionContractAnchors = map[string][][]string{
	textassets.LegacyLocale: {
		{
			"稳定、可版本化、可增量更新的仓库级认知层", "`aoci.txt`", "Header", "Entry", "F/R/A/S",
			"模型生成、模型读取", "模型按当前Guide调查仓库事实", "模型先阅读当前 Header", "真实证据",
			"路径、文件名、扩展名、AST", "固定模板或规则引擎", "不能代替模型完成语义理解和裁决",
			"源码阅读、测试、LSP、CodeGraph", "完整认知仍可靠时直接复用", "局部文件、模块、关系或约束不确定",
			"局部条目与搜索工具", "Agent显式调用普通 `aoci_overview`", "完整交付请求scope", "`check_only=true`", "进入当前AOCI Guide",
		},
		{
			"不得用索引摘要替代具体实现证据",
			"AOCI不规定架构、算法、调查顺序、实现方式、测试策略、分支策略或技术栈选择",
			"用户可见沟通应聚焦需求理解、源码调查、实现、测试、风险、提交和工作树状态",
			"内部认知建立、状态检查和认知维护默认不主动展开", "纯只读问答、分析、版本核验",
			"最终稳定状态后，再调用一次 `aoci_maintain`", "`repair_required`", "`stopped`", "`source_sha256`",
			"冲突、审批、人工裁决、权限和安全信号不得忽略",
			"用户明确禁止修改 `aoci.txt`、`.aoci`、元数据或任何额外文件时",
		},
		{
			"普通的只读审计、分析、检查", "不自动等于严格零写入", "Codex Memory和历史Skill只能辅助",
			"不能替代与当前仓库根、索引摘要、AOCI服务身份和认知范围匹配的当前认知收据",
			"项目AGENTS、当前AOCI身份、源码和运行事实优先",
			"用户明确禁止Ledger、元数据、`.aoci`运行资产及任何文件写入",
			"必须报告冲突并请求用户裁决或建议使用隔离副本", "不得静默以Memory替代当前仓库认知",
		},
	},
	textassets.DefaultLocale: {
		{
			"stable, versioned, incrementally updatable repository-level cognition layer", "`aoci.txt`", "Header", "Entry", "F/R/A/S",
			"model-generated, model-read cognition loop", "model follows the current Guide to investigate repository facts",
			"model first reads the current Header", "actual evidence", "paths, filenames, extensions, an AST",
			"fixed templates, or rule engines", "cannot replace model semantic understanding and judgment",
			"source reading, tests, LSP, CodeGraph", "Reuse complete cognition directly while it remains reliable",
			"local file, module, relationship, or constraint", "local Entry and search tools", "explicitly calls ordinary `aoci_overview`",
			"deliver the complete requested scope", "`check_only=true`",
			"enter the current AOCI Guide",
		},
		{
			"Do not substitute index summaries for concrete implementation evidence",
			"AOCI does not dictate architecture, algorithms, investigation order, implementation method, test strategy, branch strategy, or technology-stack choices",
			"User-visible communication for an ordinary task should focus on understanding the request, source investigation, implementation, tests, risks, commits, and worktree state",
			"Do not proactively narrate internal cognition establishment, state checks, or cognition maintenance by default",
			"purely read-only question, analysis, version check", "final stable state for this task", "`repair_required`", "`stopped`", "`source_sha256`",
			"never ignore conflicts, approvals, human decisions, permissions, or safety signals",
			"explicitly forbids changes to `aoci.txt`, `.aoci`, metadata, or any additional file",
		},
		{
			"ordinary read-only audit, analysis, or check", "does not automatically mean strictly zero writes",
			"Codex Memory and historical Skills may only help",
			"cannot replace a current cognition receipt matching the repository root, index digest, AOCI service identity, and cognition scope",
			"Project AGENTS, current AOCI identity, source, and runtime facts take precedence",
			"explicitly prohibits Ledger, metadata, `.aoci` runtime assets, and every filesystem write",
			"report the conflict and ask the user to decide or recommend an isolated copy",
			"Never silently substitute Memory for current repository cognition",
		},
	},
}

func assertAgentsCognitionContract(t *testing.T, locale, text string) {
	t.Helper()
	numeric := machinecontract.NumericText()

	groups, exists := agentsCognitionContractAnchors[locale]
	if !exists {
		t.Fatalf("missing explicit AGENTS contract assertions for %s", locale)
	}
	for _, group := range groups {
		for _, anchor := range group {
			if !strings.Contains(text, anchor) {
				t.Fatalf("%s AGENTS is missing stable contract anchor %q:\n%s", locale, anchor, text)
			}
		}
	}

	for _, forbidden := range []string{
		"mode=execute",
		"mode=prepare_and_review",
		"approval_required=true",
		"confidence=-1",
		"ProcessStartInfo",
		"PowerShell 5",
		numeric.EntriesMaxBodyHuman,
		numeric.HeaderMaxBodyHuman,
		numeric.CurationMaxBodyHuman,
		"完整Guide机械状态机", "每个新会话必须调用", "无条件调用", "固定调用次数",
		"complete Guide mechanical state machine", "every new session must call", "unconditionally call", "fixed number of calls",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("%s AGENTS contains removed specialized Guide contract %q:\n%s", locale, forbidden, text)
		}
	}
}

func TestOfficialLocaleAgentsTemplatesCarryRepositoryCognitionContract(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			asset := loadAgentsAssetForLocaleForTest(t, locale)
			assertAgentsManagedStructure(t, asset)
			assertAgentsCognitionContract(t, locale, asset)
		})
	}
}

func TestEnsureAgentsBlockMaterializesBothLocaleContracts(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			if err := textassets.SetActiveLocale(locale); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = textassets.SetActiveLocale(previous) }()

			root := t.TempDir()
			if _, err := EnsureAgentsBlock(root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			assertAgentsManagedStructure(t, string(data))
			assertAgentsCognitionContract(t, locale, string(data))
			want := strings.TrimRight(loadAgentsAssetForLocaleForTest(t, locale), "\n") + "\n"
			if string(data) != want {
				t.Fatalf("materialized %s AGENTS does not match its asset byte-for-byte", locale)
			}
		})
	}
}

func TestRepositoryAgentsUsesCurrentTemplateContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("读取仓库AGENTS失败: %v", err)
	}

	repositoryAgents := string(data)
	locale := configuredRepositoryLocaleForTest(t)
	assertAgentsManagedStructure(t, repositoryAgents)
	want := strings.TrimRight(loadAgentsAssetForLocaleForTest(t, locale), "\n")
	if got := managedAgentsBlockForTest(t, repositoryAgents); got != want {
		t.Fatalf("repository AGENTS managed block does not match configured %s asset:\n%s", locale, got)
	}
}
