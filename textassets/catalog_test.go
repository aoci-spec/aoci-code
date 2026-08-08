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
			"Codex Memory和历史Skill只能辅助",
			"不能替代匹配当前AOCI身份的认知收据",
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
