package cognition

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

const (
	SystemRelationProjectionV1 = "system-relation-projection/v1"
	CognitionLineageV1         = "cognition-lineage/v1"
	DatabaseImpactV1           = "database-to-code-impact/v1"
	CognitionSnapshotV1        = "cognition-evolution-snapshot/v1"
	CognitionEvolutionV1       = "cognition-evolution/v1"
)

type SystemRelation struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`
	Authority string `json:"authority"`
}

type LineageBinding struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	SHA256    string `json:"sha256"`
	Authority string `json:"authority"`
}

type LineageRecord struct {
	Version      string           `json:"version"`
	ObjectRef    string           `json:"object_ref"`
	Domain       string           `json:"domain"`
	VolumeID     string           `json:"volume_id"`
	ObjectSHA256 string           `json:"object_sha256"`
	VolumeSHA256 string           `json:"volume_sha256"`
	Status       string           `json:"status"`
	Bindings     []LineageBinding `json:"bindings"`
}

type SystemProjection struct {
	Version            string           `json:"version"`
	LineageVersion     string           `json:"lineage_version"`
	Derived            bool             `json:"derived"`
	Authoritative      bool             `json:"authoritative"`
	Authorities        []string         `json:"authorities"`
	LayoutMode         string           `json:"layout_mode"`
	CompositeIdentity  string           `json:"composite_identity"`
	ProjectionIdentity string           `json:"projection_identity"`
	Relations          []SystemRelation `json:"relations"`
	Lineage            []LineageRecord  `json:"lineage"`
	Findings           []Finding        `json:"findings"`
	NetworkAccessed    bool             `json:"network_accessed"`
	BusinessDataRead   bool             `json:"business_data_read"`
}

// BuildSystemProjection derives a narrow relation and lineage view from the
// current CognitionSet and existing Baseline. It has no persistence or Apply
// behavior and therefore cannot become a second fact source.
func BuildSystemProjection(set *Set, state *baseline.Baseline) (*SystemProjection, error) {
	if set == nil || set.CompositeIdentity == "" || len(set.Errors) != 0 {
		return nil, fmt.Errorf("system_projection_cognition_invalid")
	}
	projection := &SystemProjection{
		Version: SystemRelationProjectionV1, LineageVersion: CognitionLineageV1,
		Derived: true, Authoritative: false,
		Authorities: []string{"cognition_volume", "schema_evidence_binding", "baseline", "receipt_bound_identity"},
		LayoutMode:  set.LayoutMode, CompositeIdentity: set.CompositeIdentity,
		Relations: []SystemRelation{}, Lineage: []LineageRecord{}, Findings: []Finding{},
		NetworkAccessed: false, BusinessDataRead: false,
	}
	registry := currentObjectRegistry(set)
	for _, ref := range sortedObjectRefs(registry) {
		object := registry[ref]
		volumeSHA := set.Root.SHA256
		if set.LayoutMode == LayoutVolumesV1 && set.Volumes[object.VolumeID] != nil {
			volumeSHA = set.Volumes[object.VolumeID].SHA256
		}
		projection.Relations = append(projection.Relations, SystemRelation{
			From: "volume:" + object.VolumeID, To: ref, Kind: "contains", Authority: "cognition_volume",
		})
		projection.Lineage = append(projection.Lineage, buildLineageRecord(set, state, object, volumeSHA))
	}
	if set.LayoutMode == LayoutVolumesV1 {
		for _, id := range set.DeclaredOrder {
			asset := set.Volumes[id]
			if asset == nil {
				continue
			}
			for _, dependency := range asset.Descriptor.DependsOn {
				projection.Relations = append(projection.Relations, SystemRelation{
					From: "volume:" + id, To: "volume:" + dependency, Kind: "depends_on", Authority: "root_manifest",
				})
			}
		}
	}
	appendExplicitRelations(projection, registry)
	sort.Slice(projection.Relations, func(i, j int) bool {
		left, right := projection.Relations[i], projection.Relations[j]
		if left.From != right.From {
			return left.From < right.From
		}
		if left.To != right.To {
			return left.To < right.To
		}
		return left.Kind < right.Kind
	})
	sort.Slice(projection.Lineage, func(i, j int) bool { return projection.Lineage[i].ObjectRef < projection.Lineage[j].ObjectRef })
	sort.Slice(projection.Findings, func(i, j int) bool {
		if projection.Findings[i].AssetID != projection.Findings[j].AssetID {
			return projection.Findings[i].AssetID < projection.Findings[j].AssetID
		}
		return projection.Findings[i].Code < projection.Findings[j].Code
	})
	identityValue := *projection
	identityValue.ProjectionIdentity = ""
	data, err := json.Marshal(identityValue)
	if err != nil {
		return nil, err
	}
	projection.ProjectionIdentity = digestBytes(data)
	return projection, nil
}

func currentObjectRegistry(set *Set) impactRegistry {
	result := impactRegistry{}
	if set.LayoutMode == LayoutLegacyMonolithic {
		for _, object := range set.Root.Objects {
			result[object.CanonicalRef] = object
		}
		return result
	}
	for _, id := range set.DeclaredOrder {
		asset := set.Volumes[id]
		if asset == nil || asset.State != AssetPresent {
			continue
		}
		for _, object := range asset.Objects {
			result[object.CanonicalRef] = object
		}
	}
	return result
}

func buildLineageRecord(set *Set, state *baseline.Baseline, object Object, volumeSHA string) LineageRecord {
	record := LineageRecord{Version: CognitionLineageV1, ObjectRef: object.CanonicalRef,
		Domain: object.VolumeID, VolumeID: object.VolumeID, ObjectSHA256: digestBytes([]byte(object.CanonicalLine)),
		VolumeSHA256: volumeSHA, Status: "unbaselined", Bindings: []LineageBinding{{
			Kind: "formal_volume", Reference: "volume:" + object.VolumeID, SHA256: volumeSHA, Authority: "cognition_volume",
		}}}
	if object.VolumeID == ScopeDatabase {
		binding, exists := baseline.FindDatabaseCognitionBinding(state, object.CanonicalRef)
		if !exists {
			return record
		}
		record.Bindings = append(record.Bindings, LineageBinding{
			Kind: "schema_evidence", Reference: object.CanonicalRef, SHA256: binding.TableEvidenceSHA256,
			Authority: "database_cognition_binding",
		})
		if binding.EntrySHA256 == record.ObjectSHA256 {
			record.Status = "current"
		} else {
			record.Status = "stale"
		}
		return record
	}
	path := strings.TrimPrefix(object.CanonicalRef, "code:")
	if state == nil {
		return record
	}
	fingerprint, exists := state.Files[path]
	if !exists {
		return record
	}
	record.Bindings = append(record.Bindings, LineageBinding{Kind: "code_source", Reference: path,
		SHA256: fingerprint.SHA256, Authority: "baseline"})
	current, err := baseline.HashFile(filepath.Join(set.RepositoryRoot, filepath.FromSlash(path)))
	if err != nil || current.SHA256 != fingerprint.SHA256 {
		record.Status = "stale"
	} else {
		record.Status = "current"
	}
	return record
}

func appendExplicitRelations(projection *SystemProjection, registry impactRegistry) {
	nameIndex, namespaceIndex := buildImpactNameIndexes(registry)
	for _, sourceRef := range sortedObjectRefs(registry) {
		source := registry[sourceRef]
		tokens, invalid := splitImpactRelations(source.Entry)
		for _, token := range invalid {
			projection.Findings = append(projection.Findings, Finding{Code: "relation_invalid", AssetID: sourceRef,
				Message: fmt.Sprintf("relation %q does not use the canonical relation grammar", token)})
		}
		for _, token := range tokens {
			targets, issue := resolveImpactRelation(source, token, registry, nameIndex, namespaceIndex)
			if issue != "" {
				projection.Findings = append(projection.Findings, Finding{Code: issue, AssetID: sourceRef,
					Message: fmt.Sprintf("relation %q cannot be projected unambiguously", token)})
				continue
			}
			projection.Relations = append(projection.Relations, SystemRelation{
				From: sourceRef, To: targets[0], Kind: "cognition_relation", Authority: "model_authored_R",
			})
		}
	}
}

type DatabaseImpactObject struct {
	ObjectRef string   `json:"object_ref"`
	Distance  int      `json:"distance"`
	Path      []string `json:"path"`
	Reason    string   `json:"reason"`
}

type DatabaseImpact struct {
	Version             string                 `json:"version"`
	DatabaseObjectRef   string                 `json:"database_object_ref"`
	ProjectionIdentity  string                 `json:"projection_identity"`
	Complete            bool                   `json:"complete"`
	AffectedCodeObjects []DatabaseImpactObject `json:"affected_code_objects"`
	Findings            []Finding              `json:"findings"`
	Derived             bool                   `json:"derived"`
	NetworkAccessed     bool                   `json:"network_accessed"`
}

// ResolveDatabaseImpact follows only explicit model-authored Cognition R
// relations. It never infers dependencies from paths, SQL, imports, or names.
func ResolveDatabaseImpact(projection *SystemProjection, databaseObjectRef string) (*DatabaseImpact, error) {
	if projection == nil || projection.Version != SystemRelationProjectionV1 || !IsCanonicalDatabaseRef(databaseObjectRef) {
		return nil, fmt.Errorf("database_impact_input_invalid")
	}
	known := false
	adjacent := map[string][]string{}
	for _, relation := range projection.Relations {
		if relation.Kind == "contains" && relation.To == databaseObjectRef {
			known = true
		}
		if relation.Kind != "cognition_relation" {
			continue
		}
		adjacent[relation.From] = append(adjacent[relation.From], relation.To)
		adjacent[relation.To] = append(adjacent[relation.To], relation.From)
	}
	if !known {
		return nil, fmt.Errorf("database_impact_object_not_managed")
	}
	for ref := range adjacent {
		sort.Strings(adjacent[ref])
	}
	type visit struct {
		ref  string
		path []string
	}
	queue := []visit{{ref: databaseObjectRef, path: []string{databaseObjectRef}}}
	seen := map[string]bool{databaseObjectRef: true}
	affected := []DatabaseImpactObject{}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current.ref] {
			if seen[next] {
				continue
			}
			seen[next] = true
			path := append(append([]string{}, current.path...), next)
			queue = append(queue, visit{ref: next, path: path})
			if strings.HasPrefix(next, "code:") {
				reason := "explicit_relation"
				if len(path) > 2 {
					reason = "transitive_explicit_relation"
				}
				affected = append(affected, DatabaseImpactObject{ObjectRef: next, Distance: len(path) - 1, Path: path, Reason: reason})
			}
		}
	}
	sort.Slice(affected, func(i, j int) bool { return affected[i].ObjectRef < affected[j].ObjectRef })
	return &DatabaseImpact{Version: DatabaseImpactV1, DatabaseObjectRef: databaseObjectRef,
		ProjectionIdentity: projection.ProjectionIdentity, Complete: len(projection.Findings) == 0,
		AffectedCodeObjects: affected, Findings: append([]Finding{}, projection.Findings...), Derived: true, NetworkAccessed: false}, nil
}

type CognitionSnapshotObject struct {
	ObjectRef      string `json:"object_ref"`
	Domain         string `json:"domain"`
	ObjectSHA256   string `json:"object_sha256"`
	SourceSHA256   string `json:"source_sha256,omitempty"`
	EvidenceSHA256 string `json:"evidence_sha256,omitempty"`
}

type CognitionSnapshot struct {
	Version            string                    `json:"version"`
	LayoutMode         string                    `json:"layout_mode"`
	CompositeIdentity  string                    `json:"composite_identity"`
	ProjectionIdentity string                    `json:"projection_identity"`
	Objects            []CognitionSnapshotObject `json:"objects"`
	Derived            bool                      `json:"derived"`
}

func SnapshotSystemCognition(projection *SystemProjection) (*CognitionSnapshot, error) {
	if projection == nil || projection.Version != SystemRelationProjectionV1 {
		return nil, fmt.Errorf("cognition_snapshot_projection_invalid")
	}
	result := &CognitionSnapshot{Version: CognitionSnapshotV1, LayoutMode: projection.LayoutMode,
		CompositeIdentity: projection.CompositeIdentity, ProjectionIdentity: projection.ProjectionIdentity,
		Objects: []CognitionSnapshotObject{}, Derived: true}
	for _, record := range projection.Lineage {
		object := CognitionSnapshotObject{ObjectRef: record.ObjectRef, Domain: record.Domain, ObjectSHA256: record.ObjectSHA256}
		for _, binding := range record.Bindings {
			switch binding.Kind {
			case "code_source":
				object.SourceSHA256 = binding.SHA256
			case "schema_evidence":
				object.EvidenceSHA256 = binding.SHA256
			}
		}
		result.Objects = append(result.Objects, object)
	}
	return result, nil
}

type CognitionEvolutionItem struct {
	ObjectRef      string `json:"object_ref"`
	Domain         string `json:"domain"`
	Change         string `json:"change"`
	PreviousSHA256 string `json:"previous_sha256,omitempty"`
	CurrentSHA256  string `json:"current_sha256,omitempty"`
}

type CognitionEvolutionSummary struct {
	Created         int `json:"created"`
	Removed         int `json:"removed"`
	SemanticChanged int `json:"semantic_changed"`
	LineageChanged  int `json:"lineage_changed"`
	Unchanged       int `json:"unchanged"`
}

type CognitionEvolution struct {
	Version                    string                    `json:"version"`
	PreviousCompositeIdentity  string                    `json:"previous_composite_identity"`
	CurrentCompositeIdentity   string                    `json:"current_composite_identity"`
	PreviousProjectionIdentity string                    `json:"previous_projection_identity"`
	CurrentProjectionIdentity  string                    `json:"current_projection_identity"`
	SystemChanged              bool                      `json:"system_changed"`
	Summary                    CognitionEvolutionSummary `json:"summary"`
	Changes                    []CognitionEvolutionItem  `json:"changes"`
	Derived                    bool                      `json:"derived"`
}

// CompareCognitionSnapshots compares an explicit prior observation with the
// current derived observation. Neither snapshot is persisted or authoritative.
func CompareCognitionSnapshots(previous, current *CognitionSnapshot) (*CognitionEvolution, error) {
	if previous == nil || current == nil || previous.Version != CognitionSnapshotV1 || current.Version != CognitionSnapshotV1 {
		return nil, fmt.Errorf("cognition_evolution_snapshot_invalid")
	}
	before, after := map[string]CognitionSnapshotObject{}, map[string]CognitionSnapshotObject{}
	for _, object := range previous.Objects {
		if _, exists := before[object.ObjectRef]; exists {
			return nil, fmt.Errorf("cognition_evolution_snapshot_duplicate")
		}
		before[object.ObjectRef] = object
	}
	for _, object := range current.Objects {
		if _, exists := after[object.ObjectRef]; exists {
			return nil, fmt.Errorf("cognition_evolution_snapshot_duplicate")
		}
		after[object.ObjectRef] = object
	}
	refs := map[string]bool{}
	for ref := range before {
		refs[ref] = true
	}
	for ref := range after {
		refs[ref] = true
	}
	ordered := make([]string, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)
	result := &CognitionEvolution{Version: CognitionEvolutionV1,
		PreviousCompositeIdentity: previous.CompositeIdentity, CurrentCompositeIdentity: current.CompositeIdentity,
		PreviousProjectionIdentity: previous.ProjectionIdentity, CurrentProjectionIdentity: current.ProjectionIdentity,
		SystemChanged: previous.ProjectionIdentity != current.ProjectionIdentity,
		Changes:       []CognitionEvolutionItem{}, Derived: true}
	for _, ref := range ordered {
		old, hadOld := before[ref]
		newValue, hasNew := after[ref]
		item := CognitionEvolutionItem{ObjectRef: ref, Domain: newValue.Domain,
			PreviousSHA256: old.ObjectSHA256, CurrentSHA256: newValue.ObjectSHA256}
		switch {
		case !hadOld:
			item.Change = "created"
			result.Summary.Created++
		case !hasNew:
			item.Domain = old.Domain
			item.Change = "removed"
			result.Summary.Removed++
		case old.ObjectSHA256 != newValue.ObjectSHA256:
			item.Change = "semantic_changed"
			result.Summary.SemanticChanged++
		case old.SourceSHA256 != newValue.SourceSHA256 || old.EvidenceSHA256 != newValue.EvidenceSHA256:
			item.Change = "lineage_changed"
			result.Summary.LineageChanged++
		default:
			result.Summary.Unchanged++
			continue
		}
		result.Changes = append(result.Changes, item)
	}
	return result, nil
}
