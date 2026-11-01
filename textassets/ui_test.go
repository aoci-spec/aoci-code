package textassets

import (
	"strings"
	"testing"
)

func TestUIMessageCatalogFailsClosed(t *testing.T) {
	if _, err := Message(DefaultLocale, "cli.short.root"); err != nil {
		t.Fatalf("official UI message did not load: %v", err)
	}
	if _, err := Message("fr-FR", "cli.short.root"); err == nil ||
		!strings.Contains(err.Error(), "unsupported text asset locale") {
		t.Fatalf("unknown UI message locale did not fail explicitly: %v", err)
	}
	if _, _, err := RelocalizeMessageExact("fr-FR", "anything"); err == nil ||
		!strings.Contains(err.Error(), "unsupported text asset locale") {
		t.Fatalf("unknown relocalization locale did not fail explicitly: %v", err)
	}
	if _, err := Message(DefaultLocale, "missing.runtime.message"); err == nil ||
		!strings.Contains(err.Error(), "is not declared") {
		t.Fatalf("unknown UI message key did not fail explicitly: %v", err)
	}
	if _, err := Message(DefaultLocale, "search.bad_filter_format"); err == nil ||
		!strings.Contains(err.Error(), "expects 1 format arguments") {
		t.Fatalf("UI message argument mismatch did not fail explicitly: %v", err)
	}
	if _, err := decodeMessageBundle([]byte(`{"key":"one","key":"two"}`)); err == nil ||
		!strings.Contains(err.Error(), "duplicate message key") {
		t.Fatalf("duplicate UI message key did not fail explicitly: %v", err)
	}
	if _, err := decodeMessageBundle([]byte(`{"key":"value"} {}`)); err == nil ||
		!strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing UI message data did not fail explicitly: %v", err)
	}
}

func TestOfficialLocaleSelectionNeverFallsBack(t *testing.T) {
	previous := ActiveLocale()
	t.Cleanup(func() { _ = SetActiveLocale(previous) })
	if err := SetActiveLocale("fr-FR"); err == nil {
		t.Fatal("unsupported locale was silently accepted")
	}
	if ActiveLocale() != previous {
		t.Fatalf("failed locale selection changed active locale: got=%s want=%s", ActiveLocale(), previous)
	}
}

func TestVolumeRuntimeMessagesExistInEveryOfficialLocale(t *testing.T) {
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		for _, current := range []struct {
			key  string
			args []any
		}{
			{"mcp.volume.header_identity", []any{"root", "meta"}},
			{"mcp.entries.object_ref_invalid", []any{"database://bad"}},
			{"mcp.entries.object_ref_not_indexed", []any{"database", "database://primary/public/missing"}},
			{"verify.volume.valid", []any{"root", "meta", "composite"}},
		} {
			if _, err := Message(locale, current.key, current.args...); err != nil {
				t.Fatalf("%s %s: %v", locale, current.key, err)
			}
		}
	}
}

// The PreToolUse hook texts carry tokens a host, a grep, or a reader acts on:
// the "aoci ..." line prefixes, the STALE marker, the tool and command names,
// and the line structure (a header ends with ":\n" so the Entry follows on
// its own line; the strict suffix starts on a new line). A translation may
// reword the prose, never those. Key parity and %s arity are already held by
// Validate; this locks what parity cannot see.
func TestPretoolHookMessagesKeepMachineTokensInEveryOfficialLocale(t *testing.T) {
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		for _, current := range []struct {
			key    string
			args   []any
			prefix string
			suffix string
			tokens []string
		}{
			{"hook.pretool.path_unresolved", []any{"reason"}, "aoci hook: ", "", []string{"(reason)"}},
			{"hook.pretool.not_indexed", []any{"src/n.go"}, "aoci: ", "", []string{"(src/n.go)", "aoci_update_entry"}},
			{"hook.pretool.entry_header", nil, "aoci: ", ":\n", nil},
			{"hook.pretool.stale_header", nil, "aoci ⚠ STALE: ", ":\n", nil},
			{"hook.pretool.unbaselined_header", nil, "aoci: ", ":\n", nil},
			{"hook.pretool.strict_blocked", nil, "\naoci strict: ", "", []string{"aoci_update_entry", "aoci scan"}},
		} {
			message, err := Message(locale, current.key, current.args...)
			if err != nil {
				t.Fatalf("%s %s: %v", locale, current.key, err)
			}
			if !strings.HasPrefix(message, current.prefix) || !strings.HasSuffix(message, current.suffix) {
				t.Errorf("%s %s must start with %q and end with %q: %q", locale, current.key, current.prefix, current.suffix, message)
			}
			if strings.Count(message, "\n") != strings.Count(current.prefix+current.suffix, "\n") {
				t.Errorf("%s %s must keep the line structure: %q", locale, current.key, message)
			}
			for _, token := range current.tokens {
				if !strings.Contains(message, token) {
					t.Errorf("%s %s lacks %q: %q", locale, current.key, token, message)
				}
			}
		}
	}
}

