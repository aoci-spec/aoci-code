package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

// A diagnostic that does not match the active locale is discarded wholesale and
// replaced by a generic message plus a machine-fact fragment. So a hint is not
// delivered merely because it was built: it has to survive this guard.
//
// The dimension near-miss hint interpolates the accepted spellings, and the
// canonical spelling of every dimension is Chinese. Under en-US that made the
// hint carry Han text, which collapsed the whole refusal into "the project
// configuration or initialization state is invalid — inspect .aoci/config.json",
// pointing the operator at the wrong file. rc5, which had no hint at all,
// produced a better message than rc6 did on this path.
func TestDictionaryNearMissHintSurvivesEveryOfficialLocale(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		for _, canonical := range []string{"E规模", "S配额", "A层级", "B模块", "C重要度"} {
			spellings := index.AcceptedDimensionSpellingsForLocale(canonical, locale)
			if len(spellings) == 0 {
				t.Fatalf("%s: no spelling may be quoted for %s", locale, canonical)
			}
			// The written spelling is the operator's own header text, so it may
			// carry any script. The near-miss folder strips the ideographic
			// space precisely so a Han typo like "#E 规模:" is caught, which
			// means this argument reaches an en-US message carrying Han. A test
			// that hardcodes an ASCII spelling here can never see that.
			for _, written := range []string{"E-Scale", "E 规模", "S 配额", "e_scale"} {
				hint := cliMessage("entry.write.hint.dictionary_name_unrecognized",
					index.DisplayDimensionSpelling(written, locale), strings.Join(spellings, " / "))
				if got := localeSafeCLIDetail(hint); got != hint {
					t.Fatalf("%s: the near-miss hint for %s quoting %q does not survive the locale guard.\n"+
						"  built:     %s\n  delivered: %s\n"+
						"Every interpolated argument must match the locale of the message that carries it, "+
						"or the whole diagnostic is replaced and the operator is sent to the wrong file.",
						locale, canonical, written, hint, got)
				}
			}
		}
	}
}
