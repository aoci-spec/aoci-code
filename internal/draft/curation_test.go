// curation草稿类型与Generation State往返测试。
package draft

import (
	"strings"
	"testing"
)

func TestCurationManifestRoundTrip(
	t *testing.T,
) {
	root := t.TempDir()
	runID := "20260715T010203Z"

	manifest := &Manifest{
		RunID:          runID,
		Kind:           KindCuration,
		PlanID:         strings.Repeat("a", 64),
		IndexSHA256:    strings.Repeat("b", 64),
		HeaderSHA256:   strings.Repeat("c", 64),
		CurationSHA256: strings.Repeat("d", 64),
		GenerationHash: strings.Repeat("e", 64),
		Files: []string{
			CurationFileName,
		},
	}

	if err := SaveManifest(
		root,
		manifest,
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Kind != KindCuration ||
		loaded.CurationSHA256 !=
			manifest.CurationSHA256 ||
		len(loaded.Files) != 1 ||
		loaded.Files[0] !=
			CurationFileName {
		t.Fatalf(
			"curation Manifest往返不符: %+v",
			loaded,
		)
	}
}
