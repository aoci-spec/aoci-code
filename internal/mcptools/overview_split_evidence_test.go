// The delivery confirmation and the model attestation are the two halves of evidence for one body, each cryptographically bound, and arrival order carries no information.
// Locked here: submitted separately and in any order, they latch the same reliable conclusion as a merged submission in one call; a
// brand-new complete delivery resets the evidence; the half explicitly carried always wins as the latest.
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

// Attestation first, confirmation later: when the attestation arrives alone, pass has been scored but reliability cannot latch (delivery unconfirmed),
// and a later call carrying only the confirmation must pick up the earlier pass and latch level 4.
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

	// The session state really latched: a cheap check_only no longer demands a recall.
	check := overviewCall(t, session, map[string]any{"check_only": true})
	requireAll(t, check, `"refresh_status":"refresh_not_required"`, `"model_full_cognition_reliable":true`)
}

// Confirmation first, attestation later: symmetric with the order above.
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

// Every delivery attempt collects its own proof: after a brand-new complete delivery, the old confirmation no longer testifies for the new transmission,
// an attestation sent alone drops back to unconfirmed, and it latches only once the confirmation is supplied.
func TestOverviewFreshDeliveryResetsRememberedEvidence(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	_, confirmation := deliverLegacyOverview(t, session)
	overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})

	// A new round of complete delivery (no fields, no cursor).
	deliverLegacyOverview(t, session)
	attestOnly := overviewCall(t, session, map[string]any{
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	})
	requireAll(t, attestOnly, "model_attestation: pass", "delivery_integrity: unconfirmed",
		"model_full_cognition_reliable: false")

	confirmed := overviewCall(t, session, map[string]any{"host_delivery_confirmation": confirmation})
	requireAll(t, confirmed, "model_full_cognition_reliable: true")
}

// The half explicitly carried wins as the latest: once latched, a mismatching confirmation means the host now says
// "not fully received", overriding the old confirmation in memory, and this call reports incomplete and level 1 truthfully.
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

// The tokenizer of the F semantic match uses character bigrams for CJK, so the same similarity threshold holds for Chinese Entries too.
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
