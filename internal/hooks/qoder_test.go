package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// A project with a .qoder directory (rules or settings.local.json) is a Qoder
// environment; the user-level ~/.qoder directory counts too, the way ~/.claude
// does for Claude Code.
func TestDetectRecognizesQoderProjectDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	if containsString(Detect(root), "qoder") {
		t.Fatalf("a bare repository must not report qoder: %v", Detect(root))
	}
	if err := os.MkdirAll(filepath.Join(root, ".qoder", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !containsString(Detect(root), "qoder") {
		t.Fatalf("Detect did not report qoder for a project .qoder directory: %v", Detect(root))
	}
}

// The user-level ~/.qoder directory alone reports qoder for an empty project.
// HOME and USERPROFILE both point at the fixture home so os.UserHomeDir finds
// it on every platform and the developer's real home never leaks in.
func TestDetectRecognizesUserLevelQoderDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".qoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	if !containsString(Detect(root), "qoder") {
		t.Fatalf("Detect did not report qoder for a user-level ~/.qoder directory: %v", Detect(root))
	}
}

// Installing for qoder writes the shared project-level .mcp.json and says so;
// the Claude Code installer's idempotence check then reports it as installed.
func TestInstallQoderWritesSharedMCPJSON(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	message, err := Install(root, "qoder", true)
	if err != nil {
		t.Fatal(err)
	}
	if !IsClaudeMCPInstalled(root) {
		t.Fatalf("qoder install must leave the shared .mcp.json in place: %s", message)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("qoder has no hook surface; nothing under .claude may be written: %v", err)
	}
	hint := hookMessage("hook.qoder_shares_mcp_json")
	if strings.Contains(hint, "[text asset") {
		t.Fatalf("hook.qoder_shares_mcp_json is missing from the %s catalog: %s", textassets.DefaultLocale, hint)
	}
	if !strings.Contains(message, hint) {
		t.Fatalf("qoder install must say Qoder reads the shared .mcp.json: %q", message)
	}
	if strings.Contains(message, "[text asset") {
		t.Fatalf("qoder install message carries a missing text asset: %q", message)
	}
}
