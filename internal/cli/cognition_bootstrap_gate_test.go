package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapPendingRecoveryGloballyBlocksOrdinaryCLI(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(".aoci/transactions")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "transactions", "bootstrap-deadbeef.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "verify"}, &stdout, &stderr)
	if code != ExitInvalid || !strings.Contains(stdout.String(), "bootstrap_recovery_pending") {
		t.Fatalf("ordinary CLI crossed pending Bootstrap: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "--json", "cognition", "bootstrap", "status", "--transaction", "deadbeef"}, &stdout, &stderr)
	if code == ExitOK || strings.Contains(stdout.String(), "bootstrap_recovery_pending") {
		t.Fatalf("Status was blocked by the global gate: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestMigrationPendingRecoveryGloballyBlocksOrdinaryCLI(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(".aoci/transactions")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "transactions", "migration-deadbeef.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "verify"}, &stdout, &stderr)
	if code != ExitInvalid || !strings.Contains(stdout.String(), "cognition_recovery_pending") || !strings.Contains(stdout.String(), "migration-deadbeef.json") {
		t.Fatalf("ordinary CLI crossed pending Migration: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "--json", "cognition", "migration", "status", "--transaction", "deadbeef"}, &stdout, &stderr)
	if code == ExitOK || strings.Contains(stdout.String(), "cognition_recovery_pending") {
		t.Fatalf("Migration Status was blocked by the global gate: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
