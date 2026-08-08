package cognition

import (
	"sort"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// VolumeRegistration is one production-enabled Volume kind. The registry is
// explicit: repository files never register kinds by naming convention.
type VolumeRegistration struct {
	Kind                       string   `json:"kind"`
	ID                         string   `json:"id"`
	Path                       string   `json:"path"`
	Marker                     string   `json:"marker"`
	FormatVersion              string   `json:"format_version"`
	CanonicalIdentityNamespace string   `json:"canonical_identity_namespace"`
	RequiredDependencies       []string `json:"required_dependencies"`
	AllowedScopes              []string `json:"allowed_scopes"`
	Parser                     string   `json:"parser"`
	Validator                  string   `json:"validator"`
	ProjectedValidator         string   `json:"projected_validator"`
	BaselineParticipation      string   `json:"baseline_participation"`
	BindingParticipation       string   `json:"binding_participation"`
	SecurityBoundary           string   `json:"security_boundary"`
}

// VolumeRegistryDocument is the deterministic machine representation of all
// enabled Volume kinds.
type VolumeRegistryDocument struct {
	Version string               `json:"version"`
	Entries []VolumeRegistration `json:"entries"`
}

var volumeRegistryEntries = []VolumeRegistration{
	{
		Kind: "meta", ID: "meta", Path: "aoci.meta.txt", Marker: MetaVolumeMarker,
		FormatVersion: "meta-v1", CanonicalIdentityNamespace: "meta:",
		RequiredDependencies: []string{}, AllowedScopes: []string{ScopeAll, ScopeProject, ScopeMeta, ScopeCode, ScopeDatabase},
		Parser: "meta-v1", Validator: "meta-v1", ProjectedValidator: "meta-v1",
		BaselineParticipation: "asset", BindingParticipation: "dependency",
		SecurityBoundary: "model-authored protocol and tag dictionaries only; no objects or dynamic state",
	},
	{
		Kind: "code", ID: "code", Path: "aoci.code.txt", Marker: CodeVolumeMarker,
		FormatVersion: "object-fras-v2", CanonicalIdentityNamespace: "code:",
		RequiredDependencies: []string{"meta"}, AllowedScopes: []string{ScopeAll, ScopeCode},
		Parser: "code-object-fras-v2", Validator: "code-object-fras-v2", ProjectedValidator: "code-object-fras-v2",
		BaselineParticipation: "managed-source", BindingParticipation: "source-sha256",
		SecurityBoundary: "model-authored file FRAS only; repository-relative object identities",
	},
	{
		Kind: "database", ID: "database", Path: "aoci.database.txt", Marker: DatabaseMarker,
		FormatVersion: "table-fras-v2", CanonicalIdentityNamespace: "database://",
		RequiredDependencies: []string{"meta"}, AllowedScopes: []string{ScopeAll, ScopeDatabase},
		Parser: "database-table-fras-v2", Validator: "database-table-fras-v2", ProjectedValidator: "database-table-fras-v2",
		BaselineParticipation: "evidence-snapshot", BindingParticipation: "database-evidence-binding",
		SecurityBoundary: "model-authored table FRAS bound to local schema evidence; no row access",
	},
}

// VolumeRegistry returns a deep copy sorted by kind. Callers may serialize it
// directly without depending on map iteration order.
func VolumeRegistry() VolumeRegistryDocument {
	entries := make([]VolumeRegistration, len(volumeRegistryEntries))
	copy(entries, volumeRegistryEntries)
	for index := range entries {
		entries[index].RequiredDependencies = append([]string{}, entries[index].RequiredDependencies...)
		entries[index].AllowedScopes = append([]string{}, entries[index].AllowedScopes...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Kind < entries[j].Kind })
	return VolumeRegistryDocument{Version: machinecontract.CognitionVolumeRegistryV1, Entries: entries}
}

func registrationForKind(kind string) (VolumeRegistration, bool) {
	for _, registration := range volumeRegistryEntries {
		if registration.Kind == kind {
			return registration, true
		}
	}
	return VolumeRegistration{}, false
}

func canonicalDescriptorMap() map[string]Descriptor {
	result := make(map[string]Descriptor, len(volumeRegistryEntries))
	for _, registration := range volumeRegistryEntries {
		result[registration.ID] = Descriptor{
			ID: registration.ID, Kind: registration.Kind, Path: registration.Path,
			FormatVersion: registration.FormatVersion,
			DependsOn:     append([]string{}, registration.RequiredDependencies...),
		}
	}
	return result
}
