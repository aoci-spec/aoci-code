package cognition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const volumeDeclarationPrefix = "#Volume:"

var canonicalDescriptors = canonicalDescriptorMap()

// Load reads the configured aoci.txt once and selects a layout only by the
// exact Root marker. A damaged marker is an error, never a legacy fallback.
func Load(repositoryRoot, indexPath string) (*Set, error) {
	rootPath := filepath.Join(repositoryRoot, filepath.FromSlash(indexPath))
	raw, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, err
	}
	layout, detectErr := DetectLayout(raw)
	if detectErr != nil {
		finding := Finding{Code: "root_marker_invalid", AssetID: "root", Line: 1, Message: detectErr.Error()}
		return &Set{RepositoryRoot: repositoryRoot, LayoutMode: LayoutVolumesV1, LayoutVersion: "1", Errors: []Finding{finding}}, &ValidationError{Findings: []Finding{finding}}
	}
	switch layout {
	case LayoutVolumesV1:
		return loadVolumes(repositoryRoot, indexPath, raw)
	default:
		return loadLegacy(repositoryRoot, indexPath, raw)
	}
}

// DetectLayout classifies raw aoci.txt bytes without parsing either layout.
// It is used by write guards that must protect even a damaged/incomplete
// Volumes repository while preserving legacy initialization compatibility.
func DetectLayout(raw []byte) (string, error) {
	first := firstLineWithoutBOM(raw)
	if first == RootManifestMarker {
		return LayoutVolumesV1, nil
	}
	if strings.HasPrefix(first, "#AOCI-ROOT-MANIFEST") {
		return LayoutVolumesV1, errors.New("the Root marker is recognized but is not the exact Volumes v1 marker")
	}
	return LayoutLegacyMonolithic, nil
}

func loadLegacy(repositoryRoot, indexPath string, raw []byte) (*Set, error) {
	doc, warnings := index.Parse(string(raw))
	index.ResolveRelPaths(doc, repositoryRoot)
	asset := Asset{
		Descriptor: Descriptor{ID: "legacy", Kind: "monolithic", Path: indexPath, FormatVersion: "aoci-index-v1"},
		State:      AssetPresent, SHA256: digestBytes(raw), Raw: raw, Document: doc,
	}
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			asset.Objects = append(asset.Objects, Object{VolumeID: "legacy", Kind: "code", Name: entry.Filename, CanonicalRef: entry.RelPath, Entry: entry, CanonicalLine: entry.FullLine, SourceSection: section.HeaderLine, SourceLineNumber: entry.LineNo})
		}
	}
	asset.ObjectCount = len(asset.Objects)
	set := &Set{RepositoryRoot: repositoryRoot, LayoutMode: LayoutLegacyMonolithic, LayoutVersion: "1", Root: asset, Volumes: map[string]*Asset{}}
	for _, warning := range warnings {
		set.Warnings = append(set.Warnings, Finding{Code: "legacy_parse_warning", AssetID: "legacy", Line: warning.LineNo, Message: warning.Msg})
	}
	set.computeIdentities()
	return set, nil
}

