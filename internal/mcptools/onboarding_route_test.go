package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func buildActiveFreshMCPRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, cfg.Locale, time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Next(root, 1, 1024*1024); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestActiveFreshMCPRoutesAllNineToolsWithoutAdvancingSession(t *testing.T) {
	root := buildActiveFreshMCPRepo(t)
	activePath := onboarding.SessionPath(root)
	before, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 9 {
		t.Fatalf("MCP surface changed: tools=%d err=%v", len(listed.Tools), err)
	}

	rulesResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_rules", Arguments: map[string]any{}})
	if err != nil || rulesResult.IsError {
		t.Fatalf("rules did not establish runtime contract during Fresh: result=%#v err=%v", rulesResult, err)
	}
	rulesText := resText(t, rulesResult)
	if !strings.Contains(rulesText, "AOCI_ONBOARDING_ROUTE_JSON:") || !strings.Contains(rulesText, `"status":"onboarding_in_progress"`) ||
		!strings.Contains(rulesText, `"--max-objects","1"`) ||
		!strings.Contains(rulesText, `"--max-evidence-bytes","1048576"`) || !strings.Contains(rulesText, "AOCI") {
		t.Fatalf("rules omitted runtime contract or route:\n%s", rulesText)
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{"aoci_overview", map[string]any{}},
		{"aoci_get_entries", map[string]any{"paths": []string{"main.go"}}},
		{"aoci_search", map[string]any{"keyword": "main"}},
		{"aoci_update_entry", map[string]any{"path": "main.go", "new_entry": "placeholder", "source_sha256": strings.Repeat("0", 64)}},
		{"aoci_report", map[string]any{"path": "main.go", "note": "insufficient evidence"}},
		{"aoci_remove_entry", map[string]any{"path": "main.go"}},
		{"aoci_header", map[string]any{}},
		{"aoci_maintain", map[string]any{}},
	}
	for _, current := range calls {
		t.Run(current.name, func(t *testing.T) {
			result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: current.name, Arguments: current.args})
			if callErr != nil {
				t.Fatal(callErr)
			}
			if !result.IsError {
				t.Fatalf("index-dependent tool did not stop during Fresh: %#v", result)
			}
			var route onboarding.Route
			if err := json.Unmarshal([]byte(resText(t, result)), &route); err != nil || route.Status != "onboarding_in_progress" ||
				route.FormalIndexAvailable || route.NextActionContract == nil || route.NextActionContract.Command == nil {
				t.Fatalf("tool did not return the shared route: route=%#v err=%v", route, err)
			}
		})
	}

	after, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("MCP routing advanced the active Session: err=%v", err)
	}
	for _, name := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", filepath.Join(".aoci", "baseline.json")} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("routing created formal asset %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "ledger.jsonl")); !os.IsNotExist(err) {
		data, _ := os.ReadFile(filepath.Join(root, ".aoci", "ledger.jsonl"))
		t.Fatalf("route-only MCP calls created governance history: %v\n%s", err, data)
	}
}

func TestRootAbsentWithoutSessionRetainsNotInitialized(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	_, fail := loadCognitionCtx(root)
	if fail == nil || fail.Code != errNotInitialized || fail.OnboardingRoute != nil {
		t.Fatalf("absent Session did not preserve not_initialized: %#v", fail)
	}
}

func TestActiveFreshMCPPreservesRecoveryPriority(t *testing.T) {
	root := buildActiveFreshMCPRepo(t)
	transactions := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(transactions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactions, "bootstrap-route-test.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, fail := loadCognitionCtx(root)
	if fail == nil || fail.Code != errCognitionSnapshotUnavailable || fail.OnboardingRoute != nil {
		t.Fatalf("pending Recovery was hidden by Fresh route: %#v", fail)
	}
}

func TestMCPKeepsApplyPendingFinalizationAheadOfPublishedRoot(t *testing.T) {
	root := buildActiveFreshMCPRepo(t)
	activePath := onboarding.SessionPath(root)
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted onboarding.Session
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	persisted.TransactionState = "apply_pending"
	persisted.ApprovalArtifact = ".aoci/onboarding/test/approval.json"
	persisted.EnvelopeArtifact = ".aoci/onboarding/test/apply-envelope.json"
	persisted.NextAction = "human_tty_digest_confirmation"
	persisted.Revision++
	data, err = json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(activePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("formal Root is already published\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}

	_, fail := loadCognitionCtx(root)
	if fail == nil || fail.Code != errOnboardingInProgress || fail.OnboardingRoute == nil ||
		!fail.OnboardingRoute.FormalIndexAvailable || !fail.OnboardingRoute.FormalWritesStarted ||
		fail.OnboardingRoute.NextActionContract == nil || fail.OnboardingRoute.NextActionContract.Action != "resume" {
		t.Fatalf("formal cognition hid apply-pending onboarding finalization: %#v", fail)
	}

	client := connectMCPClient(t, root)
	rules, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_rules", Arguments: map[string]any{}})
	if err != nil || rules.IsError || !strings.Contains(resText(t, rules), `"formal_index_available":true`) {
		t.Fatalf("rules did not expose terminal Fresh route: result=%#v err=%v", rules, err)
	}
	overview, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{}})
	if err != nil || !overview.IsError || !strings.Contains(resText(t, overview), `"action":"resume"`) {
		t.Fatalf("index-dependent tool bypassed terminal Fresh route: result=%#v err=%v", overview, err)
	}
	after, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("MCP finalization route changed active Session: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "ledger.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("terminal route-only MCP calls created Ledger history: %v", err)
	}
}

func TestRawMCPMalformedWritesFailClosedOnPendingRecoveryWithoutWrites(t *testing.T) {
	root := buildActiveFreshMCPRepo(t)
	formalPath := filepath.Join(root, "aoci.txt")
	recoveryPath := filepath.Join(root, ".aoci", "transactions", "bootstrap-route-test.json")
	ledgerPath := filepath.Join(root, ".aoci", "ledger.jsonl")
	if err := os.WriteFile(formalPath, []byte("formal Root sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(recoveryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recoveryPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, []byte("{\"op\":\"sentinel\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{formalPath, onboarding.SessionPath(root), recoveryPath, ledgerPath}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}

	client := connectMCPClient(t, root)
	calls := []struct {
		name string
		args map[string]any
	}{
		// These are deliberately untyped/malformed Host payloads. Recovery must
		// win before field validation or stopped-result accounting.
		{name: "aoci_update_entry", args: map[string]any{"path": "main.go"}},
		{name: "aoci_maintain", args: map[string]any{"object_refs": []string{"code:main.go"}}},
	}
	for _, call := range calls {
		result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil || result == nil || !result.IsError ||
			!strings.Contains(resText(t, result), errCognitionSnapshotUnavailable) {
			t.Fatalf("%s did not fail closed on pending Recovery: result=%#v err=%v", call.name, result, err)
		}
	}

	for _, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before[path], after) {
			t.Fatalf("malformed MCP call changed %s: err=%v\nbefore=%q\nafter=%q", path, err, before[path], after)
		}
	}
}
