// Package databasebootstrap adds the Database Cognition Volume to an active
// Code-only Volumes repository after canonical Schema Evidence is accepted.
// It owns lifecycle bytes only and never generates table FRAS.
package databasebootstrap

import (
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	Operation = "database-bootstrap"

	StatusPrepared       = machinecontract.DatabaseCognitionBootstrapStatusPrepared
	StatusApplied        = machinecontract.DatabaseCognitionBootstrapStatusApplied
	StatusAlreadyApplied = machinecontract.DatabaseCognitionBootstrapStatusAlreadyApplied
	StatusRolledBack     = machinecontract.DatabaseCognitionBootstrapStatusRolledBack
)

type EvidenceSource struct {
	SourceID             string `json:"source_id"`
	EvidenceVersion      string `json:"evidence_version"`
	SourceSnapshotSHA256 string `json:"source_snapshot_sha256"`
	TableCount           int    `json:"table_count"`
}

type Preview struct {
	Version                    string           `json:"version"`
	Operation                  string           `json:"operation"`
	PreparedAt                 string           `json:"prepared_at"`
	RootPath                   string           `json:"root_path"`
	RootPreimageSHA256         string           `json:"root_preimage_sha256"`
	RootPostimageSHA256        string           `json:"root_postimage_sha256"`
	RootPostimage              string           `json:"root_postimage"`
	MetaSHA256                 string           `json:"meta_sha256"`
	CodeVolumeSHA256           string           `json:"code_volume_sha256"`
	DatabasePath               string           `json:"database_path"`
	DatabasePostimageSHA256    string           `json:"database_postimage_sha256"`
	DatabasePostimage          string           `json:"database_postimage"`
	BaselinePath               string           `json:"baseline_path"`
	BaselinePreimageSHA256     string           `json:"baseline_preimage_sha256"`
	BaselinePreimage           string           `json:"baseline_preimage"`
	BaselinePostimageSHA256    string           `json:"baseline_postimage_sha256"`
	BaselinePostimage          string           `json:"baseline_postimage"`
	EvidenceBaselineSHA256     string           `json:"evidence_baseline_sha256"`
	EvidenceSources            []EvidenceSource `json:"evidence_sources"`
	ProjectedCompositeIdentity string           `json:"projected_composite_identity"`
	ReviewSet                  []string         `json:"review_set"`
	WriteSet                   []string         `json:"write_set"`
	GuardSet                   []string         `json:"guard_set"`
	WriteOrder                 []string         `json:"write_order"`
	RootLast                   bool             `json:"root_last"`
	NetworkAccessed            bool             `json:"network_accessed"`
	BusinessDataRead           bool             `json:"business_data_read"`
	DDLDMLStatements           int              `json:"ddl_dml_statements"`
	PreviewDigest              string           `json:"preview_digest"`
}

type RecoveryIntent struct {
	Version        string                         `json:"version"`
	TransactionID  string                         `json:"transaction_id"`
	Preview        Preview                        `json:"preview"`
	Staging        []cognitiontxn.StagedPostimage `json:"staging"`
	CreatedAt      string                         `json:"created_at"`
	RecoveryDigest string                         `json:"recovery_digest"`
}

type Result struct {
	Version            string `json:"version"`
	Operation          string `json:"operation"`
	TransactionID      string `json:"transaction_id"`
	Status             string `json:"status"`
	DatabaseReady      bool   `json:"database_ready"`
	DatabaseEntryCount int    `json:"database_entry_count"`
	CodeVolumeSHA256   string `json:"code_volume_sha256"`
	RootSHA256         string `json:"root_sha256"`
	BaselineSHA256     string `json:"baseline_sha256"`
	NetworkAccessed    bool   `json:"network_accessed"`
	BusinessDataRead   bool   `json:"business_data_read"`
	DDLDMLStatements   int    `json:"ddl_dml_statements"`
	NextAction         string `json:"next_action"`
	ResultDigest       string `json:"result_digest"`
}
