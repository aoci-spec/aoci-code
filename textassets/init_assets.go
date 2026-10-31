// The stable text asset IDs of the Init runtime hints and the repository materialization templates.
package textassets

const (
	// ContractInitNextStep is the Baseline and MCP cognition entry hint after a successful initialization.
	ContractInitNextStep ID = "contracts/init/next-step"

	// ContractInitFullIndexLine is the complete-index follow-up line carrying the Guide command, the current automation.mode,
	// and the mode explanation.
	ContractInitFullIndexLine ID = "contracts/init/full-index-line"

	// ContractInitHeaderDictionaryLine is the Header generation hint shown after a new minimal skeleton.
	ContractInitHeaderDictionaryLine ID = "contracts/init/header-dictionary-line"

	// ContractInitAutomationAuto explains the host execution and automatic repair contract of a new repository's default auto mode.
	ContractInitAutomationAuto ID = "contracts/init/automation-auto"

	// ContractInitAutomationReview explains the manual Apply stop of the explicit review mode.
	ContractInitAutomationReview ID = "contracts/init/automation-review"

	// ContractInitAutomationLegacy explains the compatibility stop when an old repository lacks the field.
	ContractInitAutomationLegacy ID = "contracts/init/automation-legacy"

	// ContractInitAutomationOff explains the permission boundary of the read-only observe mode.
	ContractInitAutomationOff ID = "contracts/init/automation-off"

	// TemplateMinimalIndex is the minimal index skeleton aoci init materializes at the repository root.
	TemplateMinimalIndex ID = "templates/minimal-index"
	// TemplateVolumeRoot is the semantic-free Root created for a new project.
	TemplateVolumeRoot ID = "templates/volume-root"
	// TemplateVolumeMeta is the stable starter governance contract for an empty
	// Code Volume. It contains no repository or database object semantics.
	TemplateVolumeMeta ID = "templates/volume-meta"
	// TemplateAOCIGitignore is the project-local Git boundary created by init.
	TemplateAOCIGitignore ID = "templates/aoci-gitignore"
)
