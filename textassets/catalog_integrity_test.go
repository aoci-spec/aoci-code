package textassets

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func TestEveryRegisteredOfficialAssetLoadsThroughProductionPath(t *testing.T) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}

	for _, locale := range manifest.OfficialLocales {
		for _, asset := range manifest.Assets {
			if _, err := Load(locale, ID(asset.ID)); err != nil {
				t.Fatalf("load registered asset %q for %q: %v", asset.ID, locale, err)
			}
		}
	}
}

func TestLoadNormalizesCheckoutCRLFToContractLF(t *testing.T) {
	asset := fixtureAsset("contracts/crlf", "contracts/crlf.txt")
	fixture := catalogFixture(t, fixtureManifest([]ManifestAsset{asset}), map[string]string{
		asset.Path: "first\r\nsecond\r\n",
	})

	value, err := loadFS(fixture, "", ID(asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	if value != "first\nsecond\n" {
		t.Fatalf("checkout line endings changed runtime contract bytes: %q", value)
	}
}

func TestCatalogRejectsMissingDuplicateAndUnregisteredAssets(t *testing.T) {
	base := fixtureAsset("contracts/example", "contracts/example.txt")
	wrongKind := base
	wrongKind.Kind = "prompt"
	wrongKind.EnforcedBy = []string{}
	tests := []struct {
		name      string
		assets    []ManifestAsset
		files     map[string]string
		wantError string
	}{
		{
			name:      "missing",
			assets:    []ManifestAsset{base},
			files:     map[string]string{},
			wantError: "is missing",
		},
		{
			name: "duplicate-id",
			assets: []ManifestAsset{
				base,
				fixtureAsset(base.ID, "contracts/second.txt"),
			},
			files: map[string]string{
				base.Path:              "first\n",
				"contracts/second.txt": "second\n",
			},
			wantError: "duplicate asset id",
		},
		{
			name: "duplicate-path",
			assets: []ManifestAsset{
				base,
				fixtureAsset("contracts/second", base.Path),
			},
			files:     map[string]string{base.Path: "shared\n"},
			wantError: "duplicate path",
		},
		{
			name:   "unregistered",
			assets: []ManifestAsset{base},
			files: map[string]string{
				base.Path:                 "registered\n",
				"contracts/forgotten.txt": "unregistered\n",
			},
			wantError: "unregistered text asset",
		},
		{
			name:      "kind-namespace-mismatch",
			assets:    []ManifestAsset{wrongKind},
			files:     map[string]string{base.Path: "registered\n"},
			wantError: `kind "prompt" requires asset id namespace "prompts/"`,
		},
	}

	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			err := validateCatalogFixture(t, fixtureManifest(current.assets), current.files)
			if err == nil || !strings.Contains(err.Error(), current.wantError) {
				t.Fatalf("expected error containing %q, got %v", current.wantError, err)
			}
		})
	}
}

func TestCatalogRejectsTemplateVariableDrift(t *testing.T) {
	tests := []struct {
		name      string
		declared  []string
		text      string
		wantError string
	}{
		{
			name:      "unknown-variable",
			declared:  []string{"Expected"},
			text:      "{{.Unexpected}}\n",
			wantError: "actual=[Unexpected] declared=[Expected]",
		},
		{
			name:      "declared-variable-missing",
			declared:  []string{"Expected"},
			text:      "plain text\n",
			wantError: "actual=[] declared=[Expected]",
		},
		{
			name:      "duplicate-declaration",
			declared:  []string{"Expected", "Expected"},
			text:      "{{.Expected}}\n",
			wantError: "duplicate template variable",
		},
	}

	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			asset := fixtureAsset("prompts/example", "prompts/example.txt")
			asset.Kind = "prompt"
			asset.EnforcedBy = []string{}
			asset.Variables = current.declared
			err := validateCatalogFixture(
				t,
				fixtureManifest([]ManifestAsset{asset}),
				map[string]string{asset.Path: current.text},
			)
			if err == nil || !strings.Contains(err.Error(), current.wantError) {
				t.Fatalf("expected error containing %q, got %v", current.wantError, err)
			}
		})
	}
}

func TestCatalogSeparatesProtocolTokensFromLocaleAnchors(t *testing.T) {
	asset := fixtureAsset("contracts/example", "contracts/example.txt")
	asset.ProtocolTokens = []string{"applied"}
	asset.LocaleAnchors = map[string][]string{"zh-CN": {"中文锚点"}}

	for _, current := range []struct {
		name, text, want string
	}{
		{"protocol", "中文锚点 stopped\n", "missing protocol token"},
		{"locale-anchor", "applied\n", "missing locale anchor"},
	} {
		t.Run(current.name, func(t *testing.T) {
			err := validateCatalogFixture(
				t,
				fixtureManifest([]ManifestAsset{asset}),
				map[string]string{asset.Path: current.text},
			)
			if err == nil || !strings.Contains(err.Error(), current.want) {
				t.Fatalf("expected %q, got %v", current.want, err)
			}
		})
	}

	_, err := decodeManifest([]byte(
		`{"version":2,"default_locale":"zh-CN","official_locales":["zh-CN"],"development_locales":[],"assets":[],"unknown":true}`,
	))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown manifest field must fail: %v", err)
	}

	prompt := fixtureAsset("prompts/example", "prompts/example.txt")
	prompt.Kind = "prompt"
	prompt.EnforcedBy = []string{"internal/index.ValidateHeaderText"}
	err = validateCatalogFixture(
		t,
		fixtureManifest([]ManifestAsset{prompt}),
		map[string]string{prompt.Path: "plain\n"},
	)
	if err == nil || !strings.Contains(err.Error(), "path.go:Symbol") {
		t.Fatalf("descriptive enforcement text must be rejected: %v", err)
	}
}

