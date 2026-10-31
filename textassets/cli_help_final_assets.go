// Stable IDs for the final CLI long-help asset group.
//
// These IDs carry existing stable descriptions. Except for Short help moved to
// the shared observation contract, flag usage, dynamic reports, errors, exit
// codes, approvals, CAS, and safety controls remain in business code.
package textassets

const (
	// ContractHelpIndexScoreLong describes the nine-dimension score and the tagparse semantics.
	ContractHelpIndexScoreLong ID = "contracts/help/index-score-long"

	// ContractHelpIndexAgentLong describes the Host Agent deterministic protocol capabilities.
	ContractHelpIndexAgentLong ID = "contracts/help/index-agent-long"

	// ContractHelpUpdateEntryLong describes the single-Entry write and validation contract.
	ContractHelpUpdateEntryLong ID = "contracts/help/update-entry-long"
)
