// Host-Agent automation.mode的唯一策略映射。
//
// 本文件只回答三个问题:
//   - Guide应向宿主Agent授予什么执行权限;
//   - Stage是否允许创建草稿;
//   - Apply前是否必须等待人工批准。
//
// 它不调用模型、不读取源码、不创建草稿、不修改索引或Baseline。
//
// 稳定语义:
//   - auto:   宿主Agent严格按当前阶段Guide执行，不等待人工批准；Entries Stage
//     在内部完成Check、Diff、P-23和原子Apply。applied后Verify并重新Guide；
//     repair_required只修失败候选并自动重新Stage；stopped结束当前写尝试，
//     后续由既有zero-write/Recovery证据分类。
//   - review: 宿主Agent可执行Stage、Check、Diff，但必须在Apply前等待批准。
//   - legacy: 旧仓兼容态，停点与review相同，但不是团队显式review声明。
//   - off:    只允许Plan、Guide、Verify等确定性观察；禁止Host-Agent Stage，
//     不创建草稿、不生成候选、不Apply。
//
// blocked、Unbaselined、索引外部漂移、Orphan、计划过期、P-23、Safety、CAS和
// 写入状态不确定等安全防线不因auto而降级。候选内容错误不绕过校验，但应由
// 宿主自动修复，不应升级为用户交互停点。
package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
)

const (
	agentGuideModeExecute       = "execute"
	agentGuideModePrepareReview = "prepare_and_review"
	agentGuideModeObserve       = "observe"
	agentGuideModeBlocked       = "blocked"
	agentGuideModeComplete      = "complete"
)

// agentAutomationPolicy是automation.mode在Host-Agent协议中的稳定解释。
type agentAutomationPolicy struct {
	Mode                 string
	GuideMode            string
	AllowStage           bool
	ApprovalRequired     bool
	StopBeforeApply      bool
	ContinueThroughApply bool
}

// resolveAgentAutomationPolicy把配置模式映射为Host-Agent行为策略。
func resolveAgentAutomationPolicy(
	rawMode string,
) (agentAutomationPolicy, error) {
	mode, err := config.ParseAutomationMode(rawMode)
	if err != nil {
		return agentAutomationPolicy{}, err
	}

	switch mode {
	case config.AutomationModeAuto:
		return agentAutomationPolicy{
			Mode:                 mode,
			GuideMode:            agentGuideModeExecute,
			AllowStage:           true,
			ApprovalRequired:     false,
			StopBeforeApply:      false,
			ContinueThroughApply: true,
		}, nil

	case config.AutomationModeReview:
		return agentAutomationPolicy{
			Mode:                 mode,
			GuideMode:            agentGuideModePrepareReview,
			AllowStage:           true,
			ApprovalRequired:     true,
			StopBeforeApply:      true,
			ContinueThroughApply: false,
		}, nil

	case config.AutomationModeLegacy:
		return agentAutomationPolicy{
			Mode:                 mode,
			GuideMode:            agentGuideModePrepareReview,
			AllowStage:           true,
			ApprovalRequired:     true,
			StopBeforeApply:      true,
			ContinueThroughApply: false,
		}, nil

	case config.AutomationModeOff:
		return agentAutomationPolicy{
			Mode:                 mode,
			GuideMode:            agentGuideModeObserve,
			AllowStage:           false,
			ApprovalRequired:     false,
			StopBeforeApply:      false,
			ContinueThroughApply: false,
		}, nil

	default:
		return agentAutomationPolicy{}, fmt.Errorf("%s", cliMessage(
			"automation.mode.unknown",
			mode,
		))
	}
}

// agentAutomationPolicyForConfig读取当前团队生效策略。
func agentAutomationPolicyForConfig(
	cfg *config.Config,
) (agentAutomationPolicy, error) {
	mode := config.AutomationModeLegacy
	if cfg != nil {
		mode = cfg.EffectiveAutomationMode()
	}

	return resolveAgentAutomationPolicy(
		mode,
	)
}

// guardHostAgentStageAutomation在创建Run之前执行Stage权限硬闸。
func guardHostAgentStageAutomation(
	cfg *config.Config,
	operation string,
) (agentAutomationPolicy, error) {
	policy, err := agentAutomationPolicyForConfig(
		cfg,
	)
	if err != nil {
		return agentAutomationPolicy{}, err
	}
	if !policy.AllowStage {
		return policy, fmt.Errorf("%s", cliMessage(
			"automation.stage.forbidden",
			policy.Mode,
			operation,
		))
	}

	return policy, nil
}
