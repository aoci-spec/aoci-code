package textassets

import (
	"io/fs"
	"path"
	"sort"
	"testing"
)

func TestOfficialLocaleAssetsAreNotAccidentalCopies(t *testing.T) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}
	allowedIdentical := map[string]bool{
		"prompts/ai-probe-user.txt": true,
	}
	identical := []string{}
	for _, asset := range manifest.Assets {
		en, err := fs.ReadFile(embeddedAssets, path.Join(DefaultLocale, asset.Path))
		if err != nil {
			t.Fatal(err)
		}
		zh, err := fs.ReadFile(embeddedAssets, path.Join(LegacyLocale, asset.Path))
		if err != nil {
			t.Fatal(err)
		}
		if string(en) == string(zh) && !allowedIdentical[asset.Path] {
			identical = append(identical, asset.Path)
		}
	}
	sort.Strings(identical)
	if len(identical) > 0 {
		t.Fatalf("English assets are byte-identical to Chinese assets: %v", identical)
	}
}