func loadVolumes(repositoryRoot, indexPath string, rootRaw []byte) (*Set, error) {
	set := &Set{
		RepositoryRoot: repositoryRoot, LayoutMode: LayoutVolumesV1, LayoutVersion: "1",
		Root:    Asset{Descriptor: Descriptor{ID: "root", Kind: "root", Path: indexPath, FormatVersion: "root-manifest-v1"}, State: AssetPresent, SHA256: digestBytes(rootRaw), Raw: rootRaw},
		Volumes: map[string]*Asset{},
	}
	rootPath := filepath.Join(repositoryRoot, filepath.FromSlash(indexPath))
	if info, err := os.Lstat(rootPath); err != nil || !info.Mode().IsRegular() {
		message := "Volumes v1 Root must be a regular file and may not be a symlink"
		if err != nil {
			message = err.Error()
		}
		set.Errors = append(set.Errors, Finding{Code: "root_path_not_regular", AssetID: "root", Message: message})
	}
	if indexPath != "aoci.txt" {
		set.Errors = append(set.Errors, Finding{Code: "root_path_invalid", AssetID: "root", Message: "Volumes v1 Root must be the repository aoci.txt discovery entry"})
	}
	if !utf8.Valid(rootRaw) {
		set.Errors = append(set.Errors, Finding{Code: "root_utf8_invalid", AssetID: "root", Message: "Root must be valid UTF-8"})
	}
	set.Errors = append(set.Errors, validateRoot(rootRaw)...)
	descriptors, findings := parseRootDeclarations(rootRaw)
	set.Errors = append(set.Errors, findings...)
	for _, descriptor := range descriptors {
		set.DeclaredOrder = append(set.DeclaredOrder, descriptor.ID)
		asset := &Asset{Descriptor: descriptor, State: AssetInvalid}
		set.Volumes[descriptor.ID] = asset
	}
	if set.Volumes["meta"] == nil {
		set.Errors = append(set.Errors, Finding{Code: "meta_not_declared", AssetID: "root", Message: "Volumes v1 requires an explicit meta Volume declaration"})
	}
	set.Errors = append(set.Errors, validateDependencies(descriptors)...)
	// Declaration and Root validation is the security boundary for all later
	// file access. Never inspect an invalid descriptor, even though the final
	// result would still fail closed, because it may name a path outside the
	// repository.
	if len(set.Errors) > 0 {
		set.computeIdentities()
		return set, &ValidationError{Findings: set.Errors}
	}

	for _, descriptor := range descriptors {
		asset := set.Volumes[descriptor.ID]
		if descriptor.State == machinecontract.CognitionVolumeDisabled {
			asset.State = AssetAbsent
			continue
		}
		loadVolumeAsset(repositoryRoot, asset)
		set.Errors = append(set.Errors, asset.Findings...)
		if descriptor.ID == "meta" {
			set.Meta = *asset
		}
	}
	if len(set.Meta.Raw) > 0 {
		set.Errors = append(set.Errors, validateMetaContract(set.Meta.Raw)...)
		set.Errors = append(set.Errors, validateObjectDictionaries(set)...)
	}
	for _, id := range []string{"code", "database"} {
		if set.Volumes[id] != nil {
			continue
		}
		candidate := filepath.Join(repositoryRoot, filepath.FromSlash(canonicalDescriptors[id].Path))
		if info, err := os.Lstat(candidate); err == nil && !info.IsDir() {
			set.Warnings = append(set.Warnings, Finding{Code: "unmanaged_volume_candidate", AssetID: id, Message: canonicalDescriptors[id].Path + " exists but is not declared by the Root and was not loaded"})
		}
	}
	set.computeIdentities()
	if len(set.Errors) > 0 {
		return set, &ValidationError{Findings: set.Errors}
	}
	return set, nil
}

