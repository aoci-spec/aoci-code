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
			"模型生成、模型读取", "Header、Entry 和 Curation 语义", "当前机器签发的 Plan 与实时 Guide", "当前绑定证据", "真实证据",
			"路径、文件名、扩展名、AST", "固定模板或规则引擎",
			"Fresh Bootstrap", "只按当前机器签发的 Plan 和实时 Guide", "不自行重建 Onboarding 流程",
		},
		{
			"完整认知仍可靠时直接复用", "局部不确定本身不要求机械重读系统全貌",
			"Agent显式调用普通 `aoci_overview`", "完整交付请求scope", "`check_only=true`", "进入当前AOCI Guide",
			"如果需要建立正式完整AOCI索引", "`aoci_maintain` 不替代索引建立流程",
			"`current_system_cognition_reliable=true`", "纯只读问答、分析、版本核验",
			"最终稳定状态后，只调用一次 `aoci_maintain`", "当前布局支持 `aoci_report`", "`repair_required`", "`stopped`", "`source_sha256`",
			"冲突、审批、人工裁决、权限和安全信号不得忽略",
			"用户明确禁止修改 `aoci.txt`、`.aoci`、元数据或任何额外文件时",
		},
		{
			"普通的只读审计、分析、检查", "不自动等于严格零写入", "Codex Memory和历史Skill只能辅助",
			"不能替代与当前仓库根、索引摘要、AOCI服务身份和认知范围匹配的当前认知收据",
			"项目AGENTS和当前AOCI身份在AOCI状态上优先于历史Memory",
			"用户明确禁止Ledger、元数据、`.aoci`运行资产及任何文件写入",
			"必须报告冲突并请求用户裁决或建议使用隔离副本", "不得静默以Memory替代当前仓库认知",
		},
		{
			"当任务已有明确的代码更新计划时", "独立且完整的 Code 目标索引 `aoci.code.target.txt`",
			"`aoci cognition plan diff --target-index aoci.code.target.txt`", "确认后才修改业务代码",
			"`aoci.code.txt` 始终是当前正式索引", "不要为了流程凭空创建目标索引",
		},
	},
	textassets.DefaultLocale: {
		{
			"stable, versioned, incrementally updatable repository-level cognition layer", "`aoci.txt`", "Header", "Entry", "F/R/A/S",
			"model-generated, model-read cognition loop", "Header, Entry, and Curation semantics", "current machine-issued Plan and live Guide",
			"current bound evidence", "actual evidence", "paths, filenames, extensions, an AST",
			"fixed templates, or rule engines", "Fresh Bootstrap", "Do not reconstruct the Onboarding progression here",
		},
		{
			"Reuse complete cognition directly while it remains reliable", "Local uncertainty does not by itself require mechanically rereading the system-wide view",
			"explicitly calls ordinary `aoci_overview`", "deliver the complete requested scope", "`check_only=true`", "enter the current AOCI Guide",
			"when a formal complete AOCI index is required", "`aoci_maintain` does not replace the index-establishment workflow",
			"`current_system_cognition_reliable=true`", "purely read-only question, analysis, version check", "final stable state",
			"current layout supports `aoci_report`", "`repair_required`", "`stopped`", "`source_sha256`",
			"never ignore conflicts, approvals, human decisions, permissions, or safety signals",
			"explicitly forbids changes to `aoci.txt`, `.aoci`, metadata, or any additional file",
		},
		{
			"ordinary read-only audit, analysis, or check", "does not automatically mean strictly zero writes",
			"Codex Memory and historical Skills may only help",
			"cannot replace a current cognition receipt matching the repository root, index digest, AOCI service identity, and cognition scope",
			"Project AGENTS and current AOCI identity take precedence over historical Memory for AOCI state",
			"explicitly prohibits Ledger, metadata, `.aoci` runtime assets, and every filesystem write",
			"report the conflict and ask the user to decide or recommend an isolated copy",
			"Never silently substitute Memory for current repository cognition",
		},
		{
			"When a task has an explicit code-change plan", "separate complete Code target in `aoci.code.target.txt`",
			"`aoci cognition plan diff --target-index aoci.code.target.txt`", "only then edit business code",
			"Keep `aoci.code.txt` as the current formal Index", "Do not create a target file merely",
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
		"源码阅读、测试、LSP、CodeGraph", "source reading, tests, LSP, CodeGraph",
		"不得用索引摘要替代具体实现证据", "Do not substitute index summaries for concrete implementation evidence",
		"AOCI不规定架构、算法、调查顺序", "AOCI does not dictate architecture, algorithms, investigation order",
		"用户可见沟通应聚焦", "User-visible communication for an ordinary task should focus",
		"Fast门", "Fast gate", "Full Confidence", "Release Rehearsal",
		"项目AGENTS、当前AOCI身份、源码和运行事实优先",
		"Project AGENTS, current AOCI identity, source, and runtime facts take precedence",
		"不得描述为完整仓库认知", "Do not describe it as complete repository cognition",
		"不得把不完整内容当作完整仓库认知", "do not treat the incomplete content as complete repository cognition",
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
