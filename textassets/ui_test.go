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

func TestRelocalizeMessageExactUsesOfficialCatalogMapping(t *testing.T) {
	current, err := Message(DefaultLocale, "cli.short.root")
	if err != nil {
		t.Fatal(err)
	}
	want, err := Message(LegacyLocale, "cli.short.root")
	if err != nil {
		t.Fatal(err)
	}
	got, matched, err := RelocalizeMessageExact(LegacyLocale, current)
	if err != nil || !matched || got != want {
		t.Fatalf("exact relocalization mismatch: got=%q matched=%v err=%v want=%q", got, matched, err, want)
	}
	if got, matched, err := RelocalizeMessageExact(LegacyLocale, "not a catalog value"); err != nil || matched || got != "" {
		t.Fatalf("unknown exact value must remain unmatched: got=%q matched=%v err=%v", got, matched, err)
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
		for _, required := range []string{"Verify", "Check", "Guide", "Overview"} {
			if !strings.Contains(message, required) {
				t.Errorf("refresh-ready message for %s must require %s: %q", locale, required, message)
			}
		}
		if strings.Index(message, "Verify") > strings.Index(message, "Overview") ||
			strings.Index(message, "Check") > strings.Index(message, "Overview") ||
			strings.Index(message, "Guide") > strings.Index(message, "Overview") {
			t.Errorf("refresh-ready message for %s must put alignment proof before Overview: %q", locale, message)
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
	}
}
