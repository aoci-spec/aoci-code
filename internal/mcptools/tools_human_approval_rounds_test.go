package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// On the sessionless revision the dialog is a round of its own: the first
// call returns the form as an input request and writes nothing, and the
// answer counts only beside the request state that round issued. A forged
// state, or an accepted form without one of the two decisions, records
// nothing; the issued state with an approval records exactly what the
// terminal approval would.
func TestHumanApprovalSessionlessRoundsBindTheAnswerToTheDialog(t *testing.T) {
	root, previewRel, preview := pendingScopeApprovalRepo(t)
	approvalPath := filepath.Join(root, ".aoci", "scope-change", "pending", "approval.json")
	raw := newRawMCP(t, root)
	meta := sessionlessMeta(machinecontract.MCPProtocolSessionless, map[string]any{"elicitation": map[string]any{"form": map[string]any{}}})
	call := func(extra map[string]any) map[string]any {
		t.Helper()
		params := map[string]any{"name": "aoci_maintain", "arguments": scopeApprovalArguments(previewRel), "_meta": meta}
		for key, value := range extra {
			params[key] = value
		}
		result, wire := raw.call("tools/call", params)
		if wire != nil {
			t.Fatalf("tools/call failed: %v", wire)
		}
		return result
	}
	outcome := func(result map[string]any) map[string]any {
		t.Helper()
		content, _ := result["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("expected one text result: %v", result)
		}
		text, _ := content[0].(map[string]any)["text"].(string)
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Fatalf("result is not JSON: %v %s", err, text)
		}
		return decoded
	}
	noApproval := func(stage string) {
		t.Helper()
		if _, err := os.Stat(approvalPath); !os.IsNotExist(err) {
			t.Fatalf("%s left an approval: %v", stage, err)
		}
	}

	first := call(nil)
	requests, _ := first["inputRequests"].(map[string]any)
	form, _ := requests[humanApprovalInputKey].(map[string]any)
	params, _ := form["params"].(map[string]any)
	state, _ := first["requestState"].(string)
	message, _ := params["message"].(string)
	if first["resultType"] != "input_required" || form["method"] != "elicitation/create" || params["mode"] != "form" ||
		!strings.Contains(message, preview.Plan.PlanID) || !strings.HasPrefix(state, "v1:") || first["content"] != nil {
		t.Fatalf("the first round is not the dialog: %v", first)
	}
	noApproval("the dialog round")

	approve := map[string]any{humanApprovalInputKey: map[string]any{"action": "accept", "content": map[string]any{"decision": "approve"}}}
	forged := outcome(call(map[string]any{"inputResponses": approve, "requestState": "v1:" + strings.Repeat("0", 64)}))
	if forged["status"] != machinecontract.HumanApprovalStatusStale || forged["next_action"] != machinecontract.HumanApprovalActionAskAgain {
		t.Fatalf("an answer beside a forged state counted: %v", forged)
	}
	noApproval("a forged state")

	undecided := outcome(call(map[string]any{"requestState": state, "inputResponses": map[string]any{
		humanApprovalInputKey: map[string]any{"action": "accept", "content": map[string]any{"decision": "maybe"}}}}))
	if undecided["status"] != machinecontract.HumanApprovalStatusDismissed {
		t.Fatalf("an accepted form without a decision was read as one: %v", undecided)
	}
	noApproval("an undecided form")

	approved := outcome(call(map[string]any{"inputResponses": approve, "requestState": state}))
	if approved["status"] != machinecontract.HumanApprovalStatusApproved {
		t.Fatalf("the issued state with an approval was not recorded: %v", approved)
	}
	data, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := scopechange.DecodeApproval(data)
	if err != nil || !scopechange.ApprovalBinds(preview, approval) || approval.Actor != "host-dialog@raw-mcp-test" {
		t.Fatalf("recorded approval %#v %v", approval, err)
	}
}
