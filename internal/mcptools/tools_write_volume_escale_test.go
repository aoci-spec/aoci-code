package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// numericEScaleMeta declares the E-scale bands the way aoci init writes them.
const numericEScaleMeta = cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n"

// divergentEScaleMeta gives the database dictionary its own, incompatible E
// bands: under them a four-line file is L. Only the code dictionary may
// judge a Code object.
const divergentEScaleMeta = cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L-large>2 T-tiny<3\n"

// In Volumes v1 the E-scale thresholds live in the Meta Volume, while the
// Code Volume's own header is one marker line. The write path used to read
// the thresholds from the Code header, found none, and so never warned: a
// four-line file tagged L applied without a word. The warning is advisory,
// never a rejection, and it is written in the active Locale.
func TestVolumeUpdateWarnsWhenTheETagContradictsTheFileLength(t *testing.T) {
	for _, test := range []struct {
		name string
		meta string
		tag  string
		warn bool
	}{
		{"wrong band", numericEScaleMeta, "L", true},
		{"right band", numericEScaleMeta, "T", false},
		// Each Meta section declares its own E Scale line; folding the whole
		// Meta let the last section, database, overwrite code's thresholds
		// and would have told the model to retag this file L.
		{"database dictionary must not judge a Code object", divergentEScaleMeta, "T", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildVolumeRepoWithMeta(t, true, false, test.meta)
			writeVolumeTestFile(t, root, "main.go", "package main\n\nfunc main() {\n}\n")
			session := connectMCPClient(t, root)
			result := applyVolumeBatch(t, session, map[string]any{
				"path":          "main.go",
				"new_entry":     "main.go[CD9" + test.tag + "]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep execution deterministic",
				"source_sha256": sourceSHA256(t, root, "main.go"),
			})
			if result.Status != autoStatusApplied || result.Applied != 1 {
				t.Fatalf("the E band is advisory and must not block the write: %#v", result)
			}
			var warnings []string
			if result.Audit != nil {
				warnings = result.Audit.Warnings
			}
			if !test.warn {
				if len(warnings) != 0 {
					t.Fatalf("a correct E tag produced warnings: %#v", warnings)
				}
				return
			}
			// Both Locales spell the band and the tag as bare capitals; no other
			// capital T or L appears in either sentence.
			if len(warnings) != 1 || !strings.Contains(warnings[0], "4") ||
				!strings.Contains(warnings[0], "T") || !strings.Contains(warnings[0], "L") {
				t.Fatalf("expected one E-scale warning naming 4 lines, band T, and tag L: %#v", warnings)
			}
		})
	}
}
