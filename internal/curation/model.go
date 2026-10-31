// Package curation carries the deterministic facts, file profiles, and persistent decisions of file-level semantic curation.
//
// Design boundary:
//   - this package calls no model, creates no index entry, and changes no Baseline;
//   - a decision is valid only while its source_sha256 matches the current file;
//   - the maintainer's config.curation_exclude takes precedence over file-level decisions;
//   - empty/binary/oversize are objects open to semantic decision;
//   - unreadable is a technical skip for which no trustworthy semantic judgement can be formed today.
//
// The Missing three-way split stays conserved:
//
//	ActionableMissing
//	+ CurationExcludedMissing
//	+ SkippedMissing
//	= Raw Missing
//
// PendingCuration is a subset of SkippedMissing and adds no fourth exclusive set:
//   - an undecided empty/binary/oversize is both a SkippedMissing that cannot be drafted automatically today
//     and a PendingCuration waiting for the host Agent's semantic judgement;
//   - a valid include decision promotes it to ActionableMissing;
//   - a valid exclude decision turns it into CurationExcludedMissing.
package curation

const (
	// Version is the structure version of .aoci/curation.json.
	Version = 1

	// DecisionInclude means the file should form a file-level AOCI entry.
	DecisionInclude = "include"

	// DecisionExclude means the file should not form an AOCI entry automatically.
	DecisionExclude = "exclude"

	// ProfileReasonEmpty means the file is physically empty.
	ProfileReasonEmpty = "empty"

	// ProfileReasonBinary means the file head holds a NUL and is probably binary.
	ProfileReasonBinary = "binary"

	// ProfileReasonOversize means the file exceeds the automatic semantic reading ceiling.
	ProfileReasonOversize = "oversize"

	// ProfileReasonUnreadablePrefix means the file cannot be read or profiled reliably today.
	ProfileReasonUnreadablePrefix = "unreadable:"
)

// Profile is the deterministic physical profile of one Missing file.
type Profile struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Lines        int    `json:"lines,omitempty"`
	Ext          string `json:"ext,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Decision is one persisted file-level semantic curation verdict.
//
// Role answers "what is this file's semantic role in the system"; Reason answers "why include or exclude".
// Agent and Model serve audit and are not used to derive the decision's validity.
type Decision struct {
	Path         string `json:"path"`
	Decision     string `json:"decision"`
	Role         string `json:"role"`
	Reason       string `json:"reason"`
	Confidence   int    `json:"confidence"`
	SourceSHA256 string `json:"source_sha256"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

// Document is the complete persistent structure of .aoci/curation.json.
type Document struct {
	Version   int        `json:"version"`
	Decisions []Decision `json:"decisions"`
}

// PendingCandidate is a file waiting for the host Agent's include/exclude semantic verdict.
type PendingCandidate struct {
	Path          string `json:"path"`
	SourceSHA256  string `json:"source_sha256"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Lines         int    `json:"lines,omitempty"`
	Ext           string `json:"ext,omitempty"`
	ProfileReason string `json:"profile_reason"`
}

// ExcludedMissing is a raw Missing already excluded by a governance verdict.
type ExcludedMissing struct {
	Path       string `json:"path"`
	Reason     string `json:"reason"`
	Confidence int    `json:"confidence,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Source     string `json:"source"`
}

// SkippedMissing is a raw Missing that does not currently enter the automatic entry drafting queue.
type SkippedMissing struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Classification is the complete curation interpretation of Raw Missing.
//
// Pending is a subset of Skipped, Included a subset of Actionable.
// StaleDecisions only reports old verdicts whose source digest no longer matches, without changing the physical Missing fact.
type Classification struct {
	Missing          []string           `json:"missing"`
	Actionable       []string           `json:"actionable_missing"`
	Included         []string           `json:"included_missing"`
	CurationExcluded []ExcludedMissing  `json:"curation_excluded_missing"`
	Skipped          []SkippedMissing   `json:"skipped_missing"`
	Pending          []PendingCandidate `json:"pending_curation_missing"`
	StaleDecisions   []string           `json:"stale_curation_decisions"`

	Profiles map[string]Profile `json:"-"`
}

// NewDocument returns an empty curation asset with non-nil slices.
func NewDocument() *Document {
	return &Document{
		Version:   Version,
		Decisions: []Decision{},
	}
}

// ExcludedPaths returns the excluded path list in stable order.
func (c Classification) ExcludedPaths() []string {
	result := make(
		[]string,
		0,
		len(c.CurationExcluded),
	)

	for _, item := range c.CurationExcluded {
		result = append(
			result,
			item.Path,
		)
	}

	return result
}
