package textassets

const (
	// ContractMaintainToolDescription is the explanation of the purpose, timing, and safety boundary of the MCP tool
	// that aoci_maintain exposes to the host model.
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
