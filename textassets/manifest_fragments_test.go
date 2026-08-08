package textassets

import (
	"strings"
	"testing"
)

func TestReadManifestMergesHostAgentFragment(t *testing.T) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}

	wanted := map[string]bool{
		string(ContractHostRuntimeBaseInstructions): false,
		string(ContractHostHelpEntriesStageLong):    false,
		string(ContractHostHelpGuideLong):           false,
	}
	for _, asset := range manifest.Assets {
		if _, exists := wanted[asset.ID]; exists {
			wanted[asset.ID] = true
		}
	}
	for assetID, found := range wanted {
		if !found {
			t.Fatalf("merged text asset manifest is missing %q", assetID)
		}
	}
}

func TestManifestFragmentOwnsNoLocaleLifecycle(t *testing.T) {
	for _, source := range []string{
		`{"version":2,"default_locale":"zh-CN","assets":[]}`,
		`{"version":2,"official_locales":["zh-CN"],"assets":[]}`,
		`{"version":2,"development_locales":["en-US"],"assets":[]}`,
	} {
		if _, err := decodeManifestFragment([]byte(source)); err == nil ||
			!strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("fragment locale lifecycle duplication must fail: %s: %v", source, err)
		}
	}
}
