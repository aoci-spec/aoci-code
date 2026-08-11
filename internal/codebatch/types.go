// Package codebatch binds one machine-issued Code authoring batch to the
// current Volumes cognition and Managed Scope preimages. It owns candidate
// transport receipts only; it never authors or rewrites Tag or F/R/A/S text.
package codebatch

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
}

type Candidate struct {
	Target
}

type Plan struct {
	Version                      string      `json:"version"`
	PlanID                       string      `json:"plan_id"`
	BatchID                      string      `json:"batch_id"`
	CompositeIdentity            string      `json:"composite_identity"`
	ScopePolicyIdentity          string      `json:"scope_policy_identity"`
	CodeVolumePath               string      `json:"code_volume_path"`
	CodeVolumeSHA256             string      `json:"code_volume_sha256"`
	TotalTargets                 int         `json:"total_targets"`
	MaxEntries                   int         `json:"max_entries"`
	Included                     int         `json:"included"`
	Remaining                    int         `json:"remaining"`
	CompleteCandidateSetForBatch bool        `json:"complete_candidate_set_for_current_batch"`
	ContinuationRequired         bool        `json:"continuation_required"`
	Candidates                   []Candidate `json:"candidates"`
	NextAction                   string      `json:"next_action"`
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
