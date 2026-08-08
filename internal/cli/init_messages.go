// Init稳定用户输出的文本资产消费者。
//
// 本文件只负责自然语言表达:
//   - 首次Baseline与MCP认知入口;
//   - 完整索引Guide行;
//   - 新建骨架后的Header提示;
//   - 四种automation.mode的稳定解释。
//
// 参数校验、模式策略、文件创建、配置保存和Baseline前移仍由Go状态机负责。
package cli

import (
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// initFullIndexTemplateData是完整索引提示的严格模板变量。
type initFullIndexTemplateData struct {
	GuideCommand   string
	AutomationMode string
	AutomationHint string
}

// initNextStepMessage返回初始化成功后的固定后续操作说明。
func initNextStepMessage() (string, error) {
	return textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.ContractInitNextStep,
		nil,
	)
}

// initFullIndexMessage绑定当前Agent、automation.mode和对应模式解释。
func initFullIndexMessage(
	agent string,
	mode string,
) (string, error) {
	hint, err := agentAutomationInitHint(mode)
	if err != nil {
		return "", err
	}

	return textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.ContractInitFullIndexLine,
		initFullIndexTemplateData{
			GuideCommand: initAgentGuideCommand(
				agent,
			),
			AutomationMode: mode,
			AutomationHint: hint,
		},
	)
}

// initHeaderDictionaryMessage返回新建最小骨架后的Header生成提示。
func initHeaderDictionaryMessage() (string, error) {
	return textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.ContractInitHeaderDictionaryLine,
		nil,
	)
}

// agentAutomationInitHint返回init完成后的模式说明。
//
// 模式解析与权限策略仍复用resolveAgentAutomationPolicy；文本资产只表达已经
// 决定的策略，不参与权限计算。
func agentAutomationInitHint(
	rawMode string,
) (string, error) {
	policy, err := resolveAgentAutomationPolicy(
		rawMode,
	)
	if err != nil {
		return cliMessage("init.automation_invalid"), nil
	}

	switch policy.Mode {
	case config.AutomationModeAuto:
		return textassets.RenderScalar(
			textassets.ActiveLocale(),
			textassets.ContractInitAutomationAuto,
			nil,
		)

	case config.AutomationModeReview:
		return textassets.RenderScalar(
			textassets.ActiveLocale(),
			textassets.ContractInitAutomationReview,
			nil,
		)

	case config.AutomationModeLegacy:
		return textassets.RenderScalar(
			textassets.ActiveLocale(),
			textassets.ContractInitAutomationLegacy,
			nil,
		)

	case config.AutomationModeOff:
		return textassets.RenderScalar(
			textassets.ActiveLocale(),
			textassets.ContractInitAutomationOff,
			nil,
		)

	default:
		return cliMessage("init.automation_fallback"), nil
	}
}
