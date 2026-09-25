package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An MCP write receipt left under .aoci/transactions keeps the read-only
// surfaces open, refuses writes, and the Guide names both the file and the
// tool that closes it. Until v0.1.0-rc15 the CLI ignored such receipts while
// Overview refused on them, and nothing told the operator which file (#74).
func TestMCPWriteReceiptsKeepReadOnlyCommandsOpenAndGuideNamesTheClosure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := executeCLI(append([]string{"--repo", root}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, out, errs := run("init", "--locale", "en-US"); code != ExitOK {
		t.Fatalf("init: %d %s %s", code, out, errs)
	}
	if code, out, errs := run("scan", "--quiet"); code != ExitOK {
		t.Fatalf("scan: %d %s %s", code, out, errs)
	}
	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "remove-deadbeef.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, out, errs := run("verify", "--json")
	var report struct {
		Governance struct {
			RecoveryPending bool     `json:"recovery_pending"`
			Files           []string `json:"pending_transaction_files"`
		} `json:"governance"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("verify must stay available and answer JSON: %v\n%s\n%s", err, out, errs)
	}
	if !report.Governance.RecoveryPending || strings.Join(report.Governance.Files, ",") != "remove-deadbeef.json" {
		t.Fatalf("verify must name the receipt: %+v", report.Governance)
	}

	if code, out, errs := run("--json", "scan"); code != ExitInvalid || !strings.Contains(out+errs, "cognition_recovery_pending") ||
		!strings.Contains(out+errs, "remove-deadbeef.json") {
		t.Fatalf("a write must wait for the receipt and say which one: %d %s %s", code, out, errs)
	}

	_, out, errs = run("index", "agent", "guide", "--agent", "codex", "--json")
	var guide struct {
		Complete bool `json:"complete"`
		Stop     *struct {
			AffectedAsset  string `json:"affected_asset"`
			RuleCode       string `json:"rule_code"`
			SafeNextAction string `json:"safe_next_action"`
		} `json:"stop"`
	}
	if err := json.Unmarshal([]byte(out), &guide); err != nil {
		t.Fatalf("guide must stay available and answer JSON: %v\n%s\n%s", err, out, errs)
	}
	if guide.Complete || guide.Stop == nil || guide.Stop.RuleCode != "recovery_pending" ||
		guide.Stop.AffectedAsset != ".aoci/transactions/remove-deadbeef.json" ||
		!strings.Contains(guide.Stop.SafeNextAction, "aoci_remove_entry") {
		t.Fatalf("guide must name the receipt and its closure: %+v", guide.Stop)
	}

	// The read-only surfaces and the human closure stay open; a foreign file
	// is listed beside the receipt, each with its own closure.
	for _, args := range [][]string{{"--json", "check"}, {"--json", "status"}, {"--json", "scope", "status"},
		{"--json", "remove-entry", "--path", "main.go"}} {
		if _, out, errs := run(args...); strings.Contains(out+errs, "cognition_recovery_pending") {
			t.Fatalf("%v must not be gated: %s %s", args, out, errs)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "note.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, errs = run("verify", "--json")
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("verify: %v\n%s\n%s", err, out, errs)
	}
	if strings.Join(report.Governance.Files, ",") != "note.json,remove-deadbeef.json" {
		t.Fatalf("verify must list every receipt: %+v", report.Governance)
	}
	_, out, _ = run("index", "agent", "guide", "--agent", "codex", "--json")
	guide.Stop = nil
	if err := json.Unmarshal([]byte(out), &guide); err != nil || guide.Stop == nil ||
		!strings.Contains(guide.Stop.SafeNextAction, "note.json") || !strings.Contains(guide.Stop.SafeNextAction, "by hand") ||
		!strings.Contains(guide.Stop.SafeNextAction, "aoci_remove_entry") {
		t.Fatalf("guide must name both receipts with their closures: err=%v stop=%+v", err, guide.Stop)
	}
}

// Every receipt kind, and a kind nobody wrote, resolves to a closure that
// names the file; a missing catalogue key would surface as a text-asset error.
func TestRecoveryPendingActionNamesEveryKind(t *testing.T) {
	for _, kind := range []string{"database-bootstrap", "bootstrap", "migration", "reversal", "scope", "remove", "entries", "header", "unknown", "never-written"} {
		action := recoveryPendingAction(kind, kind+"-x.json")
		if !strings.Contains(action, kind+"-x.json") || strings.Contains(action, "text_asset_error") {
			t.Fatalf("%s: %q", kind, action)
		}
	}
}

// With receipts of several kinds pending, only the commands every kind allows
// run: a layout receipt beside a remove receipt keeps verify closed.
func TestRecoveryGateTakesTheNarrowestKind(t *testing.T) {
	if !recoveryGateAllows("remove", "aoci verify") || recoveryGateAllows("bootstrap", "aoci verify") ||
		!recoveryGateAllows("bootstrap", "aoci index agent guide") || !recoveryGateAllows("remove", "aoci remove-entry") ||
		recoveryGateAllows("entries", "aoci remove-entry") || !recoveryGateAllows("header", "aoci index header apply") ||
		recoveryGateAllows("unknown", "aoci scan") || !recoveryGateAllows("unknown", "aoci check") {
		t.Fatal("per-kind allowances changed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		code := executeCLI(append([]string{"--repo", root}, args...), &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	if code, out := run("--quiet", "init", "--locale", "en-US"); code != ExitOK {
		t.Fatalf("init: %d %s", code, out)
	}
	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"remove-deadbeef.json", "bootstrap-1.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code, out := run("--json", "verify"); code != ExitInvalid || !strings.Contains(out, "cognition_recovery_pending") {
		t.Fatalf("a layout receipt beside a remove receipt must keep verify closed: %d %s", code, out)
	}
}
