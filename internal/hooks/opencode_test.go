package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func readOpenCodeTestObject(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("written opencode.json is invalid: %v\n%s", err, raw)
	}
	return object
}

func TestInstallOpenCodeMCPCreateV1WithAbsolutePaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	message, err := InstallOpenCodeMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "已创建") || !strings.Contains(message, "若已显示可直接继续，无需重启") {
		t.Fatalf("create output lost result or conditional host-loading guidance: %q", message)
	}

	document := readOpenCodeTestObject(t, root)
	if document["$schema"] != openCodeSchemaURL {
		t.Fatalf("schema mismatch: %#v", document["$schema"])
	}
	mcp, ok := document["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("mcp must be object: %#v", document)
	}
	aoci, ok := mcp["aoci"].(map[string]any)
	if !ok || aoci["type"] != "local" || aoci["enabled"] != true {
		t.Fatalf("aoci V1 server mismatch: %#v", aoci)
	}
	command, ok := aoci["command"].([]any)
	if !ok || len(command) != 4 || command[1] != "--repo" || command[3] != "mcp" {
		t.Fatalf("command mismatch: %#v", aoci["command"])
	}
	for _, index := range []int{0, 2} {
		path, ok := command[index].(string)
		if !ok || !filepath.IsAbs(filepath.FromSlash(path)) {
			t.Fatalf("command[%d] must be an absolute stable path: %#v", index, command[index])
		}
	}
	if command[2] != toSlash(root) {
		t.Fatalf("repository path mismatch: got=%v want=%s", command[2], toSlash(root))
	}
	if !IsOpenCodeMCPInstalled(root) {
		t.Fatal("real installer output must be recognized as installed")
	}
}

func TestInstallOpenCodeMCPMergePreservesExistingKeysAndServers(t *testing.T) {
	root := t.TempDir()
	existing := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "provider/model",
  "mcp": {
    "other": {"type": "remote", "url": "https://example.invalid/mcp"}
  },
  "permission": {"bash": "ask"}
}
`
	writeFile(t, root, "opencode.json", existing)
	message, err := InstallOpenCodeMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "已合并") {
		t.Fatalf("expected merge message: %q", message)
	}
	document := readOpenCodeTestObject(t, root)
	if document["model"] != "provider/model" || document["permission"] == nil {
		t.Fatalf("top-level keys were not preserved: %#v", document)
	}
	mcp := document["mcp"].(map[string]any)
	if mcp["other"] == nil || mcp["aoci"] == nil {
		t.Fatalf("existing and aoci servers must coexist: %#v", mcp)
	}
}

func TestInstallOpenCodeMCPExactConfigIsByteAndInodeNoOp(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallOpenCodeMCP(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "opencode.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	message, err := InstallOpenCodeMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	afterInfo, _ := os.Stat(path)
	if !strings.Contains(message, "跳过") || !reflect.DeepEqual(before, after) || !os.SameFile(beforeInfo, afterInfo) {
		t.Fatalf("exact configuration must be a real no-op: message=%q same_bytes=%t same_file=%t", message, reflect.DeepEqual(before, after), os.SameFile(beforeInfo, afterInfo))
	}
	if backups, _ := filepath.Glob(path + ".backup.*"); len(backups) != 0 {
		t.Fatalf("no-op must not create backups: %v", backups)
	}
}

func TestPrepareOpenCodeMCPFailsClosedForUnsupportedOrConflictingFiles(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{name: "malformed JSON", file: "opencode.json", content: `{"mcp": // comment`, want: "不是严格合法"},
		{name: "duplicate JSON key", file: "opencode.json", content: `{"mcp":{},"mcp":{}}`, want: "重复"},
		{name: "JSONC", file: "opencode.jsonc", content: `{"mcp": {}}`, want: "opencode.jsonc"},
		{name: "V2", file: "opencode.json", content: `{"mcp":{"servers":{"other":{"type":"local","command":["x"]}}}}`, want: "V2"},
		{name: "mcpServers ambiguity", file: "opencode.json", content: `{"mcpServers":{"aoci":{"command":"x"}}}`, want: "mcpServers"},
		{name: "mcp not object", file: "opencode.json", content: `{"mcp":[]}`, want: "mcp"},
		{name: "conflicting aoci", file: "opencode.json", content: `{"mcp":{"aoci":{"type":"local","command":["old","mcp"],"enabled":true}}}`, want: "不完全一致"},
		{name: "unsupported schema", file: "opencode.json", content: `{"$schema":"https://example.invalid/config.json","mcp":{}}`, want: "$schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, test.file, test.content)
			path := filepath.Join(root, test.file)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := InstallOpenCodeMCP(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected fail-closed error containing %q, got %v", test.want, err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unsupported/conflicting input was overwritten")
			}
			if backups, _ := filepath.Glob(path + ".backup.*"); len(backups) != 0 {
				t.Fatalf("rejected input must not create backups: %v", backups)
			}
			if test.file != "opencode.json" {
				if _, err := os.Stat(filepath.Join(root, "opencode.json")); !os.IsNotExist(err) {
					t.Fatalf("JSONC rejection must not create opencode.json: %v", err)
				}
			}
		})
	}
}

