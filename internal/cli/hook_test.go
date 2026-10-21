package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func TestHookCLIConfigurationFailure(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, current := range []struct {
		name, team, local string
	}{
		{"team JSON", `{"version":`, ""},
		{"local JSON", `{"version":2,"locale":"en-US"}`, `{"ai":`},
		{"legacy local JSON", `{"version":2}`, `{"ai":`},
		{"locale", `{"version":2,"locale":"unsupported"}`, ""},
	} {
		t.Run(current.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".aoci")
			if err := os.Mkdir(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"config.json": current.team, "config.local.json": current.local} {
				if data != "" {
					if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, command := range [][]string{
				{"hook", "pretool", "--agent", "claude", "--tool", "Edit", "--path", "sample.go"},
				{"hook", "codex-compact"},
			} {
				// A previous invocation's locale must not become the fallback.
				if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
					t.Fatal(err)
				}
				printed, stdout, stderr, code := runCLIPrinted(t, root, command...)
				if code != ExitOK || stderr != "" {
					t.Errorf("%v: exit=%d stdout=%q stderr=%q", command, code, stdout, stderr)
				}
				if printed != "" || (command[1] == "pretool" && stdout != "") {
					t.Errorf("failed pretool must stay silent and compact must use its bound writer: printed=%q stdout=%q", printed, stdout)
				}
				if command[1] == "codex-compact" && (!strings.HasPrefix(stdout, "AOCI context-compaction refresh is required.\n") || !strings.Contains(stdout, "refresh_event_id: aoci-compact-")) {
					t.Errorf("compact hook lost its default-locale context: %q", stdout)
				}
			}
			for name, want := range map[string]string{"config.json": current.team, "config.local.json": current.local} {
				data, err := os.ReadFile(filepath.Join(directory, name))
				if want == "" && os.IsNotExist(err) {
					continue
				}
				if err != nil || string(data) != want {
					t.Errorf("hook changed %s: %q, %v", name, data, err)
				}
			}
			stdout, _, code := runCLI(t, root, "status", "--json")
			if code != ExitConfig || !json.Valid([]byte(stdout)) || !strings.Contains(stdout, `"error_code": "config"`) {
				t.Errorf("ordinary CLI must still refuse invalid config: exit=%d stdout=%q", code, stdout)
			}
		})
	}
}

func TestHookCLIValidConfiguration(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultConfig()
			cfg.Locale = locale
			cfg.IndexPath = ".aoci/index.txt"
			cfg.HookStrict = true
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			for path, data := range map[string]string{
				"sample.go":       "A1",
				".aoci/index.txt": "===段 " + filepath.ToSlash(root) + "/===\nsample.go[CG8T]: F:Runs the sample | R:- | A:- | S:Preserve the hook guard\n",
			} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
			if err != nil {
				t.Fatal(err)
			}
			if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runCLI(t, root, "hook", "codex-compact")
			if code != ExitOK || stderr != "" || !strings.Contains(stdout, "refresh_event_id: aoci-compact-") || containsHanText(stdout) != (locale == textassets.LegacyLocale) {
				t.Fatalf("compact hook did not honor %s: exit=%d stdout=%q stderr=%q", locale, code, stdout, stderr)
			}
			printed, _, stderr, code := runCLIPrinted(t, root, "hook", "pretool", "--agent", "claude", "--tool", "Edit", "--path", "sample.go")
			if code != ExitOK || stderr != "" || !strings.Contains(printed, "Preserve the hook guard") {
				t.Fatalf("fresh pretool did not inject its Entry: exit=%d stdout=%q stderr=%q", code, printed, stderr)
			}
			for _, agent := range []struct {
				name string
				code int
			}{{"claude", ExitHookBlockClaude}, {"", ExitHookBlock}} {
				if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("A2"), 0o644); err != nil {
					t.Fatal(err)
				}
				_, _, stderr, code := runCLIPrinted(t, root, "hook", "pretool", "--agent", agent.name, "--tool", "Edit", "--path", "sample.go")
				if code != agent.code || (agent.name == "claude" && stderr == "") {
					t.Errorf("strict stale hook %q: exit=%d stderr=%q", agent.name, code, stderr)
				}
			}
		})
	}
}

