package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestConfigCommandsHonorJSONOutput(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}

	t.Run("list", func(t *testing.T) {
		stdout, stderr := runConfigJSON(t, root, "list")
		var value map[string]any
		if err := json.Unmarshal(stdout, &value); err != nil {
			t.Fatalf("config list did not return JSON: %v\n%s", err, stdout)
		}
		if _, ok := value["locale"]; !ok {
			t.Fatalf("config list JSON omitted locale: %s", stdout)
		}
		if len(stderr) != 0 {
			t.Fatalf("config list wrote diagnostics on success: %s", stderr)
		}
	})

	t.Run("get", func(t *testing.T) {
		stdout, stderr := runConfigJSON(t, root, "get", "hook_strict")
		var value bool
		if err := json.Unmarshal(stdout, &value); err != nil {
			t.Fatalf("config get did not return a JSON scalar: %v\n%s", err, stdout)
		}
		if value != legacyTestConfig().HookStrict {
			t.Fatalf("config get returned %v, want %v", value, legacyTestConfig().HookStrict)
		}
		if len(stderr) != 0 {
			t.Fatalf("config get wrote diagnostics on success: %s", stderr)
		}
	})

	t.Run("set", func(t *testing.T) {
		stdout, stderr := runConfigJSON(t, root, "set", "ledger_enabled", "false")
		var result struct {
			OK    bool   `json:"ok"`
			Key   string `json:"key"`
			Value bool   `json:"value"`
		}
		if err := json.Unmarshal(stdout, &result); err != nil {
			t.Fatalf("config set did not return JSON: %v\n%s", err, stdout)
		}
		if !result.OK || result.Key != "ledger_enabled" || result.Value {
			t.Fatalf("unexpected config set result: %+v", result)
		}
		if len(stderr) != 0 {
			t.Fatalf("config set wrote diagnostics on success: %s", stderr)
		}
		updated, err := config.LoadBase(root)
		if err != nil {
			t.Fatal(err)
		}
		if updated.LedgerEnabled {
			t.Fatal("config set did not persist the requested value")
		}
	})
}

func TestConfigGetUsesCobraOutputWriter(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "config", "get", "locale"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("config get failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("config get did not write to the command output writer")
	}
	if stderr.Len() != 0 {
		t.Fatalf("config get wrote diagnostics on success: %s", stderr.String())
	}
}

func runConfigJSON(t *testing.T, root string, args ...string) ([]byte, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	arguments := append([]string{"--repo", root, "--json", "config"}, args...)
	if code := executeCLI(arguments, &stdout, &stderr); code != ExitOK {
		t.Fatalf("config command failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	return stdout.Bytes(), stderr.Bytes()
}
