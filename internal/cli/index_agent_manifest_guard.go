// Host-Agent Manifest Generation State共享完整性与恢复诊断。
//
// 该层只作用于generation_source=host_agent的现代草稿。
// Endpoint及历史草稿继续沿用原有兼容行为。
package cli

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

// validateHostAgentManifestState校验Host-Agent草稿在Apply前必须可信的字段。
//
// SHA字段必须是无首尾空格的小写64位十六进制。Stage已经按该格式写入；
// Apply阶段不再静默归一化Manifest，任何差异都视为草稿审计状态损坏。
func validateHostAgentManifestState(
	manifest *draft.Manifest,
	expectedKind string,
	requireCurationSHA bool,
) error {
	if manifest == nil {
		return fmt.Errorf("%s", cliMessage("agent.manifest.empty"))
	}

	if manifest.GenerationSource !=
		draft.GenerationSourceHostAgent {
		return fmt.Errorf("%s", cliMessage(
			"agent.manifest.generation_source",
			manifest.GenerationSource,
			draft.GenerationSourceHostAgent,
		))
	}

	if manifest.Kind != expectedKind {
		return fmt.Errorf("%s", cliMessage("agent.manifest.kind", manifest.Kind, expectedKind))
	}
	if manifest.HeaderIntent != "" &&
		(expectedKind != draft.KindHeader ||
			manifest.HeaderIntent != agentHeaderStageIntentSemanticRefresh) {
		return fmt.Errorf("%s", cliMessage(
			"header.stage.intent_invalid",
			manifest.HeaderIntent,
		))
	}

	if manifest.Provider != agentStageProvider {
		return fmt.Errorf("%s", cliMessage(
			"agent.manifest.provider",
			manifest.Provider,
			agentStageProvider,
		))
	}

	agent := strings.TrimSpace(
		manifest.AgentName,
	)
	if agent == "" {
		return fmt.Errorf("%s", cliMessage("agent.manifest.agent_required"))
	}
	if agent != manifest.AgentName ||
		agent != normalizeAgentAuditLabel(agent) ||
		!agentStageNameRe.MatchString(agent) {
		return fmt.Errorf("%s", cliMessage(
			"agent.manifest.agent_invalid",
			manifest.AgentName,
		))
	}

	requiredHashes := []struct {
		field string
		value string
	}{
		{
			field: "plan_id",
			value: manifest.PlanID,
		},
		{
			field: "index_sha256",
			value: manifest.IndexSHA256,
		},
		{
			field: "header_sha256",
			value: manifest.HeaderSHA256,
		},
		{
			field: "generation_hash",
			value: manifest.GenerationHash,
		},
	}

	if requireCurationSHA {
		requiredHashes = append(
			requiredHashes,
			struct {
				field string
				value string
			}{
				field: "curation_sha256",
				value: manifest.CurationSHA256,
			},
		)
	}

	for _, current := range requiredHashes {
		if err := validateManifestSHA256(
			current.field,
			current.value,
		); err != nil {
			return err
		}
	}

	return nil
}

// validateManifestSHA256拒绝缺失、错误长度、非十六进制、首尾空格和大写形式。
func validateManifestSHA256(
	field,
	value string,
) error {
	normalized, err := normalizeRequiredSHA256(
		field,
		value,
	)
	if err != nil {
		return err
	}

	if value != normalized {
		return fmt.Errorf("%s", cliMessage("agent.manifest.sha_canonical", field))
	}

	return nil
}

// hostAgentManifestDamageError生成不可与Plan过期混淆的恢复诊断。
func hostAgentManifestDamageError(
	scope string,
	cause error,
) error {
	return fmt.Errorf("%s", cliMessage(
		"agent.manifest.damaged",
		scope,
		localeSafeCLIDetail(cause.Error()),
	))
}

// hostAgentPlanExpiredError统一输出Plan过期事实和恢复动作。
func hostAgentPlanExpiredError(
	scope,
	oldPlanID,
	expectedStage,
	currentPlanID,
	currentStage string,
) error {
	return fmt.Errorf("%s", cliMessage(
		"agent.plan.expired",
		scope,
		shortAgentStageHash(oldPlanID),
		expectedStage,
		shortAgentStageHash(currentPlanID),
		currentStage,
	))
}
