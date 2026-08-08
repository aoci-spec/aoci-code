package cognition

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var volumeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ParseProjectedRoot validates a D2 Root candidate. It is intentionally
// separate from the active Volumes v1 loader: D2 descriptors add state while
// the existing on-disk reader remains byte-compatible with its five-field
// contract until a later, explicitly approved layout activation phase.
func ParseProjectedRoot(raw []byte) ([]Descriptor, []Finding) {
	findings := make([]Finding, 0)
	if !utf8.Valid(raw) {
		return nil, []Finding{{Code: "root_utf8_invalid", AssetID: "root", Message: "Root must be valid UTF-8"}}
	}
	findings = append(findings, validateRoot(raw)...)
	lines := splitLines(raw)
	seenID := map[string]int{}
	seenKind := map[string]int{}
	seenPath := map[string]int{}
	descriptors := make([]Descriptor, 0)
	wantedKeys := []string{"id", "kind", "path", "format", "depends", "state"}
	for lineIndex, line := range lines {
		if !strings.HasPrefix(line, volumeDeclarationPrefix) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, volumeDeclarationPrefix)))
		if len(fields) != len(wantedKeys) {
			findings = append(findings, Finding{Code: "volume_declaration_invalid", AssetID: "root", Line: lineIndex + 1, Message: "projected Volume declarations require exactly six ordered fields"})
			continue
		}
		values := make([]string, len(wantedKeys))
		valid := true
		for index, key := range wantedKeys {
			prefix := key + "="
			if !strings.HasPrefix(fields[index], prefix) || strings.TrimPrefix(fields[index], prefix) == "" {
				findings = append(findings, Finding{Code: "volume_declaration_invalid", AssetID: "root", Line: lineIndex + 1, Message: "projected Volume declaration fields have a fixed order and occur exactly once"})
				valid = false
				continue
			}
			values[index] = strings.TrimPrefix(fields[index], prefix)
		}
		if !valid {
			continue
		}
		descriptor := Descriptor{ID: values[0], Kind: values[1], Path: values[2], FormatVersion: values[3], State: values[5]}
		if values[4] != "-" {
			descriptor.DependsOn = strings.Split(values[4], ",")
		}
		if !volumeIDPattern.MatchString(descriptor.ID) {
			findings = append(findings, Finding{Code: "volume_id_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume id must use canonical lowercase kebab syntax"})
		}
		registration, known := registrationForKind(descriptor.Kind)
		if !known {
			findings = append(findings, Finding{Code: "volume_kind_unknown", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume kind is not present in the production registry"})
		} else if descriptor.ID != registration.ID || descriptor.Path != registration.Path || descriptor.FormatVersion != registration.FormatVersion {
			findings = append(findings, Finding{Code: "volume_descriptor_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume id, path, and format must match the registered kind"})
		}
		if normalized, err := afs.NormalizeRelPath(descriptor.Path); err != nil || normalized != descriptor.Path {
			findings = append(findings, Finding{Code: "volume_path_unsafe", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume path must be a normalized repository-relative path"})
		}
		if descriptor.State != machinecontract.CognitionVolumeEnabled && descriptor.State != machinecontract.CognitionVolumeDisabled {
			findings = append(findings, Finding{Code: "volume_state_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume state must be enabled or disabled"})
		}
		if !sort.StringsAreSorted(descriptor.DependsOn) || hasDuplicateStrings(descriptor.DependsOn) {
			findings = append(findings, Finding{Code: "volume_dependencies_invalid", AssetID: descriptor.ID, Line: lineIndex + 1, Message: "Volume dependencies must be sorted and unique"})
		}
		pathKey := strings.ToLower(strings.ReplaceAll(descriptor.Path, "\\", "/"))
		if previous := seenID[descriptor.ID]; previous != 0 {
			findings = append(findings, Finding{Code: "duplicate_volume_id", AssetID: descriptor.ID, Line: lineIndex + 1, Message: fmt.Sprintf("Volume id duplicates line %d", previous)})
		}
		if previous := seenKind[descriptor.Kind]; previous != 0 {
			findings = append(findings, Finding{Code: "duplicate_volume_kind", AssetID: descriptor.ID, Line: lineIndex + 1, Message: fmt.Sprintf("Volume kind duplicates line %d", previous)})
		}
		if previous := seenPath[pathKey]; previous != 0 {
			findings = append(findings, Finding{Code: "duplicate_volume_path", AssetID: descriptor.ID, Line: lineIndex + 1, Message: fmt.Sprintf("Volume path duplicates line %d under cross-platform case folding", previous)})
		}
		seenID[descriptor.ID] = lineIndex + 1
		seenKind[descriptor.Kind] = lineIndex + 1
		seenPath[pathKey] = lineIndex + 1
		descriptors = append(descriptors, descriptor)
	}
	if len(descriptors) == 0 {
		findings = append(findings, Finding{Code: "volume_declarations_missing", AssetID: "root", Message: "Root must declare at least the Meta Volume"})
	}
	findings = append(findings, validateProjectedDependencies(descriptors)...)
	return descriptors, findings
}

func validateProjectedDependencies(descriptors []Descriptor) []Finding {
	byID := make(map[string]Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		byID[descriptor.ID] = descriptor
	}
	findings := make([]Finding, 0)
	meta, exists := byID["meta"]
	if !exists {
		findings = append(findings, Finding{Code: "meta_not_declared", AssetID: "root", Message: "projected layout requires an explicit Meta Volume"})
	} else {
		if meta.State != machinecontract.CognitionVolumeEnabled {
			findings = append(findings, Finding{Code: "meta_not_enabled", AssetID: "meta", Message: "Meta must be enabled"})
		}
		if len(meta.DependsOn) != 0 {
			findings = append(findings, Finding{Code: "volume_dependencies_invalid", AssetID: "meta", Message: "Meta must declare depends=-"})
		}
	}
	for _, descriptor := range descriptors {
		registration, known := registrationForKind(descriptor.Kind)
		if known && strings.Join(descriptor.DependsOn, ",") != strings.Join(registration.RequiredDependencies, ",") {
			findings = append(findings, Finding{Code: "volume_dependencies_invalid", AssetID: descriptor.ID, Message: "Volume dependencies do not match its registry entry"})
		}
		for _, dependency := range descriptor.DependsOn {
			if _, present := byID[dependency]; !present {
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

func hasDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return true
		}
	}
	return false
}

// ValidateProjectedTargetPaths rejects an existing symlink, directory, device,
// or other non-regular candidate target. Missing paths are expected during
// planning and are not created or inspected recursively.
func ValidateProjectedTargetPaths(repositoryRoot string, descriptors []Descriptor) []Finding {
	findings := make([]Finding, 0)
	for _, descriptor := range descriptors {
		fullPath := filepath.Join(repositoryRoot, filepath.FromSlash(descriptor.Path))
		info, err := os.Lstat(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			findings = append(findings, Finding{Code: "volume_target_inspect_failed", AssetID: descriptor.ID, Message: err.Error()})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			findings = append(findings, Finding{Code: "volume_path_not_regular", AssetID: descriptor.ID, Message: "candidate Volume target must be absent or a regular non-symlink file"})
		}
	}
	return findings
}

// BuildProjectedSet creates and validates a CognitionSet from candidate bytes
// only. It performs no filesystem write and does not activate the layout.
func BuildProjectedSet(repositoryRoot string, rootRaw []byte, volumeRaw map[string][]byte) (*Set, []Finding) {
	descriptors, findings := ParseProjectedRoot(rootRaw)
	if len(findings) > 0 {
		return nil, findings
	}
	set := &Set{
		RepositoryRoot: repositoryRoot, LayoutMode: LayoutVolumesV1, LayoutVersion: "1",
		Root:    Asset{Descriptor: Descriptor{ID: "root", Kind: "root", Path: "aoci.txt", FormatVersion: "root-manifest-v1"}, State: AssetPresent, SHA256: digestBytes(rootRaw), Raw: append([]byte{}, rootRaw...)},
		Volumes: map[string]*Asset{},
	}
	for _, descriptor := range descriptors {
		set.DeclaredOrder = append(set.DeclaredOrder, descriptor.ID)
		raw, present := volumeRaw[descriptor.ID]
		asset := &Asset{Descriptor: descriptor, State: AssetInvalid}
		if !present {
			asset.Findings = append(asset.Findings, Finding{Code: "candidate_volume_missing", AssetID: descriptor.ID, Message: "candidate bytes are missing for a declared Volume"})
		} else {
			parseVolumeAssetBytes(repositoryRoot, asset, raw)
		}
		set.Volumes[descriptor.ID] = asset
		set.Errors = append(set.Errors, asset.Findings...)
		if descriptor.ID == "meta" {
			set.Meta = *asset
		}
	}
	for id := range volumeRaw {
		if set.Volumes[id] == nil {
			set.Errors = append(set.Errors, Finding{Code: "candidate_volume_undeclared", AssetID: id, Message: "candidate bytes were supplied for an undeclared Volume"})
		}
	}
	if len(set.Meta.Raw) > 0 {
		set.Errors = append(set.Errors, validateMetaContract(set.Meta.Raw)...)
		set.Errors = append(set.Errors, validateObjectDictionaries(set)...)
	}
	set.computeIdentities()
	if len(set.Errors) > 0 {
		return set, append(findings, set.Errors...)
	}
	return set, findings
}