func TestPrepareOpenCodeMCPRejectsSymlinkTargetWithoutTouchingReferent(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.json")
	original := []byte(`{"external":true}`)
	if err := os.WriteFile(external, original, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "opencode.json")
	if err := os.Symlink(external, path); err != nil {
		t.Skipf("symlinks are unavailable in this test environment: %v", err)
	}
	if _, err := InstallOpenCodeMCP(root); err == nil || !strings.Contains(err.Error(), "unsafe_target_type") {
		t.Fatalf("symlink target must fail closed: %v", err)
	}
	after, err := os.ReadFile(external)
	if err != nil || !reflect.DeepEqual(after, original) {
		t.Fatalf("symlink referent changed: data=%q err=%v", after, err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink target was replaced: info=%v err=%v", info, err)
	}
}

func TestOpenCodeAOCIServerNormalizesWindowsAndPOSIXPaths(t *testing.T) {
	windows := openCodeAOCIServer(`C:\Program Files\AOCI\aoci.exe`, `D:\Work Trees\shop`)
	wantWindows := []any{"C:/Program Files/AOCI/aoci.exe", "--repo", "D:/Work Trees/shop", "mcp"}
	if !reflect.DeepEqual(windows["command"], wantWindows) {
		t.Fatalf("Windows command path was not normalized safely: %#v", windows["command"])
	}
	posix := openCodeAOCIServer("/opt/aoci/aoci", "/srv/project with spaces")
	wantPOSIX := []any{"/opt/aoci/aoci", "--repo", "/srv/project with spaces", "mcp"}
	if !reflect.DeepEqual(posix["command"], wantPOSIX) {
		t.Fatalf("POSIX command path changed unexpectedly: %#v", posix["command"])
	}
}

func TestDetectAndStatusRecognizeOpenCodeInstallerOutput(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallOpenCodeMCP(root); err != nil {
		t.Fatal(err)
	}
	if !containsString(Detect(root), "opencode") {
		t.Fatalf("Detect did not report opencode: %v", Detect(root))
	}
	if !IsOpenCodeMCPInstalled(root) {
		t.Fatal("status did not recognize exact installer output")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestOpenCodeHostLoadGuidanceIsConditionalInEveryLocale(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		message := hookMessage("hook.opencode_host_load")
		for _, required := range []string{"opencode.json", "aoci_"} {
			if !strings.Contains(message, required) {
				t.Fatalf("%s guidance missing %q: %q", locale, required, message)
			}
		}
		if locale == textassets.DefaultLocale {
			if !strings.Contains(message, "If they are present") || !strings.Contains(message, "otherwise") {
				t.Fatalf("English guidance must make refresh conditional: %q", message)
			}
		} else if !strings.Contains(message, "若已显示") || !strings.Contains(message, "若未显示") {
			t.Fatalf("Chinese guidance must make refresh conditional: %q", message)
		}
	}
}
