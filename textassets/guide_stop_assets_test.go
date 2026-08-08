// Guide阻断停点资产的尾换行、标量和机器Token测试。
package textassets

import (
	"reflect"
	"strings"
	"testing"
)

func TestGuideStopAssetsRenderStableContracts(
	t *testing.T,
) {
	tests := []struct {
		id      ID
		lines   bool
		anchors []string
	}{
		{
			id: ContractGuideIndexReviewBlockedMessage,
			anchors: []string{
				"Baseline",
				"宿主Agent",
				"停止",
				"维护者",
			},
		},
		{
			id:    ContractGuideIndexReviewBlockedInstructions,
			lines: true,
			anchors: []string{
				"scan --force",
				"aoci.txt",
				"Plan",
				"Git",
			},
		},
		{
			id: ContractGuideOrphanReviewBlockedMessage,
			anchors: []string{
				"孤儿条目",
				"自动工序",
				"维护者",
			},
		},
		{
			id:    ContractGuideOrphanReviewBlockedInstructions,
			lines: true,
			anchors: []string{
				"工作区文件",
				"orphans",
				"remove-entry",
			},
		},
	}

	for _, current := range tests {
		raw := MustLoad(
			LegacyLocale,
			current.id,
		)

		if !strings.HasSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"Guide阻断资产必须保留文件终止换行: %s",
				current.id,
			)
		}

		rendered := MustRender(
			LegacyLocale,
			current.id,
			nil,
		)

		if rendered != strings.TrimSuffix(
			raw,
			"\n",
		) {
			t.Fatalf(
				"Guide阻断标量渲染只允许移除一个尾换行: %s",
				current.id,
			)
		}

		for _, anchor := range current.anchors {
			if !strings.Contains(
				rendered,
				anchor,
			) {
				t.Fatalf(
					"Guide阻断资产缺少机器Token %q: %s",
					anchor,
					current.id,
				)
			}
		}

		if current.lines {
			lines := MustRenderLines(
				LegacyLocale,
				current.id,
				nil,
			)

			if !reflect.DeepEqual(
				lines,
				strings.Split(
					rendered,
					"\n",
				),
			) {
				t.Fatalf(
					"Guide阻断Instructions分行顺序发生变化: %s",
					current.id,
				)
			}

			if len(lines) != 2 {
				t.Fatalf(
					"Guide阻断Instructions必须保持两条: %s got=%d",
					current.id,
					len(lines),
				)
			}
		}
	}
}
