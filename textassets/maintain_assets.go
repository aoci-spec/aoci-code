package textassets

const (
	// ContractMaintainToolDescription是aoci_maintain向宿主模型公开的
	// MCP工具用途、时机与安全边界说明。
	ContractMaintainToolDescription ID = "contracts/maintain/tool-description"

	ContractMaintainActionAligned                  ID = "contracts/maintain/actions/aligned"
	ContractMaintainActionApplyRemaining           ID = "contracts/maintain/actions/apply-remaining"
	ContractMaintainActionApplyDuplicate           ID = "contracts/maintain/actions/apply-duplicate"
	ContractMaintainActionApplyFinalProof          ID = "contracts/maintain/actions/apply-final-proof"
	ContractMaintainActionApplyDuplicateFinalProof ID = "contracts/maintain/actions/apply-duplicate-final-proof"
	ContractMaintainActionUpdateRepair             ID = "contracts/maintain/actions/update-repair"
	ContractMaintainActionUpdateStopped            ID = "contracts/maintain/actions/update-stopped"
	ContractMaintainActionBaselineReplay           ID = "contracts/maintain/actions/baseline-replay"
)
