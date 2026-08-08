// CLI Long Help资产的尾换行、标量渲染及关键Token测试。
package textassets

import (
	"strings"
	"testing"
)

func TestCLIHelpAssetsRenderWithoutTrailingNewline(
	t *testing.T,
) {
	ids := []ID{
		ContractHelpRootLong,
		ContractHelpDoctorLong,
		ContractHelpIndexLong,
		ContractHelpIndexUpdateLong,
		ContractHelpVerifyLong,
		ContractHelpCheckLong,
		ContractHelpReadObservationAudit,
		ContractHelpVerifyShort,
		ContractHelpCheckShort,
		ContractHelpIndexScoreShort,
		ContractHelpIndexInventoryShort,
		ContractHelpIndexInventoryLong,
		ContractHelpScanLong,
		ContractHelpRemoveEntryLong,
	}

	for _, id := range ids {
		raw := MustLoad(
			LegacyLocale,
			id,
		)

		if !strings.HasSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"CLI Help资产必须保留文件终止换行: %s",
				id,
			)
		}

		rendered := MustRender(
			LegacyLocale,
			id,
			nil,
		)

		if rendered != strings.TrimSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"CLI Help标量渲染只允许移除一个文件尾换行: %s",
				id,
			)
		}

		if strings.HasSuffix(
			rendered,
			"\n",
		) {
			t.Fatalf(
				"CLI Long字段不得携带尾换行: %s",
				id,
			)
		}
	}
}

func TestCLIHelpAssetsKeepStableContracts(
	t *testing.T,
) {
	tests := []struct {
		id      ID
		anchors []string
	}{
		{
			id: ContractHelpRootLong,
			anchors: []string{
				"aoci.txt",
				".aoci/",
				"零依赖离线工具",
				"AI 端点",
			},
		},
		{
			id: ContractHelpDoctorLong,
			anchors: []string{
				"--net",
				"数据主权",
				"不经第三方",
			},
		},
		{
			id: ContractHelpIndexLong,
			anchors: []string{
				"inventory:",
				"entries:",
				"agent:",
			},
		},
		{
			id: ContractHelpIndexUpdateLong,
			anchors: []string{
				"curation_excluded:",
				"pending_curation:",
				"CRLF/LF",
				"--dry-run",
			},
		},
		{
			id: ContractHelpVerifyLong,
			anchors: []string{
				"Result.Missing",
				"RawMissing",
				"LineEndingOnly",
				"PendingCuration",
				"exit 1",
				"exit 0",
			},
		},
		{
			id: ContractHelpCheckLong,
			anchors: []string{
				"ActionableMissing",
				"PendingCurationMissing",
				"CurationExcludedMissing",
				"tagparse Warning",
				"aoci verify",
			},
		},
		{
			id: ContractHelpReadObservationAudit,
			anchors: []string{
				"verify",
				"check",
				"index score",
				"index inventory",
				"不修改正式索引或Baseline",
				"不等同于严格零文件写入",
				"Ledger",
				"Verify History",
				"退出码与治理判据",
			},
		},
		{
			id: ContractHelpScanLong,
			anchors: []string{
				"SHA256",
				".aoci/baseline.json",
				"--force",
				"一键洗白",
			},
		},
		{
			id: ContractHelpRemoveEntryLong,
			anchors: []string{
				"aoci index update",
				"deleted",
				"Missing",
				"verify",
				"--preview",
			},
		},
	}

	for _, current := range tests {
		value := MustRender(
			LegacyLocale,
			current.id,
			nil,
		)

		for _, anchor := range current.anchors {
			if !strings.Contains(
				value,
				anchor,
			) {
				t.Fatalf(
					"CLI Help资产缺少稳定合同Token %q: id=%s",
					anchor,
					current.id,
				)
			}
		}
	}
}
