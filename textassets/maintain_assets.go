package textassets

const (
	// ContractMaintainToolDescription是aoci_maintain向宿主模型公开的
	// MCP工具用途、时机与安全边界说明。
	ContractMaintainToolDescription ID = "contracts/maintain/tool-description"

	// ContractMaintainDictionaryUnparseable是头部存在疑似字典行、但机器无法
	// 提取符号时返回的恢复说明。
	ContractMaintainDictionaryUnparseable ID = "contracts/maintain/dictionary-unparseable"

	// ContractMaintainDictionaryMissing是索引头尚未建立可用字典时返回的
	// 恢复说明。
	ContractMaintainDictionaryMissing ID = "contracts/maintain/dictionary-missing"

	ContractMaintainActionRepositoryFailure     ID = "contracts/maintain/actions/repository-failure"
	ContractMaintainActionDictionaryUnparseable ID = "contracts/maintain/actions/dictionary-unparseable"
	ContractMaintainActionDictionaryMissing     ID = "contracts/maintain/actions/dictionary-missing"
	ContractMaintainActionSnapshotFailure       ID = "contracts/maintain/actions/snapshot-failure"
	ContractMaintainActionCurationInvalid       ID = "contracts/maintain/actions/curation-invalid"
	ContractMaintainActionFormatOnlyFailure     ID = "contracts/maintain/actions/format-only-failure"
	ContractMaintainActionBlocked               ID = "contracts/maintain/actions/blocked"
	ContractMaintainActionCandidates            ID = "contracts/maintain/actions/candidates"
	ContractMaintainActionAligned               ID = "contracts/maintain/actions/aligned"
	ContractMaintainActionApplyRemaining        ID = "contracts/maintain/actions/apply-remaining"
	ContractMaintainActionApplyDuplicate        ID = "contracts/maintain/actions/apply-duplicate"
	ContractMaintainActionUpdateRepair          ID = "contracts/maintain/actions/update-repair"
	ContractMaintainActionUpdateStopped         ID = "contracts/maintain/actions/update-stopped"
	ContractMaintainActionBaselineReplay        ID = "contracts/maintain/actions/baseline-replay"
)
