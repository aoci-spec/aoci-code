package cli

import "testing"

// PowerShell reads the typographic single quotes as quote marks too, so each
// is doubled inside a single-quoted argument like the ASCII one; otherwise a
// repository path holding one ends the argument early (rv45, #211).
func TestPowerShellQuoteDoublesEverySingleQuoteMark(t *testing.T) {
	for in, want := range map[string]string{
		`D:/Work/re$po`:          `'D:/Work/re$po'`,
		"D:/Bob's/repo":          `'D:/Bob''s/repo'`,
		"D:/Work/re\u2019po":     "'D:/Work/re\u2019\u2019po'",
		"D:/\u2018a\u201a\u201b": "'D:/\u2018\u2018a\u201a\u201a\u201b\u201b'",
	} {
		if got := powerShellQuote(in); got != want {
			t.Errorf("powerShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
