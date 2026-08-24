package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

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
