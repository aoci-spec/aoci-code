// 交付确认与模型认证是同一 body 的两半证据, 各自密码学绑定, 到达顺序不携带信息。
// 这里钉死: 分次提交、任意顺序, 都与同一调用合并提交闩住同样的可靠结论; 一次
// 全新的完整交付重置证据; 明确携带的一半永远以最新为准。
package mcptools

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func deliverLegacyOverview(t *testing.T, session *mcp.ClientSession) (string, map[string]any) {
	t.Helper()
	first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	firstText := resText(t, first)
	bodyBytes, err := strconv.Atoi(overviewMetadataValue(t, firstText, "body_utf8_bytes"))
	if err != nil {
		t.Fatal(err)
	}
	return firstText, map[string]any{
		"version": overviewDeliveryReceiptV1, "body_sha256": overviewMetadataValue(t, firstText, "body_sha256"),
		"body_bytes": bodyBytes, "end_marker_observed": true,
	}
}

func overviewCall(t *testing.T, session *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return resText(t, result)
}

func requireAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

// 认证先到、确认后到: 认证单发时 pass 已被评出但可靠性不能闩(交付未确认),
// 随后仅带确认的一次调用必须把先前的 pass 接上并闩住 level 4。
func TestOverviewAttestationThenConfirmationLatchesReliability(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	_, confirmation := deliverLegacyOverview(t, session)

	attestOnly := overviewCall(t, session, map[string]any{
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	})
	requireAll(t, attestOnly,
		"model_attestation: pass", "delivery_integrity: unconfirmed",
		"model_full_cognition_reliable: false", "completed: false")

	confirmOnly := overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})
	requireAll(t, confirmOnly,
		"host_delivery_status: host_delivery_confirmed", "model_attestation: pass",
		"cognition_assimilation: complete", "cognition_level: 4",
		"model_full_cognition_reliable: true", "completed: true",
		"refresh_status: refresh_not_required")

	// 会话态确实闩住了: 廉价 check_only 不再要求召回。
	check := overviewCall(t, session, map[string]any{"check_only": true})
	requireAll(t, check, `"refresh_status":"refresh_not_required"`, `"model_full_cognition_reliable":true`)
}

// 确认先到、认证后到: 与上一顺序对称。
func TestOverviewConfirmationThenAttestationLatchesReliability(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	_, confirmation := deliverLegacyOverview(t, session)

	confirmOnly := overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})
	requireAll(t, confirmOnly,
		"host_delivery_status: host_delivery_confirmed", "model_attestation: not_provided",
		"cognition_level: 2", "model_full_cognition_reliable: false")

	attestOnly := overviewCall(t, session, map[string]any{
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	})
	requireAll(t, attestOnly,
		"delivery_integrity: confirmed", "model_attestation: pass",
		"cognition_level: 4", "model_full_cognition_reliable: true", "completed: true")
}

func TestVolumeOverviewCombinedAndSplitProofRemainEquivalent(t *testing.T) {
	for _, test := range []struct {
		name  string
		order []string
	}{
		{name: "combined", order: []string{"combined"}},
		{name: "confirmation then attestation", order: []string{"confirmation", "attestation"}},
		{name: "attestation then confirmation", order: []string{"attestation", "confirmation"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildObservedVolumeRepo(t)
			session := connectMCPClient(t, root)
			first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": "all"})
			proof := volumeAttestationArguments(t, root, first, false)

			output := ""
			for index, step := range test.order {
				arguments := map[string]any{"scope": "all"}
				switch step {
				case "combined":
					arguments = proof
				case "confirmation":
					arguments["host_delivery_confirmation"] = proof["host_delivery_confirmation"]
				case "attestation":
					arguments["model_cognition_attestation"] = proof["model_cognition_attestation"]
				}
				output = callVolumeTool(t, session, "aoci_overview", arguments)
				if len(test.order) == 2 && index == 0 {
					requireAll(t, output, "model_full_cognition_reliable: false")
					if step == "confirmation" {
						requireAll(t, output, "delivery_integrity: confirmed", "model_attestation: not_provided")
					} else {
						requireAll(t, output, "delivery_integrity: unconfirmed", "model_attestation: pass")
					}
				}
			}
			requireAll(t, output,
				"delivery_integrity: confirmed", "model_attestation: pass",
				"cognition_assimilation: complete", "cognition_level: 4",
				"model_full_cognition_reliable: true", "completed: true")
		})
	}
}

// 每次交付尝试各自取证: 一次全新的完整交付之后, 旧确认不再为新传输作证,
// 单发的认证回到 unconfirmed; 补上确认后才闩住。
func TestOverviewFreshDeliveryResetsRememberedEvidence(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	_, confirmation := deliverLegacyOverview(t, session)
	overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})

	// 新一轮完整交付(无字段、无游标)。
	deliverLegacyOverview(t, session)
	attestOnly := overviewCall(t, session, map[string]any{
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	})
	requireAll(t, attestOnly, "model_attestation: pass", "delivery_integrity: unconfirmed",
		"model_full_cognition_reliable: false")

	confirmed := overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})
	requireAll(t, confirmed, "model_full_cognition_reliable: true")
}

// 明确携带的一半以最新为准: 已闩住之后, 一份不匹配的确认表示宿主现在说
// "没收全", 它压过记忆里的旧确认, 本次调用如实报告 incomplete 与 level 1。
func TestOverviewExplicitMismatchedConfirmationOverridesMemory(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	_, confirmation := deliverLegacyOverview(t, session)
	latched := overviewCall(t, session, map[string]any{
		"host_delivery_confirmation":  confirmation,
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	})
	requireAll(t, latched, "model_full_cognition_reliable: true")

	confirmation["body_sha256"] = strings.Repeat("0", 64)
	mismatched := overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})
	requireAll(t, mismatched, "host_delivery_status: host_delivery_incomplete", "cognition_level: 1")
}

// F 语义匹配的分词对 CJK 用字符二元组, 使同一相似度门槛对中文 Entry 同样成立。
func TestOverviewCoreFMatchesAcrossScripts(t *testing.T) {
	cases := []struct {
		answer, target string
		want           bool
	}{
		{"Parses Legacy Entry sections and F/R/A/S fields into deterministic records",
			"Parses legacy entry sections and FRAS fields into deterministic records", true},
		{"Installs an idempotent repository-local Codex MCP configuration table",
			"Installs and idempotently merges project-scoped AOCI MCP and Claude PreToolUse hook configuration", false},
		{"解析条目并校验字段", "解析条目, 校验字段", true},
		{"校验标签字典", "解析条目并校验字段", false},
		{"", "anything", false},
	}
	for _, c := range cases {
		if got := overviewCoreFMatches(c.answer, c.target); got != c.want {
			t.Fatalf("overviewCoreFMatches(%q, %q) = %t, want %t", c.answer, c.target, got, c.want)
		}
	}
}
