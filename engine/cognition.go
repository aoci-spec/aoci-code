package engine

import (
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// Layout values a repository can be in.
const (
	// LayoutVolumesV1 is the current layout: a Root manifest declaring the
	// Meta Volume and the object Volumes.
	LayoutVolumesV1 = cognition.LayoutVolumesV1
	// LayoutLegacy is the single-file layout, read-only since 0.2.0.
	LayoutLegacy = cognition.LayoutLegacyMonolithic
)

// Asset describes one formal cognition file exactly as loaded.
type Asset struct {
	// ID is the Volume identity: root, meta, code, database, or legacy.
	ID string
	// Kind is the Volume kind the Root manifest declares.
	Kind string
	// Path is the repository-relative file path.
	Path string
	// State is present, absent, or invalid.
	State string
	// SHA256 is the hex digest of the exact file bytes.
	SHA256 string
	// ObjectCount is the number of Entries the asset holds.
	ObjectCount int
}

// Entry is one cognition Entry: the atomic object it describes and its
// model-authored fields, projected from the formal line without rewriting.
type Entry struct {
	// Volume is the Volume the Entry lives in: code, database, or legacy.
	Volume string
	// Domain is the cognition domain: code or database.
	Domain string
	// Kind is the atomic object kind: file for a code object, table for a
	// database table.
	Kind string
	// ObjectRef is the canonical object identity: code:<path> or a
	// database:// reference under Volumes v1; the repository-relative path
	// on a Legacy layout.
	ObjectRef string
	// Path is the repository-relative source path of a code object.
	Path string
	// Name is the bare object name as the Entry line spells it.
	Name string
	// Namespace is the database namespace of a table object.
	Namespace string
	// Tag is the raw compact tag between the brackets.
	Tag string
	// F, R, A, S are the four fields as authored.
	F, R, A, S string
	// Line is the exact formal Entry line.
	Line string
	// LineNumber is the 1-based line number inside the Volume file.
	LineNumber int
}

// Finding is a non-fatal observation the loader made about an asset.
type Finding struct {
	Code    string
	AssetID string
	Line    int
	Message string
}

// Cognition is the loaded formal cognition of a repository.
type Cognition struct {
	Layout            string
	LayoutVersion     string
	CompositeIdentity string
	// Root is the Root manifest (Volumes) or the single index (Legacy).
	Root Asset
	// Meta is the Meta Volume; absent on a Legacy layout.
	Meta Asset
	// Volumes are the declared object Volumes (code, database) in
	// declaration order; the Meta Volume is Meta.
	Volumes []Asset
	// Entries are every Entry of every Volume in formal order.
	Entries  []Entry
	Warnings []Finding
}

// Cognition loads the repository's formal cognition through the same loader
// the CLI and the MCP tools use and projects it. A damaged layout fails
// closed exactly as it does for them.
func (r *Repository) Cognition() (*Cognition, error) {
	set, err := r.load()
	if err != nil {
		return nil, err
	}
	return projectCognition(set), nil
}

func projectCognition(set *cognition.Set) *Cognition {
	out := &Cognition{
		Layout:            set.LayoutMode,
		LayoutVersion:     set.LayoutVersion,
		CompositeIdentity: set.CompositeIdentity,
		Root:              projectAsset(&set.Root),
		Meta:              projectAsset(&set.Meta),
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic {
		for index := range set.Root.Objects {
			out.Entries = append(out.Entries, projectEntry(&set.Root.Objects[index]))
		}
	}
	for _, id := range set.DeclaredOrder {
		asset := set.Volumes[id]
		if asset == nil || asset.Descriptor.Kind == "meta" {
			continue
		}
		out.Volumes = append(out.Volumes, projectAsset(asset))
		for index := range asset.Objects {
			out.Entries = append(out.Entries, projectEntry(&asset.Objects[index]))
		}
	}
	for _, warning := range set.Warnings {
		out.Warnings = append(out.Warnings, Finding{Code: warning.Code, AssetID: warning.AssetID, Line: warning.Line, Message: warning.Message})
	}
	return out
}

func projectAsset(asset *cognition.Asset) Asset {
	if asset == nil {
		return Asset{}
	}
	return Asset{
		ID:          asset.Descriptor.ID,
		Kind:        asset.Descriptor.Kind,
		Path:        asset.Descriptor.Path,
		State:       asset.State,
		SHA256:      asset.SHA256,
		ObjectCount: asset.ObjectCount,
	}
}

func projectEntry(object *cognition.Object) Entry {
	domain, kind := objectDomainKind(object.Kind)
	entry := Entry{
		Volume:     object.VolumeID,
		Domain:     domain,
		Kind:       kind,
		ObjectRef:  object.CanonicalRef,
		Name:       object.Name,
		Namespace:  object.Namespace,
		Line:       object.CanonicalLine,
		LineNumber: object.SourceLineNumber,
	}
	if object.Entry != nil {
		entry.Path = object.Entry.RelPath
		entry.Tag = object.Entry.TagsRaw
		entry.F = object.Entry.F
		entry.R = object.Entry.R
		entry.A = object.Entry.Api
		entry.S = object.Entry.S
		if entry.Line == "" {
			entry.Line = object.Entry.FullLine
		}
	}
	if entry.Path == "" && strings.HasPrefix(object.CanonicalRef, "code:") {
		entry.Path = strings.TrimPrefix(object.CanonicalRef, "code:")
	}
	return entry
}

// objectDomainKind maps the loader's object kind to the domain and the
// atomic object kind the Meta declares (code=file, database=table); the
// Legacy loader spells a code object's kind as code.
func objectDomainKind(loaderKind string) (domain, kind string) {
	switch loaderKind {
	case "table", "database":
		return "database", "table"
	default:
		return "code", "file"
	}
}
