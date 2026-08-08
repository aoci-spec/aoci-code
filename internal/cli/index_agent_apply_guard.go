// 本文件承载Host-Agent草稿在正式Apply前的Generation Plan一致性防线。
//
// 三层一致性:
//   - R52: 审阅与应用是否选择同一个Run;
//   - P-23: 应用内容是否仍是最近一次审阅过的草稿版本;
//   - R60-D: 草稿生成时依据的仓库认知状态是否仍是当前状态。
//
// 本防线只作用于generation_source=host_agent的现代草稿。
// Endpoint-native和历史草稿继续沿用原有兼容行为。
//
// 故障分类:
//   - Manifest Generation State损坏: 字段、SHA、provider、Agent或目标清单异常，
//     该Run必须重新Stage;
//   - Generation Plan过期: 当前仓库事实已经变化，必须重新Guide并生成候选。
//
// 本文件纯读，不写草稿、正式索引、Baseline或Ledger。
package cli

import (
	"errors"
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/spf13/cobra"
)

// guardHostAgentGenerationPlan重新构造当前Plan并核对Host-Agent Generation State。
//
// 成功返回一条可展示的人读确认信息；非Host-Agent草稿返回空信息零错误。
func guardHostAgentGenerationPlan(
	cmd *cobra.Command,
	repoRoot string,
	cfg *config.Config,
	manifest *draft.Manifest,
	expectedKind,
	expectedStage string,
) (string, error) {
	if manifest == nil {
		return "", &ExitError{
			Code: ExitConfig,
			Err: hostAgentManifestDamageError(
				cliMessage("agent.scope.host"),
				fmt.Errorf("%s", cliMessage("agent.manifest.empty")),
			),
		}
	}

	if manifest.GenerationSource !=
		draft.GenerationSourceHostAgent {
		return "", nil
	}

	if cmd == nil || cfg == nil {
		return "", &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("agent.guard.state_incomplete")),
		}
	}

	if err := validateHostAgentManifestState(
		manifest,
		expectedKind,
		false,
	); err != nil {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				cliMessage("agent.scope.host"),
				err,
			),
		}
	}

	doc, indexPath, err := loadIndexForCLI(
		cmd,
		repoRoot,
		cfg,
	)
	if err != nil {
		return "", &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	currentPlan, err := buildAgentPlan(
		repoRoot,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		var buildErr *agentPlanBuildError
		if errors.As(err, &buildErr) {
			return "", &ExitError{
				Code: buildErr.Code,
				Err:  buildErr.Err,
			}
		}

		return "", &ExitError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	if currentPlan.Stage != expectedStage ||
		currentPlan.PlanID != manifest.PlanID {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentPlanExpiredError(
				cliMessage("agent.scope.host"),
				manifest.PlanID,
				expectedStage,
				currentPlan.PlanID,
				currentPlan.Stage,
			),
		}
	}

	if currentPlan.IndexSHA256 !=
		manifest.IndexSHA256 ||
		currentPlan.HeaderSHA256 !=
			manifest.HeaderSHA256 {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				cliMessage("agent.scope.host"),
				fmt.Errorf("%s", cliMessage("agent.guard.manifest_plan_drift")),
			),
		}
	}

	if expectedKind == draft.KindEntries {
		if err := validateHostAgentEntryGeneration(
			manifest,
			currentPlan,
		); err != nil {
			return "", &ExitError{
				Code: ExitInvalid,
				Err: hostAgentManifestDamageError(
					cliMessage("agent.scope.host"),
					err,
				),
			}
		}
	}

	return cliMessage(
		"agent.guard.plan_ok",
		shortAgentStageHash(currentPlan.PlanID),
		currentPlan.Stage,
	), nil
}

// validateHostAgentEntryGeneration核对Entries Manifest中的目标和源码摘要。
//
// Entries Stage允许提交当前Plan的子集，因此不要求覆盖全部Plan目标；但每项
// 必须是唯一、规范、安全且属于当前Plan的drafted/warned目标。
func validateHostAgentEntryGeneration(
	manifest *draft.Manifest,
	currentPlan *agentPlan,
) error {
	if currentPlan == nil {
		return fmt.Errorf("%s", cliMessage("agent.guard.plan_nil"))
	}

	targets := make(
		map[string]agentPlanTarget,
		len(currentPlan.Targets),
	)
	for _, target := range currentPlan.Targets {
		targets[target.Path] = target
	}

	seen := map[string]bool{}
	actionable := 0

	for position, status := range manifest.Entries {
		if status.Status != "drafted" &&
			status.Status != "warned" {
			return fmt.Errorf("%s", cliMessage(
				"agent.guard.status_invalid",
				position,
				status.Status,
			))
		}

		rel, err := afs.NormalizeRelPath(
			status.Path,
		)
		if err != nil {
			return fmt.Errorf("%s", cliMessage(
				"agent.guard.path_unsafe",
				position,
				status.Path,
				localeSafeCLIDetail(err.Error()),
			))
		}
		if rel != status.Path {
			return fmt.Errorf("%s", cliMessage(
				"agent.guard.path_noncanonical",
				position,
				status.Path,
			))
		}

		actionable++

		if seen[rel] {
			return fmt.Errorf("%s", cliMessage("agent.guard.path_duplicate", rel))
		}
		seen[rel] = true

		target, found := targets[rel]
		if !found {
			return fmt.Errorf("%s", cliMessage("agent.guard.path_not_target", rel))
		}

		field := fmt.Sprintf(
			"manifest.entries[%d].source_sha256",
			position,
		)
		if err := validateManifestSHA256(
			field,
			status.SourceSHA256,
		); err != nil {
			return err
		}

		if status.SourceSHA256 != target.SourceSHA256 {
			return fmt.Errorf("%s", cliMessage(
				"agent.guard.source_drift",
				rel,
				shortAgentStageHash(
					status.SourceSHA256,
				),
				shortAgentStageHash(
					target.SourceSHA256,
				),
			))
		}
	}

	if actionable == 0 {
		return fmt.Errorf("%s", cliMessage("agent.guard.no_actionable"))
	}

	return nil
}
