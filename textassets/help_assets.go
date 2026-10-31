// Stable CLI help text asset IDs.
//
// Most Short help, flag usage, dynamic reports, and error messages remain in
// Go. Observation-command summaries and stable long descriptions live here
// when their cross-surface contract requires independent versioning.
package textassets

const (
	// ContractHelpRootLong explains the AOCI root command's product position and its offline/AI dual form.
	ContractHelpRootLong ID = "contracts/help/root-long"

	// ContractHelpDoctorLong explains Doctor's read-only diagnosis and the explicit network boundary.
	ContractHelpDoctorLong ID = "contracts/help/doctor-long"

	// ContractHelpIndexLong explains the governance processes of the Index command group.
	ContractHelpIndexLong ID = "contracts/help/index-long"

	// ContractHelpVerifyLong explains Verify's raw facts, governance debt, and exit codes.
	ContractHelpVerifyLong ID = "contracts/help/verify-long"

	// ContractHelpCheckLong explains Check's blocking and non-blocking dimensions.
	ContractHelpCheckLong ID = "contracts/help/check-long"

	// ContractHelpScanLong explains Scan's Baseline rebuild and whitewash prevention semantics.
	ContractHelpScanLong ID = "contracts/help/scan-long"

	// ContractHelpRemoveEntryLong explains the governance consequences of the manual Entry removal.
	ContractHelpRemoveEntryLong ID = "contracts/help/remove-entry-long"

	// ContractHelpReadObservationAudit is the shared read-only and local-audit
	// boundary for observation commands and public documentation.
	ContractHelpReadObservationAudit ID = "contracts/help/read-observation-audit"

	// ContractHelpVerifyShort is the stable Verify summary shown by Cobra.
	ContractHelpVerifyShort ID = "contracts/help/verify-short"

	// ContractHelpCheckShort is the stable Check summary shown by Cobra.
	ContractHelpCheckShort ID = "contracts/help/check-short"

	// ContractHelpIndexScoreShort is the stable Index Score summary.
	ContractHelpIndexScoreShort ID = "contracts/help/index-score-short"

	// ContractHelpIndexInventoryShort is the stable Index Inventory summary.
	ContractHelpIndexInventoryShort ID = "contracts/help/index-inventory-short"

	// ContractHelpIndexInventoryLong describes the Inventory observation.
	ContractHelpIndexInventoryLong ID = "contracts/help/index-inventory-long"
)
