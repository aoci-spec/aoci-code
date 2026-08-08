package cognition

const (
	ImpactStrategyDependencyClosure = "dependency_closure"
	ImpactStrategyFullCognitionSet  = "full_cognition_set"
	ImpactStrategyLegacyMonolithic  = "legacy_monolithic"

	ImpactChangeUpdate       = "update"
	ImpactChangeCreate       = "create"
	ImpactChangeDelete       = "delete"
	ImpactChangeRename       = "rename"
	ImpactChangeAsset        = "asset_change"
	ImpactChangeVolumeCreate = "volume_create"
	ImpactChangeVolumeDelete = "volume_delete"
	ImpactChangeLayout       = "layout_change"
	ImpactChangeMigration    = "migration"
)

// ImpactCandidate contains model-authored cognition or an explicit structural
// asset change. It deliberately carries no transaction, CAS, or recovery
// instructions; ResolveImpact derives those deterministic boundaries.
type ImpactCandidate struct {
	Change            string `json:"change"`
	ObjectRef         string `json:"object_ref,omitempty"`
	PreviousObjectRef string `json:"previous_object_ref,omitempty"`
	VolumeID          string `json:"volume_id,omitempty"`
	Path              string `json:"path,omitempty"`
	CanonicalLine     string `json:"canonical_line,omitempty"`
	// OriginalCandidateIndex is the immutable 1-based position in the complete
	// caller-submitted batch, before domain partitioning or deterministic sort.
	OriginalCandidateIndex int `json:"-"`
}

// ImpactReviewObject is one managed cognition object the model must consider.
// Inclusion never authorizes the program to create or rewrite its semantics.
type ImpactReviewObject struct {
	Object  string   `json:"object"`
	Volume  string   `json:"volume"`
	Reasons []string `json:"reasons"`
}

// ImpactReason records why an object or Volume joined the affected set.
type ImpactReason struct {
	Code   string `json:"code"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Volume string `json:"volume,omitempty"`
}

// AffectedCognitionSet is the read-only result consumed by future Diff, CAS,
// Apply, and Recovery integration. WriteSet contains only Volumes named by
// model candidates; ReviewSet and GuardSet may expand through dependencies.
type AffectedCognitionSet struct {
	LayoutMode    string               `json:"layout_mode"`
	ReviewSet     []ImpactReviewObject `json:"review_set"`
	WriteSet      []string             `json:"write_set"`
	GuardSet      []string             `json:"guard_set"`
	Reasons       []ImpactReason       `json:"reasons"`
	Strategy      string               `json:"strategy"`
	Upgrade       bool                 `json:"upgrade"`
	UpgradeReason []string             `json:"upgrade_reason,omitempty"`
	Findings      []ImpactFinding      `json:"findings,omitempty"`
}

// ImpactFinding is the shared candidate diagnostic. The alias prevents Impact
// from defining a second schema that could drift from Preview, MCP, or CLI.
type ImpactFinding = RepairFinding

// ImpactValidationError means the affected set cannot be proven. Callers must
// fail closed rather than guess a relation or silently expand a write target.
type ImpactValidationError struct {
	Findings []ImpactFinding
}

func (e *ImpactValidationError) Error() string {
	if len(e.Findings) == 0 {
		return "invalid cognition impact request"
	}
	return e.Findings[0].Code + ": " + e.Findings[0].Message
}

type impactRegistry map[string]Object

type impactEdge struct {
	from  string
	to    string
	stage string
}

type relationIssue struct {
	code       string
	source     string
	token      string
	candidates []string
}

type indexedImpactCandidate struct {
	candidate ImpactCandidate
	index     int
}

type impactGraph struct {
	out                map[string][]impactEdge
	in                 map[string][]impactEdge
	issuesBySource     map[string][]relationIssue
	issuesByCandidate  map[string][]relationIssue
	allRelationReasons []ImpactReason
}
