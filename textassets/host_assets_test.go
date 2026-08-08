// Host-Agent文本资产的模板事实与机器状态保护测试。
package textassets

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestHostAgentRuntimeAssetsRenderMachineFacts(
	t *testing.T,
) {
	data := machinecontract.NumericText()

	tests := []struct {
		id      ID
		anchors []string
	}{
		{
			id: ContractHostRuntimeHeaderStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", data.HeaderMaxBodyBytes),
				data.HeaderMaxBodyHuman,
				fmt.Sprintf("%d字节", data.HeaderMaxHeaderBytes),
				data.HeaderMaxHeaderHuman,
			},
		},
		{
			id: ContractHostRuntimeEntriesStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", data.EntriesMaxBodyBytes),
				data.EntriesMaxBodyHuman,
				fmt.Sprintf("最多%d条", data.EntriesMaxEntries),
			},
		},
		{
			id: ContractHostRuntimeCurationStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", data.CurationMaxBodyBytes),
				data.CurationMaxBodyHuman,
				fmt.Sprintf("最多%d项", data.CurationMaxDecisions),
				"role和reason",
				"规范化为空格",
			},
		},
	}

	for _, current := range tests {
		rendered := MustRender(
			LegacyLocale,
			current.id,
			data,
		)

		for _, anchor := range current.anchors {
			if !strings.Contains(
				rendered,
				anchor,
			) {
				t.Fatalf(
					"Host-Agent运行时资产%s缺少%q:\n%s",
					current.id,
					anchor,
					rendered,
				)
			}
		}
	}
}

func TestHostAgentHelpAssetsKeepStableStates(
	t *testing.T,
) {
	data := machinecontract.NumericText()

	entriesHelp := MustRender(
		LegacyLocale,
		ContractHostHelpEntriesStageLong,
		data,
	)

	for _, anchor := range []string{
		"automation.mode=auto",
		"Check、Diff、P-23与原子Apply",
		"repair_required",
		"stopped",
		"review或legacy",
	} {
		if !strings.Contains(
			entriesHelp,
			anchor,
		) {
			t.Fatalf(
				"Entries Stage Help资产缺少%q:\n%s",
				anchor,
				entriesHelp,
			)
		}
	}

	guideHelp := MustRender(
		LegacyLocale,
		ContractHostHelpGuideLong,
		nil,
	)

	for _, anchor := range []string{
		"execute",
		"applied",
		"repair_required",
		"stopped",
		"prepare_and_review",
		"observe",
		"blocked",
		"complete",
	} {
		if !strings.Contains(
			guideHelp,
			anchor,
		) {
			t.Fatalf(
				"Guide Help资产缺少%q:\n%s",
				anchor,
				guideHelp,
			)
		}
	}

	headerHelp := MustRender(
		LegacyLocale,
		ContractHostHelpHeaderStageLong,
		data,
	)
	for _, anchor := range []string{
		"intent=semantic_refresh",
		"aligned",
		"Manifest",
	} {
		if !strings.Contains(headerHelp, anchor) {
			t.Fatalf(
				"Header Stage Help资产缺少%q:\n%s",
				anchor,
				headerHelp,
			)
		}
	}
}
