package textassets

const (
	// ContractHostRuntimeBaseInstructions是所有Host-Agent Guide共享的
	// 可执行文件绝对路径纪律。
	ContractHostRuntimeBaseInstructions ID = "contracts/host-agent/runtime/base-instructions"

	// ContractHostRuntimeWindowsInstructions是仅在Windows Guide中注入的
	// PowerShell、JSON捕获和UTF-8请求传输纪律。
	ContractHostRuntimeWindowsInstructions ID = "contracts/host-agent/runtime/windows-instructions"

	// ContractHostRuntimeHeaderStageLimit表达Header Stage的真实协议上限。
	ContractHostRuntimeHeaderStageLimit ID = "contracts/host-agent/runtime/header-stage-limit"

	// ContractHostRuntimeEntriesStageLimit表达Entries Stage的真实协议上限。
	ContractHostRuntimeEntriesStageLimit ID = "contracts/host-agent/runtime/entries-stage-limit"

	// ContractHostRuntimeCurationStageLimit表达Curation Stage的真实协议上限
	// 与role/reason规范化行为。
	ContractHostRuntimeCurationStageLimit ID = "contracts/host-agent/runtime/curation-stage-limit"

	// ContractHostHelpEntriesStageLong是Entries Stage的完整Help合同。
	ContractHostHelpEntriesStageLong ID = "contracts/host-agent/help/entries-stage-long"

	// ContractHostHelpEntriesStageRequestFile是Entries Stage的
	// request-file参数说明。
	ContractHostHelpEntriesStageRequestFile ID = "contracts/host-agent/help/entries-stage-request-file"

	// ContractHostHelpHeaderStageLong是Header Stage的完整Help合同。
	ContractHostHelpHeaderStageLong ID = "contracts/host-agent/help/header-stage-long"

	// ContractHostHelpHeaderStageRequestFile是Header Stage的
	// request-file参数说明。
	ContractHostHelpHeaderStageRequestFile ID = "contracts/host-agent/help/header-stage-request-file"

	// ContractHostHelpCurationStageLong是Curation Stage的完整Help合同。
	ContractHostHelpCurationStageLong ID = "contracts/host-agent/help/curation-stage-long"

	// ContractHostHelpCurationStageRequestFile是Curation Stage的
	// request-file参数说明。
	ContractHostHelpCurationStageRequestFile ID = "contracts/host-agent/help/curation-stage-request-file"

	// ContractHostHelpStageStdinJSON是三个Stage命令共享的stdin-json说明。
	ContractHostHelpStageStdinJSON ID = "contracts/host-agent/help/stage-stdin-json"

	// ContractHostHelpGuideLong是Host-Agent Guide命令的完整Help合同。
	ContractHostHelpGuideLong ID = "contracts/host-agent/help/guide-long"
)
