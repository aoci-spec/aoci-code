package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

// A header line naming a dimension in a spelling the parser rejects is thrown
// away and replaced by the fallback dictionary, and the refusal then reports the
// operator's own declared symbol as illegal "see the E scale line in the header"
// — pointing at the line it discarded. The write path treats that as a hard
// refusal, so the operator is blocked by a message describing a state they
// cannot see and cannot act on. The hint must name the spelling.
func TestDictionaryRefusalNamesARejectedDimensionSpelling(t *testing.T) {
	header := "#A Layer: C Code\n#B Module: G General\n#C Importance: 9 8 7\n#E-Scale: A B\n"
	dictionary := index.ExtractTagDict(header)

	misses := dictionary.UnrecognizedDimensionNames()
	if len(misses) != 1 || misses[0].Written != "E-Scale" || misses[0].Canonical != "E规模" {
		t.Fatalf("the discarded declaration was not recorded: %+v", misses)
	}
	if index.CheckTagsAgainstDict("a.go[CG7A]: F:x | R:- | A:- | S:-", dictionary) == nil {
		t.Fatal("fixture precondition: the fallback dictionary must still refuse the operator's own symbol")
	}

	accepted := index.AcceptedDimensionSpellings(misses[0].Canonical)
	if len(accepted) == 0 {
		t.Fatal("no accepted spelling to offer")
	}
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		hint := writeMessage("entry.write.hint.dictionary_name_unrecognized",
			misses[0].Written, strings.Join(accepted, " / "))
		if !strings.Contains(hint, "E-Scale") {
			t.Fatalf("%s hint does not name the rejected spelling: %s", locale, hint)
		}
		for _, spelling := range accepted {
			if !strings.Contains(hint, spelling) {
				t.Fatalf("%s hint does not offer the accepted spelling %q: %s", locale, spelling, hint)
			}
		}
	}
}
