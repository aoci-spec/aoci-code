// Init合同和最小索引模板的变量、格式及关键机器Token测试。
package textassets

import (
	"strings"
	"testing"
)

func TestInitStableMessagesRemainSingleLine(
	t *testing.T,
) {
	ids := []ID{
		ContractInitNextStep,
		ContractInitHeaderDictionaryLine,
		ContractInitAutomationAuto,
		ContractInitAutomationReview,
		ContractInitAutomationLegacy,
		ContractInitAutomationOff,
	}

	for _, id := range ids {
		value := MustRender(
			LegacyLocale,
			id,
			nil,
		)

		if strings.Contains(
			value,
			"\n",
		) {
			t.Fatalf(
				"Init稳定提示必须保持单行: id=%s value=%q",
				id,
				value,
			)
		}
	}
}

func TestInitFullIndexTemplateKeepsStrictVariables(
	t *testing.T,
) {
	raw := MustLoad(
		LegacyLocale,
		ContractInitFullIndexLine,
	)

	for _, token := range []string{
		"{{.GuideCommand}}",
		"{{.AutomationMode}}",
		"{{.AutomationHint}}",
		"automation.mode=",
	} {
		if !strings.Contains(
			raw,
			token,
		) {
			t.Fatalf(
				"Init完整索引提示缺少变量或协议Token %q:\n%s",
				token,
				raw,
			)
		}
	}
}

func TestInitAutomationAssetsKeepModeIdentity(
	t *testing.T,
) {
	tests := []struct {
		id    ID
		token string
	}{
		{
			id:    ContractInitAutomationAuto,
			token: "auto",
		},
		{
			id:    ContractInitAutomationReview,
			token: "review",
		},
		{
			id:    ContractInitAutomationLegacy,
			token: "legacy",
		},
		{
			id:    ContractInitAutomationOff,
			token: "off",
		},
	}

	for _, current := range tests {
		value := MustRender(
			LegacyLocale,
			current.id,
			nil,
		)

		if !strings.HasPrefix(
			value,
			current.token+" ",
		) {
			t.Fatalf(
				"Init模式提示身份不符: id=%s value=%q",
				current.id,
				value,
			)
		}
	}
}

func TestMinimalIndexTemplateKeepsStrictVariablesAndBoundaries(
	t *testing.T,
) {
	raw := MustLoad(
		LegacyLocale,
		TemplateMinimalIndex,
	)

	requiredTokens := []string{
		"#===头部索引===",
		"#===头部索引完毕===",
		"#===索引规范===",
		"#===索引规范完毕===",
		"#代码索引",
		"#代码索引完毕",
		"F:",
		"R:",
		"A:",
		"S:",
		"#S配额:",
	}

	for _, token := range requiredTokens {
		if !strings.Contains(
			raw,
			token,
		) {
			t.Fatalf(
				"MinimalIndex缺少解析或认知Token %q",
				token,
			)
		}
	}

	if strings.Count(
		raw,
		"{{.ProjectName}}",
	) != 2 {
		t.Fatalf(
			"ProjectName占位符数量变化",
		)
	}

	if strings.Count(
		raw,
		"{{.RepoRootSlash}}",
	) != 2 {
		t.Fatalf(
			"RepoRootSlash占位符数量变化",
		)
	}

	if strings.Count(
		raw,
		"{{.",
	) != 4 {
		t.Fatalf(
			"MinimalIndex出现未登记模板变量",
		)
	}

	if !strings.HasSuffix(
		raw,
		"\n",
	) {
		t.Fatal(
			"MinimalIndex必须保留终止换行",
		)
	}
}
