package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// The Managed Scope walk-through of 2026-11-01: a person approved "1 files
// losing Index coverage", apply said applied, and verify, check, and Maintain
// then stood blocked on an orphan nobody had mentioned.
func TestScopeApplyNamesTheOrphansARoleChangeLeaves(t *testing.T) {
	pinDefaultLocale(t)
	root := scopeActivationFixture(t)
	mustCLI(t, root, "scope", "rule", "add", "observe-main", "--action", "observe", "--pattern", "main.go",
		"--pattern-kind", "file", "--reason", "walk-through: the file leaves the Index")
	code, out := activateJSON(t, root)
	var stop struct {
		ErrorCode string `json:"error_code"`
		Details   struct {
			PreviewFile string `json:"preview_file"`
		} `json:"details"`
	}
	if err := json.Unmarshal(out, &stop); err != nil || code == ExitOK || stop.ErrorCode != "managed_scope_human_approval_required" {
		t.Fatalf("activate did not pause for a person: code=%d err=%v\n%s", code, err, out)
	}
	if stop.Details.PreviewFile == "" {
		t.Fatalf("activate stop names no preview:\n%s", out)
	}
	data, err := os.ReadFile(stop.Details.PreviewFile)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := scopechange.DecodePreview(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary := scopechange.ApprovalEffectSummary(preview); !strings.Contains(summary, "1 file losing") || strings.Contains(summary, "1 files") {
		t.Fatalf("the approval summary does not read as one file: %q", summary)
	}
	grantTTY(t)
	capturePrompt(t)
	approvalPath := filepath.Join(t.TempDir(), "approval.json")
	if _, _, err := runScopeSplit(t, root, strings.NewReader(preview.Plan.ConfirmationPhrase+"\n"), true,
		"approve", "--preview-file", stop.Details.PreviewFile, "--actor", "tester", "--out-file", approvalPath); err != nil {
		t.Fatalf("approve: %v", err)
	}
	stdout, stderr, code := runCLI(t, root, "scope", "apply", "--preview-file", stop.Details.PreviewFile, "--approval-file", approvalPath)
	if code != ExitOK {
		t.Fatalf("apply: exit %d\n%s%s", code, stdout, stderr)
	}
	for _, want := range []string{"Managed Scope applied", "1 Entry is now an orphan", "aoci remove-entry --path main.go"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("apply output does not name the orphan (%q):\n%s", want, stdout)
		}
	}
}

func TestScopeRuleCommandsNameTheirMissingArgument(t *testing.T) {
	pinDefaultLocale(t)
	root := t.TempDir()
	_, stderr, code := runCLI(t, root, "scope", "rule", "add", "--action", "observe", "--pattern", "x", "--reason", "r")
	if code == ExitOK || !strings.Contains(stderr, "rule add <rule-id>") {
		t.Fatalf("rule add without an id does not name it: code=%d\n%s", code, stderr)
	}
}
