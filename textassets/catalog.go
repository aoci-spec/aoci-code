// Package textassets provides embedded and versioned natural-language assets.
//
// The package has no dependency on internal CLI, workflow, model, draft, storage,
// network, or filesystem packages. It is safe for deterministic core packages to
// import.
package textassets

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
)

const (
	// DefaultLocale is the locale used by a new project when no explicit locale
	// is supplied.
	DefaultLocale = "en-US"
	// LegacyLocale is assigned deterministically to pre-locale projects.
	LegacyLocale = "zh-CN"
)

var (
	activeLocale         = DefaultLocale
	activeLocaleMu       sync.RWMutex
	embeddedManifestOnce sync.Once
	embeddedManifestData Manifest
	embeddedManifestErr  error
)

// ActiveLocale returns the locale selected for the current CLI or MCP process.
// A process uses exactly one locale so one response can never mix catalogs.
func ActiveLocale() string {
	activeLocaleMu.RLock()
	defer activeLocaleMu.RUnlock()
	return activeLocale
}

// SetActiveLocale selects the single locale used by the current process. It
// validates the official catalog lifecycle and never falls back to another
// locale.
func SetActiveLocale(locale string) error {
	manifest, err := embeddedManifest()
	if err != nil {
		return err
	}
	locale = strings.TrimSpace(locale)
	if !containsString(manifest.OfficialLocales, locale) {
		return fmt.Errorf("unsupported text asset locale %q", locale)
	}
	activeLocaleMu.Lock()
	activeLocale = locale
	activeLocaleMu.Unlock()
	return nil
}

// IsOfficialLocale reports whether locale is a production-selectable locale.
func IsOfficialLocale(locale string) bool {
	manifest, err := embeddedManifest()
	if err != nil {
		return false
	}
	return containsString(manifest.OfficialLocales, strings.TrimSpace(locale))
}

const (
	baseManifestPath     = "manifest.json"
	manifestFragmentsDir = "manifests"
)

// ID is a stable language-independent asset identifier.
type ID string

const (
	// ContractRuntimeRules is the short-form behavior contract returned by
	// aoci_rules.
	ContractRuntimeRules ID = "contracts/runtime-rules"
)

// Manifest describes embedded locales and their language-independent assets.
//
// OfficialLocales are production-selectable and must contain the complete asset
// set. DevelopmentLocales may contain any non-empty subset, but production Load
// rejects them until they are promoted to OfficialLocales.
type Manifest struct {
	Version            int             `json:"version"`
	DefaultLocale      string          `json:"default_locale"`
	OfficialLocales    []string        `json:"official_locales"`
	DevelopmentLocales []string        `json:"development_locales"`
	Assets             []ManifestAsset `json:"assets"`
}

// ManifestAsset describes one language-independent asset contract.
type ManifestAsset struct {
	ID             string              `json:"id"`
	Kind           string              `json:"kind"`
	Path           string              `json:"path"`
	Description    string              `json:"description"`
	UsedBy         string              `json:"used_by"`
	ProtocolTokens []string            `json:"protocol_tokens"`
	LocaleAnchors  map[string][]string `json:"locale_anchors,omitempty"`
	EnforcedBy     []string            `json:"enforced_by"`
	Variables      []string            `json:"variables,omitempty"`
}

// manifestFragment只允许功能域片段声明版本与资产。Locale生命周期只在基础
// Manifest中声明，避免每个片段形成一份会漂移的重复事实源。
type manifestFragment struct {
	Version int             `json:"version"`
	Assets  []ManifestAsset `json:"assets"`
}

// Embedded directories are included recursively by go:embed.
//
// Locale目录使用统一BCP-47语言-地区形态；开发中Locale首次加入少量资源时
// 会被通配模式自动嵌入，只有移入official_locales后才要求完整资源集合。
//
//go:embed manifest.json manifests/*.json [a-z][a-z]-[A-Z][A-Z]
var embeddedAssets embed.FS

