// AGENTS模板资产的标记、机器Token和物理格式测试。
package textassets

import (
	"strings"
	"testing"
)

func TestAgentsTemplateKeepsManagedMarkersAndContracts(
	t *testing.T,
) {
	value := MustLoad(
		LegacyLocale,
		TemplateAgentsMD,
	)

	if !strings.HasPrefix(
		value,
		"<!-- aoci:begin -->\n",
	) {
		t.Fatalf(
			"AGENTS模板缺少标准起始标记: %q",
			value,
		)
	}

	if !strings.HasSuffix(
		value,
		"<!-- aoci:end -->\n",
	) {
		t.Fatalf(
			"AGENTS模板缺少标准结束标记或终止换行: %q",
			value,
		)
	}

	for _, token := range []string{
		"<!-- aoci:begin -->",
		"<!-- aoci:end -->",
		"aoci.txt",
		"Entry",
		"F/R/A/S",
		"aoci_rules",
		"aoci_overview",
		"aoci_maintain",
		"aoci_update_entry",
		"aoci_report",
		"repair_required",
		"stopped",
		"source_sha256",
		".aoci",
	} {
		if !strings.Contains(
			value,
			token,
		) {
			t.Fatalf(
				"AGENTS模板缺少机器合同%q",
				token,
			)
		}
	}

	if strings.Contains(
		value,
		"{{",
	) {
		t.Fatal(
			"当前AGENTS模板不得引入未登记的动态模板占位符",
		)
	}
}

func TestAgentsManifestKeepsReadonlySemanticsInLocaleAnchors(
	t *testing.T,
) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}

	for _, asset := range manifest.Assets {
		if asset.ID != string(TemplateAgentsMD) {
			continue
		}

		anchors := asset.LocaleAnchors[LegacyLocale]
		for _, expected := range []string{
			"不自动等于严格零写入",
			"Codex Memory和历史Skill只能辅助",
			"不得静默以Memory替代当前仓库认知",
		} {
			if !containsString(anchors, expected) {
				t.Fatalf("AGENTS Manifest缺少Locale语义锚点%q: %v", expected, anchors)
			}
			if containsString(asset.ProtocolTokens, expected) {
				t.Fatalf("AGENTS自然语言语义不得登记为protocol_token: %q", expected)
			}
		}

		return
	}

	t.Fatal("AGENTS模板未登记到Manifest")
}
