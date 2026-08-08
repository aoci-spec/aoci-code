// Package cognition loads legacy and Volumes v1 repository cognition into one
// read-only model and performs deterministic read-only impact analysis. It does
// not own generation, maintenance, Baseline, CAS, Apply, Ledger, or Recovery.
package cognition

import "github.com/aoci-spec/aoci-code/internal/index"

const (
	LayoutLegacyMonolithic = "legacy-monolithic"
	LayoutVolumesV1        = "volumes-v1"

	RootManifestMarker = "#AOCI-ROOT-MANIFEST: 1"
	MetaVolumeMarker   = "#AOCI-META-VOLUME: 1"
	CodeVolumeMarker   = "#AOCI-CODE-VOLUME: 1"
	DatabaseMarker     = "#AOCI-DATABASE-VOLUME: 1"

	AssetAbsent  = "absent"
	AssetPresent = "present"
	AssetInvalid = "invalid"

	ScopeAll      = "all"
	ScopeProject  = "project"
	ScopeMeta     = "meta"
	ScopeCode     = "code"
	ScopeDatabase = "database"
)

// Finding is one deterministic format, layout, safety, or density result.
// It never contains generated cognition semantics.
type Finding struct {
	Code    string `json:"code"`
	AssetID string `json:"asset_id,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// RepairFinding is the single machine-facing candidate diagnostic shared by
// Impact, Preview, batch Apply, Auto finalization, MCP, and CLI JSON. The
// candidate facts are always explicit so zero values cannot disappear from a
// rejected-batch contract. Code, ObjectRef, and Message retain the existing
// Impact compatibility aliases while RuleCode and the other fields carry the
// actionable rule evidence.
type RepairFinding struct {
	CandidateIndex          int      `json:"candidate_index"`
	Path                    string   `json:"path"`
	CanonicalObjectIdentity string   `json:"canonical_object_identity"`
	Domain                  string   `json:"domain"`
	Field                   string   `json:"field"`
	RuleCode                string   `json:"rule_code"`
	Expected                string   `json:"expected"`
	Actual                  string   `json:"actual"`
	Cause                   string   `json:"cause"`
	SafeRepairAction        string   `json:"safe_repair_action"`
	Code                    string   `json:"code,omitempty"`
	ObjectRef               string   `json:"object_ref,omitempty"`
	Relation                string   `json:"relation,omitempty"`
	Candidates              []string `json:"candidates,omitempty"`
	Message                 string   `json:"message,omitempty"`
}

// Descriptor is the Root-declared identity of one Volume.
type Descriptor struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Path          string   `json:"path"`
	FormatVersion string   `json:"format_version"`
	DependsOn     []string `json:"depends_on,omitempty"`
	State         string   `json:"state,omitempty"`
}

// Object is one model-authored FRAS cognition object.
type Object struct {
	VolumeID         string       `json:"volume_id"`
	Kind             string       `json:"kind"`
	Name             string       `json:"name"`
	Namespace        string       `json:"namespace,omitempty"`
	CanonicalRef     string       `json:"canonical_ref"`
	Entry            *index.Entry `json:"-"`
	CanonicalLine    string       `json:"canonical_line"`
	SourceSection    string       `json:"source_section,omitempty"`
	SourceLineNumber int          `json:"source_line_number"`
}

// Asset holds immutable bytes and their deterministic parse result.
type Asset struct {
	Descriptor  Descriptor      `json:"descriptor"`
	State       string          `json:"asset_state"`
	SHA256      string          `json:"sha256,omitempty"`
	Raw         []byte          `json:"-"`
	Document    *index.Document `json:"-"`
	Objects     []Object        `json:"objects,omitempty"`
	ObjectCount int             `json:"object_count"`
	Findings    []Finding       `json:"findings,omitempty"`
}

// Set is the single read-only representation for both physical layouts.
type Set struct {
	RepositoryRoot    string            `json:"repository_root"`
	LayoutMode        string            `json:"layout_mode"`
	LayoutVersion     string            `json:"layout_version"`
	Root              Asset             `json:"root"`
	Meta              Asset             `json:"meta"`
	Volumes           map[string]*Asset `json:"volumes"`
	DeclaredOrder     []string          `json:"declared_order,omitempty"`
	CompositeIdentity string            `json:"composite_identity"`
	Warnings          []Finding         `json:"warnings,omitempty"`
	Errors            []Finding         `json:"errors,omitempty"`
}

// ScopeView is the exact dependency-closed asset delivery for one Overview.
type ScopeView struct {
	RequestedScope string   `json:"requested_scope"`
	EffectiveScope string   `json:"effective_scope"`
	Available      bool     `json:"scope_available"`
	AssetState     string   `json:"asset_state"`
	Assets         []*Asset `json:"-"`
	ObjectCount    int      `json:"scope_object_count"`
	ScopeIdentity  string   `json:"scope_identity"`
}

// ValidationError means the recognized layout is damaged. Callers must fail
// closed; it is never permission to reinterpret the Root as a legacy index.
type ValidationError struct {
	Findings []Finding
}

func (e *ValidationError) Error() string {
	if len(e.Findings) == 0 {
		return "invalid cognition layout"
	}
	return e.Findings[0].Code + ": " + e.Findings[0].Message
}
