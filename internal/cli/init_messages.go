// The consumer of the stable Init user-output text assets.
//
// This file is responsible only for natural-language expression:
//   - the first Baseline and the MCP cognition entry;
//   - the complete-index Guide line;
//   - the Header hint after a new skeleton;
//   - the stable explanations of the four automation.mode values.
//
// Argument validation, mode policy, file creation, configuration saving, and the Baseline advance remain the Go state machine's job.
package cli

import (
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// initFullIndexTemplateData holds the strict template variables of the complete-index hint.
type initFullIndexTemplateData struct {
	GuideCommand   string
	AutomationMode string
	AutomationHint string
}

// initNextStepMessage returns the fixed follow-up explanation after a successful initialization.
func initNextStepMessage() (string, error) {
	return textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.ContractInitNextStep,
		nil,
	)
}

// initFullIndexMessage binds the current Agent, automation.mode, and the matching mode explanation.
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

// initHeaderDictionaryMessage returns the Header generation hint after a new minimal skeleton.
func initHeaderDictionaryMessage() (string, error) {
	return textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.ContractInitHeaderDictionaryLine,
		nil,
	)
}

// agentAutomationInitHint returns the mode explanation after init completes.
//
// Mode parsing reuses config.ParseAutomationMode; the text asset only expresses the mode already decided
// and takes no part in permission computation.
func agentAutomationInitHint(
	rawMode string,
) (string, error) {
	mode, err := config.ParseAutomationMode(
		rawMode,
	)
	if err != nil {
		return cliMessage("init.automation_invalid"), nil
	}

	switch mode {
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
