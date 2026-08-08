// Package dbcognition connects deterministic Database Evidence to
// model-authored table cognition. It discovers work, binds machine identities,
// and validates complete batches; it never generates or rewrites FRAS text.
package dbcognition

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

type Summary struct {
	Current             int `json:"current"`
	Missing             int `json:"missing"`
	Stale               int `json:"stale"`
	Unbaselined         int `json:"unbaselined"`
	Orphan              int `json:"orphan"`
	EvidenceUnavailable int `json:"evidence_unavailable"`
	EvidenceInvalid     int `json:"evidence_invalid"`
	SourceDisabled      int `json:"source_disabled"`
}

type SourceState struct {
	SourceID        string `json:"source_id"`
	State           string `json:"state"`
	EvidenceVersion string `json:"evidence_version,omitempty"`
	TableCount      int    `json:"table_count"`
	ErrorCode       string `json:"error_code,omitempty"`
}

type Item struct {
	ObjectRef           string                             `json:"object_ref"`
	SourceID            string                             `json:"source_id"`
	State               string                             `json:"state"`
	CurrentEntry        string                             `json:"current_entry,omitempty"`
	TableEvidenceSHA256 string                             `json:"table_evidence_sha256,omitempty"`
	EvidenceVersion     string                             `json:"evidence_version,omitempty"`
	EvidenceRef         string                             `json:"evidence_ref,omitempty"`
	Binding             *baseline.DatabaseCognitionBinding `json:"binding,omitempty"`
	record              *dbevidence.SnapshotTable
}

type Assessment struct {
	Version              int           `json:"version"`
	ConfiguredSources    int           `json:"configured_sources"`
	BlockingSourceCount  int           `json:"blocking_source_count"`
	DatabaseVolumeState  string        `json:"database_volume_state"`
	DatabaseVolumePath   string        `json:"database_volume_path,omitempty"`
	DatabaseVolumeSHA256 string        `json:"database_volume_sha256,omitempty"`
	EvidenceTableCount   int           `json:"evidence_table_count"`
	CognitionEntryCount  int           `json:"cognition_entry_count"`
	CognitionCurrent     bool          `json:"cognition_current"`
	NetworkAccessed      bool          `json:"network_accessed"`
	Summary              Summary       `json:"summary"`
	Sources              []SourceState `json:"sources"`
	Items                []Item        `json:"items"`
	NextAction           string        `json:"next_action"`
}

type ReceiptTarget struct {
	CandidateID         string `json:"candidate_id"`
	ObjectRef           string `json:"object_ref"`
	SourceID            string `json:"source_id"`
	CognitionState      string `json:"cognition_state"`
	EvidenceVersion     string `json:"evidence_version"`
	TableEvidenceSHA256 string `json:"table_evidence_sha256"`
	EvidenceRef         string `json:"evidence_ref"`
}

type Receipt struct {
	Version              string          `json:"version"`
	BatchID              string          `json:"batch_id"`
	DatabaseVolumePath   string          `json:"database_volume_path"`
	DatabaseVolumeSHA256 string          `json:"database_volume_sha256"`
	Targets              []ReceiptTarget `json:"targets"`
}

type Candidate struct {
	ReceiptTarget
	ExistingDatabaseEntry string                    `json:"existing_database_entry,omitempty"`
	EvidenceBytes         int                       `json:"evidence_bytes"`
	EvidenceBundle        dbevidence.EvidenceBundle `json:"evidence_bundle"`
}

type Plan struct {
	Version              string      `json:"version"`
	BatchID              string      `json:"batch_id"`
	DatabaseVolumePath   string      `json:"database_volume_path"`
	DatabaseVolumeSHA256 string      `json:"database_volume_sha256"`
	TargetCount          int         `json:"target_count"`
	Remaining            int         `json:"remaining"`
	EvidenceBytes        int         `json:"evidence_bytes"`
	Candidates           []Candidate `json:"candidates"`
	NextAction           string      `json:"next_action"`
}

type Submission struct {
	ObjectRef   string
	CandidateID string
}
