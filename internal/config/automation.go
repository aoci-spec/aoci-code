// 自动化治理模式定义、兼容解释与团队策略边界。
//
// 索引条目: automation.go[GCF7S]
//
// D69 已实现的 `aoci index update` 四模式:
//
//	legacy:
//	  配置字段缺失的旧仓兼容态。保持升级前行为：显式执行 update 时调用
//	  用户配置的模型端点生成草稿，随后由人运行 check/diff/apply。
//	off:
//	  团队明确关闭 update 的 AI 编排。命令只做确定性漂移检测和报告，
//	  不构造 AI client、不校验密钥、不创建草稿、不发送源码。
//	review:
//	  update 自动起草并调用共用机器预检，始终停在草稿区等待人工
//	  diff/apply；机器拒绝或无草稿项返回 ExitInvalid。
//	auto:
//	  update 在 generation 完整、机器预检通过、P-23 摘要一致后，使用
//	  原子批量回写应用整批；任一失败均保留草稿且不得部分应用。
//
// 自动工作流绝不静默删除源码文件或无治理依据的 Entry。auto 模式可以在
// Scope Policy、Retention Review、精确 Envelope、CAS 与可恢复事务全部
// 通过时，原子退役角色转换所覆盖的正式 Entry；Orphan 仍然只报告。
//
// Automation 属团队治理资产，只允许 config.json 声明。config.Load 会在
// 合并 local 层后恢复团队值，防止个人 config.local.json 静默改写团队策略。
package config

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	AutomationModeLegacy = "legacy"
	AutomationModeOff    = "off"
	AutomationModeReview = "review"
	AutomationModeAuto   = "auto"
)

// AutomationConfig 是 config.json 中的 automation 配置块。
type AutomationConfig struct {
	Mode string `json:"mode"`
}

// AutomationPolicy is the machine-selected policy captured by a Plan and an
// Onboarding Session. Source explains whether the mode came from team config,
// the Fresh Bootstrap default, or the legacy compatibility boundary.
type AutomationPolicy struct {
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

// ResolveOnboardingAutomation selects a policy without changing configuration.
// Missing automation stays legacy everywhere except a proven Fresh Bootstrap.
func (c *Config) ResolveOnboardingAutomation(freshBootstrap bool) AutomationPolicy {
	if c != nil && c.HasDeclaredAutomationMode() {
		return AutomationPolicy{Mode: c.EffectiveAutomationMode(), Source: machinecontract.CognitionAutomationPolicyTeamConfig}
	}
	if freshBootstrap {
		return AutomationPolicy{Mode: AutomationModeAuto, Source: machinecontract.CognitionAutomationPolicyFreshDefault}
	}
	return AutomationPolicy{Mode: AutomationModeLegacy, Source: machinecontract.CognitionAutomationPolicyLegacy}
}

// ParseAutomationMode 把用户输入归一为规范模式。
// manual 仅作为 CLI 友好别名，落盘时归一为 legacy，即删除配置块。
func ParseAutomationMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", AutomationModeLegacy, "manual":
		return AutomationModeLegacy, nil
	case AutomationModeOff:
		return AutomationModeOff, nil
	case AutomationModeReview:
		return AutomationModeReview, nil
	case AutomationModeAuto:
		return AutomationModeAuto, nil
	default:
		return "", fmt.Errorf(
			"automation mode 非法: %q(可用: legacy/manual/off/review/auto)",
			raw,
		)
	}
}

// EffectiveAutomationMode 返回当前生效模式。
// Automation=nil 是旧仓兼容态，不得擅自解释成 auto 或 off。
func (c *Config) EffectiveAutomationMode() string {
	if c == nil || c.Automation == nil {
		return AutomationModeLegacy
	}
	mode, err := ParseAutomationMode(c.Automation.Mode)
	if err != nil {
		return c.Automation.Mode
	}
	return mode
}

// HasDeclaredAutomationMode 判断团队配置是否显式声明了 automation。
// legacy 以字段缺失表达，因此返回 false。
func (c *Config) HasDeclaredAutomationMode() bool {
	return c != nil && c.Automation != nil
}

// SetAutomationMode 写入规范模式。
// legacy/manual 会把 Automation 设为 nil，从 JSON 中移除字段，恢复旧仓兼容态。
func (c *Config) SetAutomationMode(raw string) error {
	mode, err := ParseAutomationMode(raw)
	if err != nil {
		return err
	}
	if mode == AutomationModeLegacy {
		c.Automation = nil
		return nil
	}
	c.Automation = &AutomationConfig{Mode: mode}
	return nil
}

// normalizeAutomationConfig 在加载后校验并规范化模式。
func normalizeAutomationConfig(c *Config) error {
	if c == nil || c.Automation == nil {
		return nil
	}
	mode, err := ParseAutomationMode(c.Automation.Mode)
	if err != nil {
		return err
	}
	if mode == AutomationModeLegacy {
		c.Automation = nil
		return nil
	}
	c.Automation.Mode = mode
	return nil
}

// cloneAutomationConfig 复制团队 automation 配置。
// Load 合并 local 前保存团队值，合并后再恢复，防 local 层越权覆盖。
func cloneAutomationConfig(in *AutomationConfig) *AutomationConfig {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
