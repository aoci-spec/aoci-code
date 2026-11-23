package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// Pi reads project MCP servers from .pi/mcp.json (#209). Installing merges
// the aoci server into that file, keeps the user's other servers, is
// idempotent, writes nothing else, and refuses a file that is not JSON.
func TestInstallPiMergesProjectMCPFile(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		t.Run(locale, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
			if err := textassets.SetActiveLocale(locale); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path := filepath.Join(root, ".pi", "mcp.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"}},"keep":1}`), 0o644); err != nil {
				t.Fatal(err)
			}
			message, err := Install(root, "pi", true)
			if err != nil || strings.Contains(message, "[text asset") {
				t.Fatalf("install: %v %q", err, message)
			}
			var doc struct {
				MCPServers map[string]struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				} `json:"mcpServers"`
				Keep int `json:"keep"`
			}
			raw, _ := os.ReadFile(path)
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			aoci := doc.MCPServers["aoci"]
			if doc.Keep != 1 || doc.MCPServers["other"].Command != "x" || aoci.Command == "" ||
				len(aoci.Args) != 3 || aoci.Args[0] != "--repo" || aoci.Args[2] != "mcp" {
				t.Fatalf("merged file lost content or has the wrong server: %s", raw)
			}
			if !IsPiMCPInstalled(root) || IsClaudeMCPInstalled(root) {
				t.Fatal("pi install must write .pi/mcp.json and not .mcp.json")
			}
			for _, other := range []string{".mcp.json", ".claude", ".codex"} {
				if _, err := os.Lstat(filepath.Join(root, other)); !os.IsNotExist(err) {
					t.Fatalf("pi install wrote %s", other)
				}
			}
			again, err := Install(root, "pi", false)
			after, _ := os.ReadFile(path)
			if err != nil || string(after) != string(raw) || again == message {
				t.Fatalf("second install must be an unchanged no-op: %v %q", err, again)
			}
		})
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pi", "mcp.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "pi", false); err == nil {
		t.Fatal("a .pi/mcp.json that is not JSON must be refused, not overwritten")
	}
	if raw, _ := os.ReadFile(filepath.Join(root, ".pi", "mcp.json")); string(raw) != "{not json" {
		t.Fatal("the invalid file must be left as it was")
	}
}

func TestDetectRecognizesPi(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	if containsString(Detect(root), "pi") {
		t.Fatal("a bare repository must not report pi")
	}
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !containsString(Detect(root), "pi") {
		t.Fatalf("a project .pi directory must report pi: %v", Detect(root))
	}
}

// A symlinked .pi directory or a symlinked .mcp.json would send the merge,
// its backup, and the rewrite outside the repository: both are refused and
// nothing outside changes.
func TestMCPServersInstallRefusesSymlinkedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs a privilege on Windows")
	}
	outside := t.TempDir()
	foreign := filepath.Join(outside, "mcp.json")
	if err := os.WriteFile(foreign, []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		agent, link, target string
		hooks               bool
	}{
		{"pi", ".pi", outside, false},
		{"qoder", ".mcp.json", foreign, false},
		{"codex", ".codex", outside, true},
		{"claude", ".claude", outside, true},
	} {
		t.Run(test.agent, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Symlink(test.target, filepath.Join(root, test.link)); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(root, test.agent, test.hooks); err == nil {
				t.Fatal("a symlinked host configuration path must be refused")
			}
			entries, _ := os.ReadDir(outside)
			raw, _ := os.ReadFile(foreign)
			if len(entries) != 1 || string(raw) != `{"mcpServers":{"other":{"command":"x"}}}` {
				t.Fatalf("nothing outside the repository may change: %d entries, %s", len(entries), raw)
			}
		})
	}
}
