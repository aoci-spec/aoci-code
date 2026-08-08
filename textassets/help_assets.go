// Stable CLI help text asset IDs.
//
// Most Short help, flag usage, dynamic reports, and error messages remain in
// Go. Observation-command summaries and stable long descriptions live here
// when their cross-surface contract requires independent versioning.
package textassets

const (
	// ContractHelpRootLong说明AOCI根命令的产品定位和离线/AI双形态。
	ContractHelpRootLong ID = "contracts/help/root-long"

	// ContractHelpDoctorLong说明Doctor只读诊断与显式联网边界。
	ContractHelpDoctorLong ID = "contracts/help/doctor-long"

	// ContractHelpIndexLong说明Index命令组的各治理工序。
	ContractHelpIndexLong ID = "contracts/help/index-long"

	// ContractHelpIndexUpdateLong说明Update的漂移分类与零外发边界。
	ContractHelpIndexUpdateLong ID = "contracts/help/index-update-long"

	// ContractHelpVerifyLong说明Verify原始事实、治理债务与退出码。
	ContractHelpVerifyLong ID = "contracts/help/verify-long"

	// ContractHelpCheckLong说明Check提交阻断与非阻断维度。
	ContractHelpCheckLong ID = "contracts/help/check-long"

	// ContractHelpScanLong说明Scan重建Baseline及防洗白语义。
	ContractHelpScanLong ID = "contracts/help/scan-long"

	// ContractHelpRemoveEntryLong说明人工删除Entry的治理后果。
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
