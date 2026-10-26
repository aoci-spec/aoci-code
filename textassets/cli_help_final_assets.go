// Stable IDs for the final CLI long-help asset group.
//
// These IDs carry existing stable descriptions. Except for Short help moved to
// the shared observation contract, flag usage, dynamic reports, errors, exit
// codes, approvals, CAS, and safety controls remain in business code.
package textassets

const (
	// ContractHelpIndexScoreLong描述九维度评分及tagparse语义。
	ContractHelpIndexScoreLong ID = "contracts/help/index-score-long"

	// ContractHelpIndexAgentLong描述Host-Agent确定性协议能力。
	ContractHelpIndexAgentLong ID = "contracts/help/index-agent-long"

	// ContractHelpUpdateEntryLong描述单条Entry写入与校验合同。
	ContractHelpUpdateEntryLong ID = "contracts/help/update-entry-long"
)
