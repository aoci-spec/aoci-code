package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestInitOpenCodeCreatesConfigAndRecordsInstalledAgent(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false"); err != nil {
		t.Fatalf("init --agent=opencode failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "opencode.json")); err != nil {
		t.Fatalf("opencode.json was not created: %v", err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(cfg.InstalledAgents, "opencode") {
		t.Fatalf("installed_agents did not record opencode: %v", cfg.InstalledAgents)
	}
}

func TestInitOpenCodeAdvancesExistingBaselineForOwnConfigWrite(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	seedBaseline(t, root, []string{"aoci.txt", "AGENTS.md"})
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("baseline unavailable: exists=%t err=%v", exists, err)
	}
	got, exists := state.Files["opencode.json"]
	if !exists {
		t.Fatalf("opencode.json was not captured by init Baseline advancement: %v", state.Files)
	}
	disk, err := baseline.HashFile(filepath.Join(root, "opencode.json"))
	if err != nil || disk.SHA256 != got.SHA256 {
		t.Fatalf("Baseline postimage mismatch: got=%+v disk=%+v err=%v", got, disk, err)
	}
}

func TestInitAllKeepsExistingThreeAgentSet(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=all", "--hooks=false"); err != nil {
		t.Fatalf("init --agent=all failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("--agent=all must not silently expand to OpenCode: %v", err)
	}
	for _, required := range []string{".mcp.json", ".codex/config.toml"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(required))); err != nil {
			t.Fatalf("existing all-host integration %s regressed: %v", required, err)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "codex", "cursor"}
	if !reflect.DeepEqual(cfg.InstalledAgents, want) {
		t.Fatalf("--agent=all set changed: got=%v want=%v", cfg.InstalledAgents, want)
	}
}

func snapshotInitTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			result[filepath.ToSlash(relative)+"/"] = info.Mode().String()
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = "symlink:" + target
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		result[filepath.ToSlash(relative)] = info.Mode().String() + ":" + hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInitOpenCodePreflightRejectsBeforeAnyRepositoryWrite(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		content string
	}{
		{name: "malformed", rel: "opencode.json", content: `{"mcp": // comment`},
		{name: "duplicate", rel: "opencode.json", content: `{"mcp":{},"mcp":{}}`},
		{name: "jsonc", rel: "opencode.jsonc", content: `{"mcp": {}}`},
		{name: "nested JSON", rel: ".opencode/opencode.json", content: `{"mcp": {}}`},
		{name: "nested JSONC", rel: ".opencode/opencode.jsonc", content: `{"mcp": {}}`},
		{name: "V2", rel: "opencode.json", content: `{"mcp":{"servers":{}}}`},
		{name: "conflict", rel: "opencode.json", content: `{"mcp":{"aoci":{"type":"local","command":["old"],"enabled":true}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(test.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshotInitTree(t, root)
			_, err := runInit(t, root, "--agent=opencode", "--hooks=false")
			if err == nil {
				t.Fatal("incompatible OpenCode configuration must fail")
			}
			after := snapshotInitTree(t, root)
			if !reflect.DeepEqual(before, after) {
				beforeKeys, afterKeys := make([]string, 0, len(before)), make([]string, 0, len(after))
				for key := range before {
					beforeKeys = append(beforeKeys, key)
				}
				for key := range after {
					afterKeys = append(afterKeys, key)
				}
				sort.Strings(beforeKeys)
				sort.Strings(afterKeys)
				t.Fatalf("preflight failure changed repository tree: before=%v after=%v", beforeKeys, afterKeys)
			}
			for _, forbidden := range []string{".aoci", "aoci.txt", "AGENTS.md"} {
				if _, statErr := os.Stat(filepath.Join(root, forbidden)); !os.IsNotExist(statErr) {
					t.Fatalf("preflight must run before creating %s: %v", forbidden, statErr)
				}
			}
		})
	}
}

func TestDoctorReportsOpenCodeMCPStatus(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	output, _ := runDoctor(t, root)
	if !strings.Contains(output, "[✓] OpenCode MCP（opencode.json V1）") {
		t.Fatalf("Doctor did not report the exact OpenCode integration:\n%s", output)
	}
}
