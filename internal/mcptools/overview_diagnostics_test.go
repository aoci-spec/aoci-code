package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func overviewProofEvents(t *testing.T, root string) []ledger.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".aoci/ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var events []ledger.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event ledger.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(event.Op, "overview_") {
			events = append(events, event)
		}
	}
	return events
}

func TestAttestationSchemaBoundaryDiagnosticsAndScoringStates(t *testing.T) {
	for _, key := range []string{"object", "object_ref", "object_identity"} {
		t.Run(key, func(t *testing.T) {
			root := buildObservedVolumeRepo(t)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.LedgerEnabled = true
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			session := connectMCPClient(t, root)
			first := callVolumeTool(t, session, "aoci_overview", map[string]any{})
			args := volumeAttestationArguments(t, root, first, false)
			report := args["model_cognition_attestation"].(map[string]any)
			if key != "object_identity" {
				for _, value := range report["challenge_answers"].([]any) {
					answer := value.(map[string]any)
					answer[key] = answer["object_identity"]
					delete(answer, "object_identity")
				}
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			events := overviewProofEvents(t, root)
			ops := map[string]int{}
			for _, event := range events {
				ops[event.Op+":"+event.Result]++
			}
			if ops["overview_delivery_completed:completed"] != 1 {
				t.Fatal("delivery completion not distinct")
			}
			if key != "object_identity" {
				if !result.IsError || !strings.Contains(resText(t, result), "object_identity") || !strings.Contains(resText(t, result), "additionalProperties=false") {
					t.Fatalf("missing precise schema diagnostic: %s", resText(t, result))
				}
				if ops["overview_attestation_schema_rejected:schema_rejected"] != 1 || ops["overview_attestation_scoring_started:started"] != 0 || ops["overview_attestation_schema_accepted:accepted"] != 0 || ops["overview_attestation_scored:fail"] != 0 {
					t.Fatalf("schema error reached scorer: %v", events)
				}
				if strings.Contains(resText(t, result), "cognition_usable") || strings.Contains(resText(t, result), "model_attestation: fail") {
					t.Fatal("schema rejection claimed semantic cognition state")
				}
			} else {
				if result.IsError || !strings.Contains(resText(t, result), "model_attestation: pass") {
					t.Fatalf("correct schema failed: %s", resText(t, result))
				}
				for _, op := range []string{"overview_host_confirmation_accepted:accepted", "overview_attestation_schema_accepted:accepted", "overview_attestation_scoring_started:started", "overview_attestation_scored:pass"} {
					if ops[op] != 1 {
						t.Fatalf("missing %s: %v", op, events)
					}
				}
				// A schema-valid identity error reaches the unchanged semantic scorer.
				for _, value := range report["challenge_answers"].([]any) {
					value.(map[string]any)["object_identity"] = "code:wrong.go"
				}
				result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError || !strings.Contains(resText(t, result), "model_attestation: fail") {
					t.Fatal("semantic failure not reported distinctly")
				}
				events = overviewProofEvents(t, root)
				if events[len(events)-2].Op == "overview_attestation_schema_rejected" {
					t.Fatal("semantic failure misclassified")
				}
				found := false
				for _, event := range events {
					if event.Op == "overview_attestation_scored" && event.Result == "fail" {
						found = true
					}
				}
				if !found {
					t.Fatal("semantic failure event missing")
				}
			}
		})
	}
}