// embeddedManifest parses and validates the immutable embedded metadata once.
// Custom fs and fault-injection paths continue to call readManifestFS directly.
func embeddedManifest() (Manifest, error) {
	embeddedManifestOnce.Do(func() {
		embeddedManifestData, embeddedManifestErr = readManifestFS(embeddedAssets)
		if embeddedManifestErr == nil {
			embeddedManifestErr = validateManifestMetadata(embeddedManifestData)
		}
	})
	return embeddedManifestData, embeddedManifestErr
}

// ReadManifest returns an isolated clone of the cached embedded metadata.
func ReadManifest() (Manifest, error) {
	manifest, err := embeddedManifest()
	if err != nil {
		return Manifest{}, err
	}
	return cloneManifest(manifest), nil
}

func cloneManifest(source Manifest) Manifest {
	cloned := source
	cloned.OfficialLocales = cloneStrings(source.OfficialLocales)
	cloned.DevelopmentLocales = cloneStrings(source.DevelopmentLocales)
	cloned.Assets = make([]ManifestAsset, len(source.Assets))
	for index, asset := range source.Assets {
		cloned.Assets[index] = asset
		cloned.Assets[index].ProtocolTokens = cloneStrings(asset.ProtocolTokens)
		cloned.Assets[index].EnforcedBy = cloneStrings(asset.EnforcedBy)
		cloned.Assets[index].Variables = cloneStrings(asset.Variables)
		cloned.Assets[index].LocaleAnchors = cloneStringMap(asset.LocaleAnchors)
	}

	return cloned
}

func cloneStrings(source []string) []string {
	if source == nil {
		return nil
	}

	return append([]string{}, source...)
}

func cloneStringMap(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}

	cloned := make(map[string][]string, len(source))
	for key, values := range source {
		cloned[key] = cloneStrings(values)
	}

	return cloned
}

// readManifestFS从指定文件系统读取并合并Manifest，供生产嵌入资源与故障注入
// 测试共用。结构与正文完整性由调用方按所需失败域分别校验。
func readManifestFS(assetFS fs.FS) (Manifest, error) {
	manifest, err := readBaseManifestFile(assetFS, baseManifestPath)
	if err != nil {
		return Manifest{}, err
	}

	entries, err := fs.ReadDir(assetFS, manifestFragmentsDir)
	if err != nil {
		return Manifest{}, fmt.Errorf(
			"read embedded text asset manifest fragments: %w",
			err,
		)
	}

	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			continue
		}

		fragmentPath := path.Join(manifestFragmentsDir, entry.Name())
		fragment, readErr := readManifestFragmentFile(assetFS, fragmentPath)
		if readErr != nil {
			return Manifest{}, readErr
		}
		if fragment.Version != manifest.Version {
			return Manifest{}, fmt.Errorf(
				"text asset manifest fragment %q version mismatch: got=%d want=%d",
				fragmentPath,
				fragment.Version,
				manifest.Version,
			)
		}

		manifest.Assets = append(manifest.Assets, fragment.Assets...)
	}

	return manifest, nil
}

func readBaseManifestFile(assetFS fs.FS, manifestPath string) (Manifest, error) {
	data, err := fs.ReadFile(assetFS, manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf(
			"read embedded text asset manifest %q: %w",
			manifestPath,
			err,
		)
	}

	manifest, err := decodeManifest(data)
	if err != nil {
		return Manifest{}, fmt.Errorf(
			"decode embedded text asset manifest %q: %w",
			manifestPath,
			err,
		)
	}

	return manifest, nil
}

func readManifestFragmentFile(assetFS fs.FS, manifestPath string) (manifestFragment, error) {
	data, err := fs.ReadFile(assetFS, manifestPath)
	if err != nil {
		return manifestFragment{}, fmt.Errorf(
			"read embedded text asset manifest fragment %q: %w",
			manifestPath,
			err,
		)
	}

	fragment, err := decodeManifestFragment(data)
	if err != nil {
		return manifestFragment{}, fmt.Errorf(
			"decode embedded text asset manifest fragment %q: %w",
			manifestPath,
			err,
		)
	}

	return fragment, nil
}

// Validate verifies the complete embedded release catalog. Production Load does
// not call this function; release gates and tests do.
func Validate() error {
	return ValidateForRelease()
}