func parseRootDeclarations(raw []byte) ([]Descriptor, []Finding) {
	lines := splitLines(raw)
	seenID := map[string]bool{}
	seenPath := map[string]bool{}
	var descriptors []Descriptor
	var findings []Finding
	for lineIndex, line := range lines {
		if !strings.HasPrefix(line, volumeDeclarationPrefix) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, volumeDeclarationPrefix)))
		values := map[string]string{}
		allowedKeys := map[string]bool{"id": true, "kind": true, "path": true, "format": true, "depends": true, "state": true}
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 || !allowedKeys[parts[0]] || values[parts[0]] != "" {
				findings = append(findings, Finding{Code: "volume_declaration_invalid", AssetID: "root", Line: lineIndex + 1, Message: "Volume declarations use unique key=value fields"})
				continue
			}
			values[parts[0]] = parts[1]
		}
		descriptor := Descriptor{ID: values["id"], Kind: values["kind"], Path: values["path"], FormatVersion: values["format"], State: values["state"]}
		if len(fields) == 5 && descriptor.State == "" {
			// Existing Volumes v1 roots remain byte-compatible and are interpreted
			// as enabled. New roots use the six-field descriptor frozen by D2.
			descriptor.State = machinecontract.CognitionVolumeEnabled
		} else {
			wanted := []string{"id=", "kind=", "path=", "format=", "depends=", "state="}
			if len(fields) != len(wanted) {
				findings = append(findings, Finding{Code: "volume_declaration_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume declarations use the legacy five-field or canonical six-field form"})
			} else {
				for index, prefix := range wanted {
					if !strings.HasPrefix(fields[index], prefix) {
						findings = append(findings, Finding{Code: "volume_declaration_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "canonical Volume declaration fields have a fixed order"})
						break
					}
				}
			}
		}
		if descriptor.State != machinecontract.CognitionVolumeEnabled && descriptor.State != machinecontract.CognitionVolumeDisabled {
			findings = append(findings, Finding{Code: "volume_state_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume state must be enabled or disabled"})
		}
		if depends := values["depends"]; depends != "" && depends != "-" {
			descriptor.DependsOn = strings.Split(depends, ",")
		}
		canonical, known := canonicalDescriptors[descriptor.ID]
		if !known || descriptor.Kind != canonical.Kind || descriptor.Path != canonical.Path || descriptor.FormatVersion != canonical.FormatVersion {
			findings = append(findings, Finding{Code: "volume_descriptor_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "the descriptor must match the canonical Volumes v1 id, kind, path, and format"})
		}
		if normalized, err := afs.NormalizeRelPath(descriptor.Path); err != nil || normalized != descriptor.Path {
			findings = append(findings, Finding{Code: "volume_path_unsafe", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume path must be a normalized repository-relative path"})
		}
		pathKey := strings.ToLower(strings.ReplaceAll(descriptor.Path, "\\", "/"))
		if seenID[descriptor.ID] {
			findings = append(findings, Finding{Code: "duplicate_volume_id", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume id is declared more than once"})
		}
		if seenPath[pathKey] {
			findings = append(findings, Finding{Code: "duplicate_volume_path", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume path is declared more than once, using cross-platform case folding"})
		}
		seenID[descriptor.ID] = true
		seenPath[pathKey] = true
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, findings
}

func validateDependencies(descriptors []Descriptor) []Finding {
	byID := map[string]Descriptor{}
	var findings []Finding
	for _, descriptor := range descriptors {
		byID[descriptor.ID] = descriptor
	}
	if meta, exists := byID["meta"]; exists && meta.State != machinecontract.CognitionVolumeEnabled {
		findings = append(findings, Finding{Code: "meta_not_enabled", AssetID: "meta", Message: "Meta must be enabled"})
	}
	for _, descriptor := range descriptors {
		expected := canonicalDescriptors[descriptor.ID].DependsOn
		if strings.Join(descriptor.DependsOn, ",") != strings.Join(expected, ",") {
			findings = append(findings, Finding{Code: "volume_dependencies_invalid", AssetID: descriptor.ID, Message: "Volume dependencies do not match the Volumes v1 dependency closure"})
		}
		for _, dependency := range descriptor.DependsOn {
			if byID[dependency].ID == "" {
				findings = append(findings, Finding{Code: "volume_dependency_missing", AssetID: descriptor.ID, Message: "declared dependency " + dependency + " is absent"})
			}
		}
	}
	state := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		if state[id] == 1 {
			findings = append(findings, Finding{Code: "volume_dependency_cycle", AssetID: id, Message: "Volume dependency graph contains a cycle"})
			return
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		for _, dependency := range byID[id].DependsOn {
			visit(dependency)
		}
		state[id] = 2
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		visit(id)
	}
	return findings
}

func loadVolumeAsset(repositoryRoot string, asset *Asset) {
	if asset.Descriptor.ID == "" {
		return
	}
	fullPath := filepath.Join(repositoryRoot, filepath.FromSlash(asset.Descriptor.Path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		code := "volume_read_failed"
		if os.IsNotExist(err) {
			code = "declared_volume_missing"
		}
		asset.Findings = append(asset.Findings, Finding{Code: code, AssetID: asset.Descriptor.ID, Message: err.Error()})
		return
	}
	if !info.Mode().IsRegular() {
		asset.Findings = append(asset.Findings, Finding{Code: "volume_path_not_regular", AssetID: asset.Descriptor.ID, Message: "declared Volume must be a regular file and may not be a symlink"})
		return
	}
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		asset.Findings = append(asset.Findings, Finding{Code: "volume_read_failed", AssetID: asset.Descriptor.ID, Message: err.Error()})
		return
	}
	parseVolumeAssetBytes(repositoryRoot, asset, raw)
}

// ValidateProjectedCodeVolume validates an in-memory replacement for the
// already declared Code Volume. It shares the loader's marker, object, FRAS,
// boundary, and Meta dictionary checks and never writes the candidate to disk.
func ValidateProjectedCodeVolume(set *Set, raw []byte) []Finding {
	return ValidateProjectedObjectVolume(set, "code", raw)
}

// ValidateProjectedObjectVolume validates an in-memory replacement for one
// already declared object Volume. The candidate is parsed with the same
// marker, FRAS, boundary, and scoped Meta dictionary rules as a normal load.
func ValidateProjectedObjectVolume(set *Set, volumeID string, raw []byte) []Finding {
	if set == nil || set.LayoutMode != LayoutVolumesV1 {
		return []Finding{{Code: "projected_layout_invalid", AssetID: volumeID, Message: "projected object Volume validation requires a Volumes v1 CognitionSet"}}
	}
	if volumeID != "code" && volumeID != "database" {
		return []Finding{{Code: "projected_volume_unsupported", AssetID: volumeID, Message: "projected validation supports only Code and Database object Volumes"}}
	}
	current := set.Volumes[volumeID]
	if current == nil || current.State != AssetPresent {
		return []Finding{{Code: "projected_volume_absent", AssetID: volumeID, Message: "projected validation requires an existing valid object Volume"}}
	}
	projectedAsset := &Asset{Descriptor: current.Descriptor, State: AssetInvalid}
	parseVolumeAssetBytes(set.RepositoryRoot, projectedAsset, raw)

	projectedSet := *set
	projectedSet.Volumes = make(map[string]*Asset, len(set.Volumes))
	for id, asset := range set.Volumes {
		projectedSet.Volumes[id] = asset
	}
	projectedSet.Volumes[volumeID] = projectedAsset
	findings := append([]Finding{}, projectedAsset.Findings...)
	if len(findings) == 0 {
		findings = append(findings, validateObjectDictionaries(&projectedSet)...)
	}
	return findings
}

func parseVolumeAssetBytes(repositoryRoot string, asset *Asset, raw []byte) {
	asset.Raw = raw
	asset.SHA256 = digestBytes(raw)
	if !utf8.Valid(raw) {
		asset.Findings = append(asset.Findings, Finding{Code: "volume_utf8_invalid", AssetID: asset.Descriptor.ID, Message: "Volume must be valid UTF-8"})
		return
	}
	expectedMarker := map[string]string{"meta": MetaVolumeMarker, "code": CodeVolumeMarker, "database": DatabaseMarker}[asset.Descriptor.Kind]
	if firstLineWithoutBOM(raw) != expectedMarker {
		asset.Findings = append(asset.Findings, Finding{Code: "volume_marker_kind_mismatch", AssetID: asset.Descriptor.ID, Line: 1, Message: "Volume marker does not match its declared kind"})
		return
	}
	if countExactLine(raw, expectedMarker) != 1 {
		asset.Findings = append(asset.Findings, Finding{Code: "volume_marker_duplicate", AssetID: asset.Descriptor.ID, Message: "Volume marker must appear exactly once"})
	}
	switch asset.Descriptor.Kind {
	case "meta":
		asset.Findings = append(asset.Findings, validateMeta(raw)...)
	case "code":
		document, objects, findings := parseCodeVolume(repositoryRoot, raw)
		asset.Document = document
		asset.Objects = objects
		asset.Findings = append(asset.Findings, findings...)
		asset.Findings = append(asset.Findings, validateObjectVolumeBoundary(raw, "code")...)
	case "database":
		objects, findings := parseDatabaseVolume(raw)
		asset.Objects = objects
		asset.Findings = append(asset.Findings, findings...)
		asset.Findings = append(asset.Findings, validateObjectVolumeBoundary(raw, "database")...)
	default:
		asset.Findings = append(asset.Findings, Finding{Code: "volume_kind_unsupported", AssetID: asset.Descriptor.ID, Message: "unsupported Volumes v1 kind"})
	}
	asset.ObjectCount = len(asset.Objects)
	if len(asset.Findings) == 0 {
		asset.State = AssetPresent
	}
}

func validateMeta(raw []byte) []Finding {
	var findings []Finding
	for lineIndex, line := range splitLines(raw) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") {
			findings = append(findings, Finding{Code: "meta_contains_object_section", AssetID: "meta", Line: lineIndex + 1, Message: "Meta may not contain business object sections"})
		}
		if _, ok := index.ParseEntryLine(line, lineIndex+1); ok && !strings.HasPrefix(trimmed, "#") {
			findings = append(findings, Finding{Code: "meta_contains_business_entry", AssetID: "meta", Line: lineIndex + 1, Message: "Meta may not contain business Entries"})
		}
		if strings.HasPrefix(trimmed, volumeDeclarationPrefix) || hasForbiddenDynamicPrefix(trimmed) {
			findings = append(findings, Finding{Code: "meta_contains_layout_or_dynamic_state", AssetID: "meta", Line: lineIndex + 1, Message: "Meta may not contain Volume declarations or dynamic repository state"})
		}
	}
	return findings
}

func validateRoot(raw []byte) []Finding {
	var findings []Finding
	if countExactLine(raw, RootManifestMarker) != 1 {
		findings = append(findings, Finding{Code: "root_marker_duplicate", AssetID: "root", Message: "Root marker must appear exactly once"})
	}
	if !hasExactLine(raw, "#Format-Version: cognition-volumes/v1") {
		findings = append(findings, Finding{Code: "root_format_version_missing", AssetID: "root", Message: "Root must declare #Format-Version: cognition-volumes/v1"})
	}
	if locale, explicit, err := index.DetectLocale(string(raw)); err != nil || !explicit || locale == "" {
		findings = append(findings, Finding{Code: "root_locale_invalid", AssetID: "root", Message: "Root must declare one supported Locale"})
	}
	for lineIndex, line := range splitLines(raw) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") {
			findings = append(findings, Finding{Code: "root_contains_object_section", AssetID: "root", Line: lineIndex + 1, Message: "Root may not contain object sections"})
		}
		if _, ok := index.ParseEntryLine(line, lineIndex+1); ok && !strings.HasPrefix(trimmed, "#") {
			findings = append(findings, Finding{Code: "root_contains_business_entry", AssetID: "root", Line: lineIndex + 1, Message: "Root may not contain business Entries"})
		}
		if strings.HasPrefix(trimmed, "#[Tag dictionary:") || strings.HasPrefix(trimmed, "#Object-Protocol:") || strings.HasPrefix(trimmed, "#FRAS-Discipline:") || strings.HasPrefix(trimmed, "#S-Admission:") {
			findings = append(findings, Finding{Code: "root_contains_meta_authority", AssetID: "root", Line: lineIndex + 1, Message: "Root may not contain Meta protocol or tag-dictionary authority"})
		}
		if hasForbiddenDynamicPrefix(trimmed) {
			findings = append(findings, Finding{Code: "root_contains_dynamic_state", AssetID: "root", Line: lineIndex + 1, Message: "Root may not persist dynamic identity, count, alignment, session, receipt, Git, or environment state"})
		}
	}
	return findings
}

func validateMetaContract(raw []byte) []Finding {
	required := []string{
		"#Object-Protocol: repository-cognition-object/v2",
		"#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract",
		"#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]",
		"#[Tag dictionary: database]",
	}
	var findings []Finding
	for _, marker := range required {
		if !hasExactLine(raw, marker) {
			findings = append(findings, Finding{Code: "meta_contract_incomplete", AssetID: "meta", Message: "Meta is missing required contract marker " + marker})
		}
	}
	return findings
}

func validateObjectVolumeBoundary(raw []byte, assetID string) []Finding {
	var findings []Finding
	for lineIndex, line := range splitLines(raw) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || lineIndex == 0 {
			continue
		}
		if assetID == "database" && strings.HasPrefix(trimmed, "#") {
			findings = append(findings, Finding{Code: "database_contains_non_object_content", AssetID: assetID, Line: lineIndex + 1, Message: "Database Volume contains only its marker, namespace sections, and table Entries"})
			continue
		}
		for _, forbidden := range []string{RootManifestMarker, MetaVolumeMarker, CodeVolumeMarker, DatabaseMarker, "#[Tag dictionary:", "#Object-Protocol:", "#FRAS-Discipline:", "#S-Admission:", volumeDeclarationPrefix} {
			if strings.HasPrefix(trimmed, forbidden) {
				findings = append(findings, Finding{Code: "object_volume_contains_copied_authority", AssetID: assetID, Line: lineIndex + 1, Message: "Object Volumes may not copy Root declarations or Meta authority"})
				break
			}
		}
	}
	return findings
}

func hasExactLine(raw []byte, want string) bool {
	return countExactLine(raw, want) > 0
}

func countExactLine(raw []byte, want string) int {
	count := 0
	for _, line := range splitLines(raw) {
		if strings.TrimSpace(line) == want {
			count++
		}
	}
	return count
}

func hasForbiddenDynamicPrefix(line string) bool {
	for _, prefix := range []string{
		"#Root-SHA256:", "#Meta-SHA256:", "#Volume-SHA256:", "#Composite-SHA256:",
		"#Composite-Identity:", "#Scope-Identity:", "#Entry-Count:", "#Object-Count:",
		"#Asset-State:", "#Alignment-State:", "#Missing:", "#Stale:", "#Generated-At:",
		"#Git-HEAD:", "#Receipt:", "#MCP-Session:", "#DSN:", "#Password:", "#Credential:", "#Environment:",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func validateObjectDictionaries(set *Set) []Finding {
	profiles := map[string]*index.TagDict{
		"code":     index.ExtractScopedTagDict(string(set.Meta.Raw), "code"),
		"database": index.ExtractScopedTagDict(string(set.Meta.Raw), "database"),
	}
	var findings []Finding
	for _, id := range []string{"code", "database"} {
		asset := set.Volumes[id]
		if asset == nil {
			continue
		}
		dictionary := profiles[id]
		if dictionary == nil {
			findings = append(findings, Finding{Code: "meta_tag_dictionary_missing", AssetID: "meta", Message: "Meta lacks the " + id + " tag dictionary section"})
			continue
		}
		if problems := dictionary.ObjectContractProblems(); len(problems) > 0 {
			code := "meta_tag_dictionary_invalid"
			for _, problem := range problems {
				if strings.Contains(problem, "state=conflict") {
					code = "meta_tag_dictionary_conflict"
					break
				}
			}
			findings = append(findings, Finding{Code: code, AssetID: "meta", Message: id + " tag dictionary is unusable: " + strings.Join(problems, "|")})
			continue
		}
		for _, object := range asset.Objects {
			violations := index.ValidateTagAgainstDict(object.Entry.TagsParsed, object.Entry.TagsRaw, dictionary)
			if len(violations) > 0 {
				findings = append(findings, Finding{Code: "object_tag_dictionary_violation", AssetID: id, Line: object.SourceLineNumber, Message: violations[0].Cause})
			}
		}
	}
	return findings
}

func firstLineWithoutBOM(raw []byte) string {
	lines := splitLines(raw)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimPrefix(lines[0], "\ufeff")
}

func splitLines(raw []byte) []string {
	return strings.Split(strings.ReplaceAll(strings.TrimPrefix(string(raw), "\ufeff"), "\r\n", "\n"), "\n")
}

func (set *Set) Scope(requested string) (ScopeView, error) {
	scope := strings.ToLower(strings.TrimSpace(requested))
	if scope == "" {
		scope = ScopeAll
	}
	if scope != ScopeAll && scope != ScopeProject && scope != ScopeMeta && scope != ScopeCode && scope != ScopeDatabase {
		return ScopeView{}, fmt.Errorf("unsupported cognition scope %q", requested)
	}
	if set.LayoutMode == LayoutLegacyMonolithic {
		if scope == ScopeDatabase {
			absent := set.assetOrAbsent("database")
			return ScopeView{RequestedScope: scope, EffectiveScope: scope, Available: false, AssetState: AssetAbsent, Assets: []*Asset{absent}, ScopeIdentity: set.scopeIdentity(scope, []*Asset{absent})}, nil
		}
		assets := []*Asset{&set.Root}
		return ScopeView{RequestedScope: scope, EffectiveScope: ScopeAll, Available: true, AssetState: AssetPresent, Assets: assets, ObjectCount: set.Root.ObjectCount, ScopeIdentity: set.scopeIdentity(scope, assets)}, nil
	}
	assets := []*Asset{&set.Root, &set.Meta}
	view := ScopeView{RequestedScope: scope, EffectiveScope: scope, Available: true, AssetState: AssetPresent}
	if scope == ScopeCode || scope == ScopeDatabase {
		asset := set.Volumes[scope]
		if asset == nil {
			view.Available = false
			view.AssetState = AssetAbsent
			view.Assets = assets
			view.ScopeIdentity = set.scopeIdentity(scope, assets)
			return view, nil
		}
		assets = append(assets, asset)
	} else if scope == ScopeAll {
		for _, id := range []string{"code", "database"} {
			if asset := set.Volumes[id]; asset != nil {
				assets = append(assets, asset)
			}
		}
	}
	for _, asset := range assets {
		view.ObjectCount += asset.ObjectCount
	}
	view.Assets = assets
	view.ScopeIdentity = set.scopeIdentity(scope, assets)
	return view, nil
}