func TestDevelopmentLocaleLifecycle(t *testing.T) {
	assetA := fixtureAsset("contracts/a", "contracts/a.txt")
	assetA.ProtocolTokens = []string{"contract-key"}
	assetA.LocaleAnchors = map[string][]string{"zh-CN": {"中文锚点"}}
	assetB := fixtureAsset("contracts/b", "contracts/b.txt")
	manifest := fixtureManifest([]ManifestAsset{assetA, assetB})
	manifest.DevelopmentLocales = []string{"en-US"}

	fixture := catalogFixture(t, manifest, map[string]string{
		assetA.Path:            "contract-key 中文锚点\n",
		assetB.Path:            "中文B\n",
		"en-US/" + assetA.Path: "contract-key English A\n",
	})
	read, err := readManifestFS(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManifestFS(fixture, read); err != nil {
		t.Fatalf("partial development locale must be release-valid: %v", err)
	}
	if _, err := loadFS(fixture, "en-US", ID(assetA.ID)); err == nil ||
		!strings.Contains(err.Error(), "development-only") {
		t.Fatalf("development locale must not be production-selectable: %v", err)
	}

	manifest.OfficialLocales = append(manifest.OfficialLocales, "en-US")
	manifest.DevelopmentLocales = nil
	fixture = catalogFixture(t, manifest, map[string]string{
		assetA.Path:            "contract-key 中文锚点\n",
		assetB.Path:            "中文B\n",
		"en-US/" + assetA.Path: "contract-key English A\n",
	})
	read, err = readManifestFS(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManifestFS(fixture, read); err == nil ||
		!strings.Contains(err.Error(), "is missing for locale \"en-US\"") {
		t.Fatalf("promoted incomplete locale must fail release validation: %v", err)
	}
}

func TestLoadFailureDomainAndRetryAreAssetLocal(t *testing.T) {
	good := fixtureAsset("contracts/good", "contracts/good.txt")
	bad := fixtureAsset("contracts/bad", "contracts/bad.txt")
	bad.ProtocolTokens = []string{"required-token"}
	manifest := fixtureManifest([]ManifestAsset{good, bad})
	fixture := catalogFixture(t, manifest, map[string]string{
		good.Path: "good\n",
		bad.Path:  "broken\n",
	})

	const goroutines = 32
	var wait sync.WaitGroup
	errorsByCall := make(chan error, goroutines)
	for index := 0; index < goroutines; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := loadFS(fixture, "", ID(good.ID))
			if err == nil && value != "good\n" {
				err = errors.New("unexpected asset bytes: " + value)
			}
			errorsByCall <- err
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatalf("unrelated concurrent load failed: %v", err)
		}
	}

	if _, err := loadFS(fixture, "", ID(bad.ID)); err == nil ||
		!strings.Contains(err.Error(), `asset "contracts/bad"`) {
		t.Fatalf("dependent load must return a located error: %v", err)
	}
	fixture["zh-CN/"+bad.Path].Data = []byte("required-token\n")
	value, err := loadFS(fixture, "", ID(bad.ID))
	if err != nil || value != "required-token\n" {
		t.Fatalf("retry after repair must not reuse a cached failure: value=%q err=%v", value, err)
	}
}

func fixtureAsset(id, assetPath string) ManifestAsset {
	return ManifestAsset{
		ID:          id,
		Kind:        "contract",
		Path:        assetPath,
		Description: "test contract",
		UsedBy:      "textassets/catalog_integrity_test.go:validateCatalogFixture",
	}
}

func fixtureManifest(assets []ManifestAsset) Manifest {
	return Manifest{
		Version:         2,
		DefaultLocale:   "zh-CN",
		OfficialLocales: []string{"zh-CN"},
		Assets:          assets,
	}
}

func validateCatalogFixture(
	t *testing.T,
	manifest Manifest,
	files map[string]string,
) error {
	t.Helper()
	fixture := catalogFixture(t, manifest, files)
	read, err := readManifestFS(fixture)
	if err != nil {
		return err
	}

	return validateManifestFS(fixture, read)
}

func catalogFixture(
	t *testing.T,
	manifest Manifest,
	files map[string]string,
) fstest.MapFS {
	t.Helper()
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture := fstest.MapFS{
		baseManifestPath: {Data: manifestData},
		manifestFragmentsDir + "/placeholder.txt": {Data: []byte("not json")},
	}
	for filePath, text := range files {
		if !strings.HasPrefix(filePath, "en-US/") {
			filePath = "zh-CN/" + filePath
		}
		fixture[filePath] = &fstest.MapFile{Data: []byte(text)}
	}

	return fixture
}
