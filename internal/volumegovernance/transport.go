package volumegovernance

// TransportFacts is Facts as a Maintain response carries it: the bounded
// lists of BoundListsForTransport, without the four per-asset records.
//
// A Maintain response already names every identity those records hold —
// cognition_receipt carries the Root and Meta digests and the delivered
// Volumes, code_plan the Code Volume path and digest — and on a deep tree the
// four records cost more than a candidate. The records are shadowed here at
// the outer depth under their JSON names and left nil, which encoding/json
// resolves as "omitted": the shallower field owns the key, and omitempty
// drops a nil pointer. Go code keeps reading Facts.Root, Facts.Meta,
// Facts.Code and Facts.Database through the embedding, because the shadow
// fields have different Go names, so only the bytes on the wire change.
// Verify, Check, the Guide and the engine keep the complete Facts.
type TransportFacts struct {
	*Facts
	RootOmitted     *AssetState `json:"root,omitempty"`
	MetaOmitted     *AssetState `json:"meta,omitempty"`
	CodeOmitted     *AssetState `json:"code,omitempty"`
	DatabaseOmitted *AssetState `json:"database,omitempty"`
}

// MaintainTransportOmittedKeys lists the governance keys a Maintain response
// leaves out; a test pins them against the wire.
var MaintainTransportOmittedKeys = []string{"root", "meta", "code", "database"}

// BoundForMaintainTransport returns facts as a Maintain response carries them:
// per-item enumerations cut to limit leading items with their complete counts
// in list_truncation, and the per-asset records off the wire. The verdict,
// every count, the budget bands the authoring instructions point at, the
// Managed Scope facts, and the findings sample are untouched.
func BoundForMaintainTransport(facts *Facts, limit int) *TransportFacts {
	if facts == nil {
		return nil
	}
	return &TransportFacts{Facts: BoundListsForTransport(facts, limit)}
}