func TestPostureEffectMessageNamesOneModeInEveryOfficialLocale(t *testing.T) {
	// The cross-locale signature check compares argument arity, and "%[1]s to
	// %[1]s" still consumes exactly one argument, so it would pass while the
	// prompt told the approver that auto becomes auto. Only the rendered text
	// shows that, so assert on the rendering.
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		message, err := Message(locale, "scope.approval_effect.posture", "auto")
		if err != nil {
			t.Fatalf("load posture effect message for %s: %v", locale, err)
		}
		if strings.Count(message, "auto") != 1 {
			t.Errorf("posture effect for %s must name the destination once: %q", locale, message)
		}
	}
}

func TestRefreshReadyMessageRequiresAlignmentProofBeforeOverview(t *testing.T) {
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		message, err := Message(locale, "overview.refresh.ready")
		if err != nil {
			t.Fatalf("load refresh-ready message for %s: %v", locale, err)
		}
		for _, required := range []string{"terminal_proof", "Overview"} {
			if !strings.Contains(message, required) {
				t.Errorf("refresh-ready message for %s must require %s: %q", locale, required, message)
			}
		}
		if strings.Index(message, "terminal_proof") > strings.Index(message, "Overview") {
			t.Errorf("refresh-ready message for %s must put alignment proof before Overview: %q", locale, message)
		}
		for _, retired := range []string{"Verify", "Check", "Guide"} {
			if strings.Contains(message, retired) {
				t.Errorf("refresh-ready message for %s still prescribes %s after the final Apply: %q", locale, retired, message)
			}
		}
	}
}

func TestFormatSignatureTracksIndexedAndDynamicArguments(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"%s %d", "sd"},
		{"%[2]s %[1]d", "ds"},
		{"%[3]*.[2]*[1]f", "f**"},
		{"%*s", "*s"},
		{"%% %[1]q", "q"},
	}
	for _, test := range tests {
		if got := string(formatSignature(test.format)); got != test.want {
			t.Fatalf("formatSignature(%q)=%q, want %q", test.format, got, test.want)
		}
	}
}

func TestDiagnosticFactsPreserveMachineTokensWithoutEnglishProse(t *testing.T) {
	detail := `Could not open file. The system cannot find the file specified. json: unknown field "unexpected"; locale migration removed API Client.Send from src/client.go required=[source_sha256] candidate=[sha256=abc123]`
	facts := DiagnosticFacts(detail)
	for _, wanted := range []string{`"unexpected"`, "API", "Client.Send", "src/client.go", "source_sha256", "sha256=abc123"} {
		if !strings.Contains(facts, wanted) {
			t.Errorf("machine fact %q was not preserved: %q", wanted, facts)
		}
	}
	for _, prose := range []string{"Could", "file.", "The", "specified.", "unknown field", "locale migration removed"} {
		if strings.Contains(facts, prose) {
			t.Errorf("English prose %q leaked into preserved facts: %q", prose, facts)
		}
	}
}

func TestScopeChangeRemediationNeverAdvisesScanInEveryOfficialLocale(t *testing.T) {
	// The Guide reaches this instruction only after the baseline-missing branch
	// has returned, so a Baseline always exists here and scan always refuses.
	// The old text ended by offering scan anyway, which sent a blocked operator
	// at a command that cannot help them.
	for _, locale := range []string{DefaultLocale, LegacyLocale} {
		message, err := Message(locale, "guide.volumes_instruction_scope_change")
		if err != nil {
			t.Fatalf("load scope-change instruction for %s: %v", locale, err)
		}
		if strings.Contains(message, "aoci scan") {
			t.Errorf("scope-change remediation for %s still offers scan: %q", locale, message)
		}
		for _, required := range []string{"scope", "Baseline"} {
			if !strings.Contains(message, required) {
				t.Errorf("scope-change remediation for %s must mention %s: %q", locale, required, message)
			}
		}
		if !strings.Contains(message, "scope_activate") {
			t.Errorf("scope-change remediation for %s must name the activation command: %q", locale, message)
		}
	}
}