// ValidateForRelease verifies complete official locales, every present
// development asset, the manifest/resource set, variables, protocol tokens and
// locale-specific content anchors.
func ValidateForRelease() error {
	manifest, err := readManifestFS(embeddedAssets)
	if err != nil {
		return err
	}

	return validateManifestFS(embeddedAssets, manifest)
}

// Load returns the exact embedded bytes for one production locale and asset ID.
//
// An empty locale resolves to the manifest default. The requested asset alone is
// validated; corruption in an unrelated resource body cannot block this call.
// Development locales are deliberately unavailable to production consumers.
func Load(locale string, id ID) (string, error) {
	manifest, err := embeddedManifest()
	if err != nil {
		return "", err
	}
	return loadManifestFS(embeddedAssets, manifest, locale, id)
}

// MustLoad is retained for test and migration compatibility. Production callers
// must use Load and propagate the returned, asset-local error.
func MustLoad(locale string, id ID) string {
	text, err := Load(locale, id)
	if err != nil {
		panic(err)
	}

	return text
}

func loadFS(assetFS fs.FS, locale string, id ID) (string, error) {
	manifest, err := readManifestFS(assetFS)
	if err != nil {
		return "", err
	}
	return loadManifestFS(assetFS, manifest, locale, id)
}

func loadManifestFS(assetFS fs.FS, manifest Manifest, locale string, id ID) (string, error) {
	if _, _, err := validateLocaleLifecycle(manifest); err != nil {
		return "", err
	}

	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = manifest.DefaultLocale
	}
	if !containsString(manifest.OfficialLocales, locale) {
		if containsString(manifest.DevelopmentLocales, locale) {
			return "", fmt.Errorf(
				"text asset locale %q is development-only and not production-selectable",
				locale,
			)
		}
		return "", fmt.Errorf("unsupported text asset locale %q", locale)
	}

	wantedID := strings.TrimSpace(string(id))
	if wantedID == "" {
		return "", fmt.Errorf("text asset id is empty")
	}

	matched, err := findAssetForLoad(manifest.Assets, wantedID)
	if err != nil {
		return "", err
	}
	if err := validateAssetMetadata(matched, manifest); err != nil {
		return "", fmt.Errorf("asset %q: %w", wantedID, err)
	}
	cleanPath, err := validateAssetPath(matched.Path)
	if err != nil {
		return "", fmt.Errorf("asset %q: %w", wantedID, err)
	}
	for _, other := range manifest.Assets {
		if other.ID != matched.ID && other.Path == cleanPath {
			return "", fmt.Errorf(
				"assets %q and %q declare duplicate path %q",
				matched.ID,
				other.ID,
				cleanPath,
			)
		}
	}

	data, err := fs.ReadFile(assetFS, path.Join(locale, cleanPath))
	if err != nil {
		return "", fmt.Errorf(
			"read text asset %q for locale %q: %w",
			wantedID,
			locale,
			err,
		)
	}
	// Embedded public contracts use LF as their runtime byte authority. Git may
	// materialize the source assets with CRLF on Windows, so normalize that
	// checkout representation before validation and rendering.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if err := validateLocalizedAssetText(locale, matched, text); err != nil {
		return "", err
	}

	return text, nil
}

func findAssetForLoad(assets []ManifestAsset, wantedID string) (ManifestAsset, error) {
	var matched *ManifestAsset
	for index := range assets {
		if assets[index].ID != wantedID {
			continue
		}
		if matched != nil {
			return ManifestAsset{}, fmt.Errorf(
				"text asset manifest contains duplicate asset id %q",
				wantedID,
			)
		}
		candidate := assets[index]
		matched = &candidate
	}
	if matched == nil {
		return ManifestAsset{}, fmt.Errorf("unknown text asset id %q", wantedID)
	}

	return *matched, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}

// validateAssetPath accepts only clean repository-relative slash paths.
func validateAssetPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("asset path is empty")
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("asset path must use forward slashes: %q", value)
	}

	cleaned := path.Clean(value)
	if cleaned == "." || path.IsAbs(cleaned) || cleaned != value ||
		strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("asset path is not a clean relative path: %q", value)
	}

	return cleaned, nil
}
