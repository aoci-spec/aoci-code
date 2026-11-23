package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Under a review posture, scope acknowledge pauses with a saved preview and
// the commands to approve and apply it, as scope activate does; calling Apply
// without an approval could only fail (audit G4-14).
func TestScopeAcknowledgePausesUnderReviewPosture(t *testing.T) {
	root := scopeActivationFixtureWith(t, map[string]string{"sample_test.go": "package sample\n"})
	if _, err := runScopeCLI(t, root, "approval-mode", "review"); err != nil {
		t.Fatal(err)
	}
	_, data := activateJSON(t, root)
	var paused struct {
		Details scopeActivationApproval `json:"details"`
	}
	if err := json.Unmarshal(data, &paused); err != nil || paused.Details.PreviewFile == "" {
		t.Fatalf("review posture activation did not pause: %s", data)
	}
	approvalFile := filepath.Join(filepath.Dir(paused.Details.PreviewFile), "approval.json")
	grantTTY(t)
	if _, _, err := runScopeSplit(t, root, strings.NewReader("yes\n"), true, "approve", "--preview-file", paused.Details.PreviewFile, "--actor", "tester", "--out-file", approvalFile); err != nil {
		t.Fatal(err)
	}
	if out, _, err := runScopeSplit(t, root, nil, true, "apply", "--preview-file", paused.Details.PreviewFile, "--approval-file", approvalFile); err != nil {
		t.Fatalf("apply review posture: %v %s", err, out)
	}

	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package sample\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs strings.Builder
	code := executeCLI([]string{"--repo", root, "--json", "scope", "acknowledge", "--reviewed-by", "tester"}, &out, &errs)
	var response struct {
		ErrorCode string                  `json:"error_code"`
		Details   scopeActivationApproval `json:"details"`
	}
	if err := json.Unmarshal([]byte(out.String()), &response); err != nil {
		t.Fatalf("acknowledge output: code=%d %s %s", code, &out, &errs)
	}
	if code != 2 || response.ErrorCode != "managed_scope_human_approval_required" || response.Details.PreviewFile == "" {
		t.Fatalf("acknowledge under review posture did not pause with a preview: code=%d %s", code, &out)
	}
	if _, err := os.Stat(response.Details.PreviewFile); err != nil {
		t.Fatalf("paused preview not saved: %v", err)
	}
}
