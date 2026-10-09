package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestConfigCommandsHonorJSONOutput(t *testing.T) {
	root := t.TempDir()
	fixture := legacyTestConfig()
	fixture.ExcludeDirs = []string{"vendor", "third_party"}
	if err := fixture.SetOverviewChunkTokens(9000); err != nil {
		t.Fatal(err)
	}
	fixture.HookStrict = false
	fixture.LedgerEnabled = true
	if err := config.Save(root, fixture); err != nil {
		t.Fatal(err)
	}

	t.Run("list", func(t *testing.T) {
		stdout, stderr := runConfigJSON(t, root, "list")
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(stdout, &fields); err != nil {
			t.Fatalf("config list did not return a JSON object: %v\n%s", err, stdout)
		}
		for key, want := range map[string]any{
			"locale":         textassets.LegacyLocale,
			"exclude_dirs":   []any{"vendor", "third_party"},
			"hook_strict":    false,
			"ledger_enabled": true,
		} {
			assertJSONValue(t, "config list "+key, fields[key], want)
		}
		var delivery map[string]json.RawMessage
		if err := json.Unmarshal(fields["overview_delivery"], &delivery); err != nil {
			t.Fatalf("config list overview_delivery is not a JSON object: %v\n%s", err, stdout)
		}
		assertJSONValue(t, "config list overview_delivery.chunk_tokens", delivery["chunk_tokens"], float64(9000))
		if len(stderr) != 0 {
			t.Fatalf("config list wrote diagnostics on success: %s", stderr)
		}
	})

	t.Run("get", func(t *testing.T) {
		for _, current := range []struct {
			key  string
			want any
		}{
			{"locale", textassets.LegacyLocale},
			{"exclude_dirs", []any{"vendor", "third_party"}},
			{"exclude_root_dirs", []any{}},
			{"overview_delivery.chunk_tokens", float64(9000)},
			{"hook_strict", false},
			{"ledger_enabled", true},
		} {
			stdout, stderr := runConfigJSON(t, root, "get", current.key)
			assertJSONValue(t, "config get "+current.key, stdout, current.want)
			if len(stderr) != 0 {
				t.Fatalf("config get %s wrote diagnostics on success: %s", current.key, stderr)
			}
		}
		// The headline case of issue #94: locale is one JSON string, byte for byte.
		if stdout, _ := runConfigJSON(t, root, "get", "locale"); string(stdout) != `"zh-CN"`+"\n" {
			t.Fatalf("config get locale = %q, want %q", stdout, `"zh-CN"`+"\n")
		}
	})

	t.Run("set", func(t *testing.T) {
		stdout, stderr := runConfigJSON(t, root, "set", "ledger_enabled", "false")
		result := decodeConfigSetResult(t, stdout)
		assertConfigSetResultKeys(t, result, "ok", "key", "value")
		assertJSONValue(t, "config set ledger_enabled value", result["value"], false)
		assertConfigSetMatchesGet(t, root, "ledger_enabled", result)
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

	t.Run("set value is the following get", func(t *testing.T) {
		for _, current := range []struct {
			key   string
			raw   string
			value any
		}{
			{"exclude_dirs", "a, b", []any{"a", "b"}},
			{"exclude_root_dirs", "build, cache", []any{"build", "cache"}},
			{"exclude_root_dirs", "build/, cache, build", []any{"build", "cache"}},
			{"overview_delivery.chunk_tokens", "12000", float64(12000)},
			{"code_cognition_batch_entries", "20", float64(20)},
			{"hook_strict", "true", true},
			{"index_path", "aoci.txt", "aoci.txt"},
		} {
			stdout, stderr := runConfigJSON(t, root, "set", current.key, current.raw)
			result := decodeConfigSetResult(t, stdout)
			assertConfigSetResultKeys(t, result, "ok", "key", "value")
			assertJSONValue(t, "config set "+current.key+" value", result["value"], current.value)
			assertConfigSetMatchesGet(t, root, current.key, result)
			if len(stderr) != 0 {
				t.Fatalf("config set %s wrote diagnostics on success: %s", current.key, stderr)
			}
		}
	})
}

// A personal config.local.json can override a key that set writes to the team
// layer. The set result still reports what the next get returns.
func TestConfigSetJSONValueFollowsLocalOverride(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalFilePath(root), []byte(`{"ledger_enabled": true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runConfigJSON(t, root, "set", "ledger_enabled", "false")
	result := decodeConfigSetResult(t, stdout)
	assertConfigSetResultKeys(t, result, "ok", "key", "value")
	assertJSONValue(t, "config set ledger_enabled value under a local override", result["value"], true)
	assertConfigSetMatchesGet(t, root, "ledger_enabled", result)

	team, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if team.LedgerEnabled {
		t.Fatal("config set did not persist false to the team layer")
	}
}

func TestConfigSetLocaleJSONCarriesRestartAndMigrationFacts(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	t.Run("no index", func(t *testing.T) {
		if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := config.Save(root, legacyTestConfig()); err != nil {
			t.Fatal(err)
		}
		stdout, stderr := runConfigJSON(t, root, "set", "locale", textassets.DefaultLocale)
		result := decodeConfigSetResult(t, stdout)
		assertConfigSetResultKeys(t, result, "ok", "key", "value", "restart_mcp_required")
		assertJSONValue(t, "config set locale value", result["value"], textassets.DefaultLocale)
		assertJSONValue(t, "config set locale restart_mcp_required", result["restart_mcp_required"], true)
		assertConfigSetMatchesGet(t, root, "locale", result)
		if len(stderr) != 0 {
			t.Fatalf("config set locale wrote diagnostics on success: %s", stderr)
		}
	})

	t.Run("legacy index with pending migration", func(t *testing.T) {
		if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
			t.Fatal(err)
		}
		jsonRoot := writeLocaleMigrationFixture(t)
		humanRoot := writeLocaleMigrationFixture(t)

		stdout, stderr := runConfigJSON(t, jsonRoot, "set", "locale", textassets.DefaultLocale)
		result := decodeConfigSetResult(t, stdout)
		assertConfigSetResultKeys(t, result, "ok", "key", "value", "restart_mcp_required", "locale_migration")
		assertJSONValue(t, "config set locale value", result["value"], textassets.DefaultLocale)
		assertJSONValue(t, "config set locale restart_mcp_required", result["restart_mcp_required"], true)
		assertConfigSetMatchesGet(t, jsonRoot, "locale", result)
		if len(stderr) != 0 {
			t.Fatalf("config set locale wrote diagnostics on success: %s", stderr)
		}

		var migration map[string]json.RawMessage
		if err := json.Unmarshal(result["locale_migration"], &migration); err != nil {
			t.Fatalf("locale_migration is not a JSON object: %v\n%s", err, stdout)
		}
		if got := sortedRawKeys(migration); strings.Join(got, ",") != "curation_paths,entry_paths,header_pending" {
			t.Fatalf("locale_migration keys = %v\n%s", got, stdout)
		}
		assertJSONValue(t, "locale_migration.header_pending", migration["header_pending"], true)
		assertJSONValue(t, "locale_migration.entry_paths", migration["entry_paths"], float64(2))
		assertJSONValue(t, "locale_migration.curation_paths", migration["curation_paths"], float64(1))

		receipt, err := config.LoadBase(jsonRoot)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.LocaleMigration == nil || !receipt.LocaleMigration.HeaderPending ||
			len(receipt.LocaleMigration.EntryPaths) != 2 || len(receipt.LocaleMigration.CurationPaths) != 1 {
			t.Fatalf("persisted receipt does not match the JSON summary: %+v", receipt.LocaleMigration)
		}

		// Human mode on an identical fixture prints the same three numbers.
		var humanStdout, humanStderr bytes.Buffer
		if code := executeCLI(
			[]string{"--repo", humanRoot, "config", "set", "locale", textassets.DefaultLocale},
			&humanStdout,
			&humanStderr,
		); code != ExitOK {
			t.Fatalf("human config set locale failed: code=%d stdout=%s stderr=%s", code, humanStdout.String(), humanStderr.String())
		}
		if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
			t.Fatal(err)
		}
		wantMigration := cliMessage("config.locale_migration_pending", true, 2, 1)
		wantRestart := cliMessage("config.restart_mcp")
		if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(humanStdout.String(), wantMigration) || !strings.Contains(humanStdout.String(), wantRestart) {
			t.Fatalf("human config set locale output lacks the notices:\n%s\nwant lines:\n%s\n%s", humanStdout.String(), wantMigration, wantRestart)
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

// assertJSONValue decodes raw as one JSON value of any type and requires the
// exact Go type and value json.Unmarshal produces for it: a missing field
// (nil raw) and null both fail instead of decoding to a zero value.
func assertJSONValue(t *testing.T, label string, raw []byte, want any) {
	t.Helper()
	if raw == nil {
		t.Fatalf("%s: value is missing", label)
	}
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: not JSON: %v\n%s", label, err, raw)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v (%T), want %#v (%T)\n%s", label, got, got, want, want, raw)
	}
}

func decodeConfigSetResult(t *testing.T, stdout []byte) map[string]json.RawMessage {
	t.Helper()
	var result map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("config set did not return a JSON object: %v\n%s", err, stdout)
	}
	return result
}

func assertConfigSetResultKeys(t *testing.T, result map[string]json.RawMessage, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := sortedRawKeys(result); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("config set result keys = %v, want %v", got, want)
	}
}

// assertConfigSetMatchesGet requires ok=true, the echoed key, and a value that
// is the JSON the next `aoci --json config get <key>` prints.
func assertConfigSetMatchesGet(t *testing.T, root, key string, result map[string]json.RawMessage) {
	t.Helper()
	assertJSONValue(t, "config set "+key+" ok", result["ok"], true)
	assertJSONValue(t, "config set "+key+" key", result["key"], key)
	getStdout, getStderr := runConfigJSON(t, root, "get", key)
	if len(getStderr) != 0 {
		t.Fatalf("config get %s wrote diagnostics on success: %s", key, getStderr)
	}
	if setValue, getValue := compactJSON(t, result["value"]), compactJSON(t, getStdout); setValue != getValue {
		t.Fatalf("config set %s value = %s, but the following get returned %s", key, setValue, getValue)
	}
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	return buffer.String()
}

func sortedRawKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writeLocaleMigrationFixture builds a zh-CN Legacy repository whose locale
// change to en-US records a receipt with the Header pending, two Entry paths
// and one Curation path.
func writeLocaleMigrationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	agentPlanWriteFile(t, root, "keep.go", "package main\n")
	agentPlanWriteFile(t, root, "empty.bin", "")
	agentPlanWriteFile(t, root, "aoci.txt", "#Locale: zh-CN\n"+agentPlanHeader(true)+
		"\n===代码索引"+rootSlash+"/===\n"+
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n"+
		"keep.go[XAP7T]: F:对齐文件 | R:- | A:- | S:-\n")
	profile, err := curation.ProfilePath(root, "empty.bin")
	if err != nil {
		t.Fatal(err)
	}
	document := curation.NewDocument()
	document.Decisions = []curation.Decision{{
		Path:         "empty.bin",
		Decision:     curation.DecisionExclude,
		Role:         "空占位文件",
		Reason:       "不承载运行语义",
		Confidence:   100,
		SourceSHA256: profile.SourceSHA256,
		Agent:        "config-json-test",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}}
	if err := curation.Save(root, document); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	return root
}
