// Package codebatch binds one machine-issued Code authoring batch to the
// current Volumes cognition and Managed Scope preimages. It owns candidate
// transport receipts only; it never authors or rewrites Tag or F/R/A/S text,
// and it never reads or reasons about the relations those entries declare.
package codebatch

import "encoding/json"

type Target struct {
	CandidateID   string `json:"candidate_id"`
	ObjectRef     string `json:"object_ref"`
	Path          string `json:"path"`
	Change        string `json:"change"`
	SourceSHA256  string `json:"source_sha256"`
	ExistingEntry string `json:"existing_entry,omitempty"`
}

type Receipt struct {
	Version             string   `json:"version"`
	PlanID              string   `json:"plan_id"`
	BatchID             string   `json:"batch_id"`
	CompositeIdentity   string   `json:"composite_identity"`
	ScopePolicyIdentity string   `json:"scope_policy_identity"`
	CodeVolumePath      string   `json:"code_volume_path"`
	CodeVolumeSHA256    string   `json:"code_volume_sha256"`
	AllTargets          []Target `json:"all_targets"`
	Targets             []Target `json:"targets"`
	// LogicalPlan names the parallel plan this batch is a sibling of; empty
	// for an ordinary single current batch. It is outside the batch identity.
	LogicalPlan string `json:"logical_plan,omitempty"`
	// LegacyObservedRelations exists only so old receipts stay readable. Receipt loading refuses unknown fields,
	// and a receipt issued before the upgrade may carry this field; dropping it would deadlock a plan in progress the instant of the
	// upgrade. The machine no longer derives anything from R, so this data is discarded once decoded: it takes part in no identity derivation,
	// no packing, and new receipts no longer write it.
	LegacyObservedRelations json.RawMessage `json:"observed_relations,omitempty"`
}

type Candidate struct {
	Target
}

type Plan struct {
	Version                      string `json:"version"`
	PlanID                       string `json:"plan_id"`
	BatchID                      string `json:"batch_id"`
	CompositeIdentity            string `json:"composite_identity"`
	ScopePolicyIdentity          string `json:"scope_policy_identity"`
	CodeVolumePath               string `json:"code_volume_path"`
	CodeVolumeSHA256             string `json:"code_volume_sha256"`
	TotalTargets                 int    `json:"total_targets"`
	MaxEntries                   int    `json:"max_entries"`
	Included                     int    `json:"included"`
	Remaining                    int    `json:"remaining"`
	CompleteCandidateSetForBatch bool   `json:"complete_candidate_set_for_current_batch"`
	ContinuationRequired         bool   `json:"continuation_required"`
	// TransportBudgetBytes is the Maintain response budget that bounded this
	// batch; zero means the batch was cut by count alone.
	TransportBudgetBytes int `json:"transport_budget_bytes,omitempty"`
	// Candidates is the issued batch. It stays in the disk receipt and in
	// process; the wire response carries the same objects once, as the
	// top-level candidates, so a response never repeats an existing Entry.
	Candidates []Candidate `json:"candidates,omitempty"`
	NextAction string      `json:"next_action"`
	// LogicalPlan, SiblingOf, and Siblings are the parallel current batch
	// facts (L4): the plan every sibling of one preimage records, the plan a
	// delivered sibling belongs to, and the other siblings with their state.
	LogicalPlan string    `json:"logical_plan,omitempty"`
	SiblingOf   string    `json:"sibling_of,omitempty"`
	Siblings    []Sibling `json:"siblings,omitempty"`
}

type Submission struct {
	CandidateIndex int
	ObjectRef      string
	CandidateID    string
	SourceSHA256   string
}

// SubmissionIssue identifies one exact caller-owned binding that differs from
// the current machine-issued Code batch. It contains no authored semantics and
// is safe to expose as a zero-write repair diagnostic.
type SubmissionIssue struct {
	CandidateIndex int
	Path           string
	ObjectRef      string
	Field          string
	Expected       string
	Actual         string
	Code           string
}

// SubmissionError preserves structured Code batch binding evidence across the
// codebatch/mcptools boundary. BatchID issues are top-level request repairs;
// candidate issues identify the original 1-based submitted entry.
type SubmissionError struct {
	Code            string
	ExpectedBatchID string
	ActualBatchID   string
	Issues          []SubmissionIssue
}

func (e *SubmissionError) Error() string {
	if e == nil || e.Code == "" {
		return "code_candidate_submission_mismatch"
	}
	return e.Code
}
