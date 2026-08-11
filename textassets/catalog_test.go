package textassets

import (
	"strings"
	"testing"
)

func TestCatalogValid(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf(
			"embedded text asset catalog is invalid: %v",
			err,
		)
	}
}

func TestLoadRuntimeRulesPreservesExactText(t *testing.T) {
	text, err := Load(
		LegacyLocale,
		ContractRuntimeRules,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(
		text,
		"AOCI 运行合同（会话级）",
	) {
		t.Fatalf(
			"runtime rules have an unexpected prefix: %q",
			text,
		)
	}

	if !strings.HasSuffix(
		text,
		"\n",
	) {
		t.Fatal(
			"runtime rules must preserve the final newline",
		)
	}

	for _, token := range []string{
		"aoci_overview",
		"aoci_maintain",
		"repair_required",
		"stopped",
		"source_sha256",
	} {
		if !strings.Contains(
			text,
			token,
		) {
			t.Fatalf(
				"runtime rules are missing machine token %q",
				token,
			)
		}
	}
}

func TestLongRunningAutoContractsSeparateClaimsFromTaskContinuation(t *testing.T) {
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		rules, err := Load(locale, ContractRuntimeRules)
		if err != nil {
			t.Fatal(err)
		}
		overview, err := Load(locale, ContractMCPOverviewDescription)
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := Load(locale, ContractMCPOverviewAttestationPrompt)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{
			"full_system_claim_disabled_source_bound_task_continuation_allowed",
			"source-bound",
			"generation",
		} {
			if !strings.Contains(rules+overview, token) {
				t.Fatalf("locale %s long-running contract is missing %q", locale, token)
			}
		}
		for _, token := range []string{"Memory", "Search", "Get Entries", "Scope"} {
			if !strings.Contains(prompt, token) {
				t.Fatalf("locale %s Attestation prompt lost no-bypass token %q", locale, token)
			}
		}
	}
}

func TestLoadUsesDefaultLocale(t *testing.T) {
	explicit, err := Load(
		DefaultLocale,
		ContractRuntimeRules,
	)
	if err != nil {
		t.Fatal(err)
	}

	implicit, err := Load(
		"",
		ContractRuntimeRules,
	)
	if err != nil {
		t.Fatal(err)
	}

	if explicit != implicit {
		t.Fatal(
			"empty locale must resolve to the manifest default",
		)
	}
}

func TestLoadRejectsUnknownLocaleAndID(t *testing.T) {
	if _, err := Load(
		"fr-FR",
		ContractRuntimeRules,
	); err == nil {
		t.Fatal(
			"an undeclared locale must be rejected",
		)
	}

	if _, err := Load(
		LegacyLocale,
		ID("contracts/not-found"),
	); err == nil {
		t.Fatal(
			"an undeclared asset id must be rejected",
		)
	}
}

func TestManifestDeclaresRuntimeRules(t *testing.T) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}

	if manifest.DefaultLocale != DefaultLocale {
		t.Fatalf(
			"default locale mismatch: got=%q want=%q",
			manifest.DefaultLocale,
			DefaultLocale,
		)
	}

	found := false

	for _, asset := range manifest.Assets {
		if asset.ID != string(
			ContractRuntimeRules,
		) {
			continue
		}

		found = true

		if asset.Kind != "contract" ||
			asset.Path !=
				"contracts/runtime-rules.txt" ||
			asset.UsedBy == "" {
			t.Fatalf(
				"runtime rules manifest entry is incomplete: %+v",
				asset,
			)
		}
		if !containsString(asset.ProtocolTokens, "cognition_scope") {
			t.Fatalf("runtime rules manifest缺少cognition_scope机器Token: %+v", asset)
		}
		for _, anchor := range []string{
			"不能替代匹配当前AOCI身份的认知收据",
			"该结果保留已交付认知的实际scope和状态",
			"aoci_maintain只用于已建立索引的增量维护",
			"只在当前布局和工具返回状态支持时使用 aoci_report",
		} {
			if !containsString(asset.LocaleAnchors[LegacyLocale], anchor) {
				t.Fatalf("runtime rules manifest缺少Locale语义锚点%q: %+v", anchor, asset)
			}
			if containsString(asset.ProtocolTokens, anchor) {
				t.Fatalf("runtime rules自然语言语义不得登记为protocol_token: %q", anchor)
			}
		}
	}

	if !found {
		t.Fatal(
			"runtime rules are not declared in the manifest",
		)
	}
}

func TestRuntimeRulesStayWithinCognitionBoundary(t *testing.T) {
	tests := []struct {
		locale    string
		required  []string
		forbidden []string
	}{
		{
			locale: DefaultLocale,
			required: []string{
				"stable, versioned, incrementally maintainable repository-level cognition",
				"The result retains delivered cognition's reported scope and state",
				"Only current_system_cognition_reliable=true permits an unqualified current complete-system cognition claim",
				"aoci_maintain is for incremental maintenance of an established index",
				"semantic artifacts required by the current Plan and Guide",
				"Use aoci_report only when the current layout and returned tool state support it",
			},
			forbidden: []string{
				"complements rather than replaces source reading",
				"remain implementation truth",
				"inspect current source first",
				"Ordinary development and user communication",
				"User-visible messages for an ordinary task",
				"formatting, tests, Lint",
				"Commit the index to Git",
				"defines an engineering workflow",
				"do not treat the incomplete content as complete repository cognition",
			},
		},
		{
			locale: LegacyLocale,
			required: []string{
				"稳定、可版本化、可增量维护的仓库级认知",
				"该结果保留已交付认知的实际scope和状态",
				"只有current_system_cognition_reliable=true允许无保留地声称当前完整系统认知可靠",
				"aoci_maintain只用于已建立索引的增量维护",
				"当前Plan与Guide要求的语义产物",
				"只在当前布局和工具返回状态支持时使用 aoci_report",
			},
			forbidden: []string{
				"与源码阅读、LSP、CodeGraph及其他结构化工具互补",
				"仍是实现真值",
				"先检查当前源码",
				"普通开发与用户沟通",
				"用户可见消息聚焦",
				"格式化、测试、Lint",
				"索引进入Git",
				"定义工程开发流程",
				"不得把不完整内容当作完整仓库认知",
			},
		},
	}

	for _, current := range tests {
		t.Run(current.locale, func(t *testing.T) {
			rules, err := Load(current.locale, ContractRuntimeRules)
			if err != nil {
				t.Fatal(err)
			}
			for _, anchor := range current.required {
				if !strings.Contains(rules, anchor) {
					t.Fatalf("%s runtime rules are missing cognition-boundary anchor %q", current.locale, anchor)
				}
			}
			for _, phrase := range current.forbidden {
				if strings.Contains(rules, phrase) {
					t.Fatalf("%s runtime rules retain ordinary-development direction %q", current.locale, phrase)
				}
			}
		})
	}
}
