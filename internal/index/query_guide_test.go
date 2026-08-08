// aoci_rules会话认知、普通开发边界与增量收尾合同测试。
package index

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestRuntimeRulesExposeSessionCognitionContract(t *testing.T) {
	numeric := machinecontract.NumericText()
	rules, err := BuildRuntimeRules(nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range []string{
		"AOCI 运行合同（会话级）",
		"AOCI 与源码阅读、LSP、CodeGraph及其他结构化工具互补",
		"runtime_repository_root",
		"index_sha256",
		"mcp_service_version",
		"cognition_scope",
		"Codex Memory和历史Skill只能辅助",
		"不能替代匹配当前AOCI身份的认知收据",
		"项目AGENTS、当前AOCI身份、源码和运行事实优先",
		"valid",
		"uncertain",
		"invalid",
		"正常 Agent Run 开始时读取一次 aoci_rules，并请求一次 aoci_overview",
		"checkpoint评估识别三种事实",
		"context_compaction",
		"semantic_threshold",
		"phase_transition",
		"cognition_refresh_threshold: 30",
		"refresh_not_required",
		"refresh_required",
		"refresh_deferred_until_stable",
		"refresh_ready_for_overview",
		"只有 check_only=true",
		"每次显式调用普通 aoci_overview",
		"多个同时成立的原因合并",
		"先完成最近的稳定工作单元",
		"aoci_get_entries 或 aoci_search",
		"进入当前AOCI Guide",
		"模型生成、模型读取",
		"生成或修订Header时",
		"生成或更新Entry时",
		"仓库事实",
		"必要关联证据",
		"不能直接生成、预填、拼接或改写标签、F/R/A/S",
		"不得替模型决定架构、算法、调查顺序、实现方式、测试策略",
		"用户可见消息聚焦需求理解、源码调查、实现、测试、风险、提交和工作树状态",
		"用户只限制业务文件范围",
		"最终稳定状态后，只调用一次 aoci_maintain",
		"aligned=true、status=applied",
		"format-only",
		"真实 candidates",
		"source_sha256",
		"entries 批量入口",
		"repair_required 且包含 findings",
		"stopped",
		"failed_step、error 和 recovery",
		"不得绕过冲突、审批、权限、人工裁决、CAS和安全信号",
		"aoci_report 不替代Curation、孤儿裁决",
		"错误认知比暂时缺失更有害",
	} {
		if !strings.Contains(rules, anchor) {
			t.Fatalf("运行时合同缺少稳定语义锚点%q:\n%s", anchor, rules)
		}
	}

	for _, forbidden := range []string{
		"mode=execute",
		"mode=prepare_and_review",
		"commands.verify",
		"commands.entries_stage",
		"approval_required",
		"stop_before_apply",
		"confidence=-1",
		"ConvertFrom-Json",
		"ProcessStartInfo",
		"PowerShell",
		numeric.EntriesMaxBodyHuman,
		numeric.HeaderMaxBodyHuman,
		numeric.CurationMaxBodyHuman,
	} {
		if strings.Contains(rules, forbidden) {
			t.Fatalf("运行时合同含已移出的专项Guide合同%q:\n%s", forbidden, rules)
		}
	}
}

func TestRuntimeRulesUseProjectRefreshThreshold(t *testing.T) {
	rules, err := BuildRuntimeRules(nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, "cognition_refresh_threshold: 5") ||
		strings.Contains(rules, "cognition_refresh_threshold: 30") {
		t.Fatalf("runtime rules must expose the selected machine threshold:\n%s", rules)
	}
}

func TestRuntimeRulesRejectInvalidRefreshThreshold(t *testing.T) {
	for _, value := range []int{0, machinecontract.CognitionRefreshThresholdMax + 1} {
		if _, err := BuildRuntimeRules(nil, value); err == nil {
			t.Fatalf("invalid threshold %d was accepted", value)
		}
	}
}
