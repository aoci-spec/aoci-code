package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func claudeHookCommands(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	commands := []string{}
	for _, entry := range settings.Hooks.PreToolUse {
		for _, hook := range entry.Hooks {
			commands = append(commands, hook.Command)
		}
	}
	return commands
}

// The hook command is one shell word: a script path with a space is quoted,
// an entry installed unquoted on such a path is rewritten in place, and a
// plain path stays as it was (audit G5-10).
func TestClaudeHookCommandIsOneShellWord(t *testing.T) {
	spaced := filepath.Join(t.TempDir(), "my repo")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(spaced, ".aoci", "hooks", "claude-pretool.sh")
	if err := os.MkdirAll(filepath.Join(spaced, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "Edit|Write|MultiEdit", "hooks": []any{map[string]any{"type": "command", "command": script}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaced, ".claude", "settings.json"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClaudeHook(spaced); err != nil {
		t.Fatal(err)
	}
	if got := claudeHookCommands(t, spaced); len(got) != 1 || got[0] != quotePOSIXShellArgument(script) {
		t.Fatalf("hook commands on a spaced path: %q", got)
	}

	// A plain path keeps its form; fixed samples, since the temporary
	// directory's own spelling is the environment's (rv36).
	for _, plain := range []string{"/home/dev/repo/.aoci/hooks/claude-pretool.sh", "C:/work/repo/.aoci/hooks/claude-pretool.sh"} {
		if got := claudeHookCommand(plain); got != plain {
			t.Fatalf("hook command on a plain path changed form: %q", got)
		}
	}
}