func TestHookCLIFallbackPreservesRecoveryGate(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		"config.json":                          `{"version":`,
		"transactions/bootstrap-deadbeef.json": "{}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, ".aoci", path), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, code := runCLI(t, root, "--json", "hook", "pretool", "--path", "sample.go")
	if code != ExitInvalid || !strings.Contains(stdout, "bootstrap_recovery_pending") {
		t.Fatalf("pretool crossed Recovery: exit=%d stdout=%q", code, stdout)
	}
	stdout, stderr, code := runCLI(t, root, "hook", "codex-compact")
	if code != ExitOK || stderr != "" || !strings.Contains(stdout, "refresh_event_id: aoci-compact-") {
		t.Fatalf("compact context must bypass Recovery: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

type failingCodexCompactEntropy struct{}

func (failingCodexCompactEntropy) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func useEnglishCodexCompactContext(t *testing.T) {
	t.Helper()
	previous := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
}

func TestCodexCompactRefreshEventIDUniqueAndBounded(t *testing.T) {
	entropy := bytes.NewReader(make([]byte, 32))
	first := newCodexCompactRefreshEventID(entropy)
	second := newCodexCompactRefreshEventID(entropy)

	if first == second {
		t.Fatalf("refresh_event_id must be unique: %q", first)
	}
	for _, id := range []string{first, second} {
		if len(id) > 128 {
			t.Fatalf("refresh_event_id exceeds 128 characters: %d", len(id))
		}
		if !strings.HasPrefix(id, "aoci-compact-") || strings.ContainsAny(id, "\r\n") {
			t.Fatalf("invalid refresh_event_id: %q", id)
		}
	}
}

func TestCodexCompactRefreshEventIDFallsBack(t *testing.T) {
	first := newCodexCompactRefreshEventID(failingCodexCompactEntropy{})
	second := newCodexCompactRefreshEventID(failingCodexCompactEntropy{})

	if first == second {
		t.Fatalf("fallback refresh_event_id must be unique: %q", first)
	}
	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "aoci-compact-fallback-") {
			t.Fatalf("expected safe fallback refresh_event_id, got %q", id)
		}
		if len(id) > 128 {
			t.Fatalf("fallback refresh_event_id exceeds 128 characters: %d", len(id))
		}
	}
}

func TestCodexCompactDeveloperContext(t *testing.T) {
	useEnglishCodexCompactContext(t)
	const id = "aoci-compact-test-event"
	output := codexCompactDeveloperContext(id)

	for _, required := range []string{
		"refresh_event_id: " + id,
		"deliberately omits AOCI Whole-Index cognition",
		"Before continuing the business task",
		"aoci_rules",
		"ordinary complete Whole-Index aoci_overview",
		"check_only absent or false",
		"probe absent or false",
		`refresh_reasons=["context_compaction"]`,
		`refresh_event_id="` + id + `"`,
		"stable_checkpoint",
		"cognition_receipt",
		"exact next_cursor",
		"completed=true",
		"host_delivery_confirmation",
		"exactly one model_cognition_attestation",
	} {
		if !strings.Contains(output, required) {
			t.Errorf("developer context missing %q:\n%s", required, output)
		}
	}
	if strings.Count(output, id) != 2 {
		t.Fatalf("developer context must repeat the exact event ID twice:\n%s", output)
	}
}

func TestCodexCompactCommandOutputsPlainDeveloperContext(t *testing.T) {
	useEnglishCodexCompactContext(t)
	command := newCodexCompactCmd(bytes.NewReader(make([]byte, 16)))
	recoveryGateCalled := false
	root := &cobra.Command{
		Use: "aoci",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			recoveryGateCalled = true
			return nil
		},
	}
	hook := &cobra.Command{Use: "hook"}
	hook.AddCommand(command)
	root.AddCommand(hook)
	root.SetArgs([]string{"hook", "codex-compact"})

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(io.Discard)

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if recoveryGateCalled {
		t.Fatal("codex-compact must not run the repository recovery gate")
	}
	if !command.Hidden {
		t.Fatal("codex-compact must remain hidden")
	}
	if !strings.HasPrefix(output, "AOCI context-compaction refresh is required.\n") {
		t.Fatalf("command did not emit plain developer context:\n%s", output)
	}
	if !strings.HasSuffix(output, "\n") || strings.HasSuffix(output, "\n\n") {
		t.Fatalf("command output must have exactly one trailing newline: %q", output)
	}

	const prefix = "refresh_event_id: "
	var id string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			id = strings.TrimPrefix(line, prefix)
			break
		}
	}
	if id == "" || len(id) > 128 {
		t.Fatalf("command emitted invalid refresh_event_id %q", id)
	}
}
