package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// check and verify record that they ran in result and their verdict in
// check_passed, so a failing check no longer reads as result=ok beside
// exit_code=1 (#150).
func TestCheckAndVerifyLedgerSeparateRunningFromPassing(t *testing.T) {
	root, cfg := alignedVolumeCLIFixture(t, true, false)
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"check", "verify"} {
		if _, stderr, code := runCLI(t, root, "--json", command); code != ExitOK {
			t.Fatalf("aligned %s exit=%d %s", command, code, stderr)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"check", "verify"} {
		if _, _, code := runCLI(t, root, "--json", command); code == ExitOK {
			t.Fatalf("drifted %s passed", command)
		}
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("corrupt Ledger lines: %d", corrupt)
	}
	seen := []ledger.Event{}
	for _, event := range events {
		if event.Op == "check" || event.Op == "verify" {
			seen = append(seen, event)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("check and verify events: %#v", seen)
	}
	for index, event := range seen {
		passed := index < 2
		if event.Result != ledger.ResultOK || event.CheckPassed == nil || *event.CheckPassed != passed ||
			event.ExitCode == nil || (*event.ExitCode == ExitOK) != passed || event.GovernanceResult == "" || event.NextAction == "" {
			t.Fatalf("event %d does not separate running from passing: %#v", index, event)
		}
		if !passed && event.GovernanceResult != volumegovernance.ResultAuthoringRequired {
			t.Fatalf("a drifted source is not recorded as authoring work: %#v", event)
		}
	}
}
