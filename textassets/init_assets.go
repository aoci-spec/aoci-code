// Init运行提示和仓库物化模板的稳定文本资产ID。
package textassets

const (
	// ContractInitNextStep是初始化成功后的Baseline与MCP认知入口提示。
	ContractInitNextStep ID = "contracts/init/next-step"

	// ContractInitFullIndexLine是带Guide命令、当前automation.mode和模式解释的
	// 完整索引后续操作行。
	ContractInitFullIndexLine ID = "contracts/init/full-index-line"

	// ContractInitHeaderDictionaryLine是新建最小骨架后显示的Header生成提示。
	ContractInitHeaderDictionaryLine ID = "contracts/init/header-dictionary-line"

	// ContractInitAutomationAuto解释新仓默认auto模式的宿主执行与自动修复合同。
	ContractInitAutomationAuto ID = "contracts/init/automation-auto"

	// ContractInitAutomationReview解释显式review模式的人工Apply停点。
	ContractInitAutomationReview ID = "contracts/init/automation-review"

	// ContractInitAutomationLegacy解释旧仓字段缺失时的兼容停点。
	ContractInitAutomationLegacy ID = "contracts/init/automation-legacy"

	// ContractInitAutomationOff解释只读观察模式的权限边界。
	ContractInitAutomationOff ID = "contracts/init/automation-off"

	// TemplateMinimalIndex是aoci init物化到仓库根的最小索引骨架。
	TemplateMinimalIndex ID = "templates/minimal-index"
	// TemplateVolumeRoot is the semantic-free Root created for a new project.
	TemplateVolumeRoot ID = "templates/volume-root"
	// TemplateVolumeMeta is the stable starter governance contract for an empty
	// Code Volume. It contains no repository or database object semantics.
	TemplateVolumeMeta ID = "templates/volume-meta"
	// TemplateAOCIGitignore is the project-local Git boundary created by init.
	TemplateAOCIGitignore ID = "templates/aoci-gitignore"
)
