package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The listing shows cognition_receipt and probe_answers as plain objects; the
// registration keeps the closed shapes, so the SDK still refuses a call that
// breaks either one, and listing twice hands out the same compact view.
func TestToolListingAdvertisesCompactOverviewMembersWhileCallsStayClosed(t *testing.T) {
	root := buildRepo(t)
	session := connectMCPClient(t, root)
	ctx := context.Background()

	overviewProperties := func() map[string]any {
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range listed.Tools {
			if tool.Name != "aoci_overview" {
				continue
			}
			data, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			return schema.Properties
		}
		t.Fatal("aoci_overview is missing from tools/list")
		return nil
	}
	for round := 0; round < 2; round++ {
		properties := overviewProperties()
		receipt, _ := json.Marshal(properties["cognition_receipt"])
		if strings.Contains(string(receipt), "runtime_repository_root") || !strings.Contains(string(receipt), `{"type":"object"}`) {
			t.Fatalf("round %d: cognition_receipt is not listed as a plain object: %s", round, receipt)
		}
		probe, _ := json.Marshal(properties["probe_answers"])
		if strings.Contains(string(probe), "answers") || !strings.Contains(string(probe), `"type":"object"`) {
			t.Fatalf("round %d: probe_answers is not listed as a plain object: %s", round, probe)
		}
		attestation, _ := json.Marshal(properties["model_cognition_attestation"])
		if !strings.Contains(string(attestation), "challenge_answers") {
			t.Fatalf("round %d: the attestation must keep its listed structure: %s", round, attestation)
		}
	}

	for name, arguments := range map[string]map[string]any{
		"unknown receipt field": {"check_only": true, "cognition_receipt": map[string]any{"bogus": 1}},
		"receipt wrong type":    {"check_only": true, "cognition_receipt": map[string]any{"version": "2"}},
		"probe wrong version":   {"check_only": true, "probe_answers": map[string]any{"version": "x"}},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "aoci_overview", Arguments: arguments})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := ""
		for _, content := range result.Content {
			if typed, ok := content.(*mcp.TextContent); ok {
				text += typed.Text
			}
		}
		if !result.IsError || !strings.Contains(text, `validating "arguments"`) {
			t.Fatalf("%s: the closed schema no longer refuses the call: error=%v %s", name, result.IsError, text)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{"check_only": true, "cognition_receipt": nil}})
	if err != nil || result.IsError {
		t.Fatalf("a null receipt must stay accepted: %v %+v", err, result)
	}
}
