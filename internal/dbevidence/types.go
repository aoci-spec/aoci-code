// Package dbevidence owns deterministic database schema facts. It never
// generates, suggests, truncates, or rewrites cognition tags or F/R/A/S text.
package dbevidence

import "time"

const (
	EvidenceVersion       = "database-evidence/v1"
	SourceManifestVersion = "database-source-manifest/v1"
	SnapshotVersion       = "database-snapshot/v1"
	BaselineVersion       = "database-evidence-baseline/v1"
	BundleVersion         = "database-evidence-bundle/v1"
)

type Engine string

const (
	EnginePostgreSQL Engine = "postgresql"
	EngineMySQL      Engine = "mysql"
	EngineOpenGauss  Engine = "opengauss"
)

type SourceConfig struct {
	SourceID              string   `json:"source_id"`
	Engine                Engine   `json:"engine"`
	Database              string   `json:"database"`
	Namespaces            []string `json:"namespaces"`
	IncludeNamespaces     []string `json:"include_namespaces,omitempty"`
	ExcludeNamespaces     []string `json:"exclude_namespaces,omitempty"`
	IncludeTables         []string `json:"include_tables,omitempty"`
	ExcludeTables         []string `json:"exclude_tables,omitempty"`
	CredentialEnv         string   `json:"credential_env"`
	ConnectTimeoutSeconds int      `json:"connect_timeout_seconds"`
	QueryTimeoutSeconds   int      `json:"query_timeout_seconds"`
	Enabled               bool     `json:"enabled"`
}

func (s SourceConfig) ConnectTimeout() time.Duration {
	return time.Duration(s.ConnectTimeoutSeconds) * time.Second
}

func (s SourceConfig) QueryTimeout() time.Duration {
	return time.Duration(s.QueryTimeoutSeconds) * time.Second
}

type CaseSemantics struct {
	IdentifierCase      string `json:"identifier_case"`
	LowerCaseTableNames *int   `json:"lower_case_table_names,omitempty"`
}

type SourceManifest struct {
	Version           string        `json:"version"`
	SourceID          string        `json:"source_id"`
	Engine            Engine        `json:"engine"`
	Database          string        `json:"database"`
	Namespaces        []string      `json:"namespaces"`
	IncludeNamespaces []string      `json:"include_namespaces"`
	ExcludeNamespaces []string      `json:"exclude_namespaces"`
	IncludeTables     []string      `json:"include_tables"`
	ExcludeTables     []string      `json:"exclude_tables"`
	CaseSemantics     CaseSemantics `json:"case_semantics"`
	ServerVersion     string        `json:"server_version,omitempty"`
	BusinessDataRead  bool          `json:"business_data_read"`
}

type Column struct {
	Ordinal              int     `json:"ordinal"`
	Name                 string  `json:"name"`
	NativeType           string  `json:"native_type"`
	CanonicalType        string  `json:"canonical_type"`
	Nullable             bool    `json:"nullable"`
	DefaultExpression    *string `json:"default_expression,omitempty"`
	Identity             string  `json:"identity,omitempty"`
	Serial               bool    `json:"serial,omitempty"`
	AutoIncrement        bool    `json:"auto_increment,omitempty"`
	Generated            string  `json:"generated,omitempty"`
	GenerationExpression string  `json:"generation_expression,omitempty"`
}

type KeyConstraint struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

type ForeignKey struct {
	Name              string   `json:"name"`
	Columns           []string `json:"columns"`
	ReferencedObject  string   `json:"referenced_object"`
	ReferencedColumns []string `json:"referenced_columns"`
	UpdateAction      string   `json:"update_action"`
	DeleteAction      string   `json:"delete_action"`
}

type CheckConstraint struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

type IndexElement struct {
	Position     int     `json:"position"`
	Column       *string `json:"column,omitempty"`
	Expression   *string `json:"expression,omitempty"`
	PrefixLength *int    `json:"prefix_length,omitempty"`
	Descending   bool    `json:"descending,omitempty"`
	Included     bool    `json:"included,omitempty"`
}

type Index struct {
	Name      string         `json:"name"`
	Unique    bool           `json:"unique"`
	Primary   bool           `json:"primary,omitempty"`
	Visible   *bool          `json:"visible,omitempty"`
	Method    string         `json:"method,omitempty"`
	Elements  []IndexElement `json:"elements"`
	Predicate string         `json:"predicate,omitempty"`
}

type Partition struct {
	Partitioned  bool                  `json:"partitioned"`
	Method       string                `json:"method,omitempty"`
	Expression   string                `json:"expression,omitempty"`
	ParentObject string                `json:"parent_object,omitempty"`
	Bound        string                `json:"bound,omitempty"`
	ChildObjects []string              `json:"child_objects,omitempty"`
	Definitions  []PartitionDefinition `json:"definitions,omitempty"`
}

type PartitionDefinition struct {
	Name        string `json:"name"`
	Ordinal     int    `json:"ordinal"`
	Description string `json:"description,omitempty"`
}

type TableEvidence struct {
	Version           string            `json:"version"`
	ObjectRef         string            `json:"object_ref"`
	Engine            Engine            `json:"engine"`
	SourceID          string            `json:"source_id"`
	Database          string            `json:"database"`
	Namespace         string            `json:"namespace"`
	Name              string            `json:"name"`
	Kind              string            `json:"kind"`
	Columns           []Column          `json:"columns"`
	PrimaryKey        *KeyConstraint    `json:"primary_key,omitempty"`
	UniqueConstraints []KeyConstraint   `json:"unique_constraints"`
	ForeignKeys       []ForeignKey      `json:"foreign_keys"`
	Checks            []CheckConstraint `json:"checks"`
	Indexes           []Index           `json:"indexes"`
	Partition         *Partition        `json:"partition,omitempty"`
}

type ComponentHashes struct {
	Columns           string `json:"columns"`
	PrimaryKey        string `json:"primary_key"`
	UniqueConstraints string `json:"unique_constraints"`
	ForeignKeys       string `json:"foreign_keys"`
	Checks            string `json:"checks"`
	Indexes           string `json:"indexes"`
	Partition         string `json:"partition"`
}

type SnapshotTable struct {
	ObjectRef           string          `json:"object_ref"`
	TableEvidenceSHA256 string          `json:"table_evidence_sha256"`
	ComponentHashes     ComponentHashes `json:"component_hashes"`
	EvidenceRef         string          `json:"evidence_ref"`
}

type Snapshot struct {
	Version              string          `json:"version"`
	EvidenceVersion      string          `json:"evidence_version"`
	SourceID             string          `json:"source_id"`
	Engine               Engine          `json:"engine"`
	Database             string          `json:"database"`
	Namespaces           []string        `json:"namespaces"`
	IncludeNamespaces    []string        `json:"include_namespaces"`
	ExcludeNamespaces    []string        `json:"exclude_namespaces"`
	IncludeTables        []string        `json:"include_tables"`
	ExcludeTables        []string        `json:"exclude_tables"`
	CaseSemantics        CaseSemantics   `json:"case_semantics"`
	State                string          `json:"state"`
	Tables               []SnapshotTable `json:"tables"`
	SourceSnapshotSHA256 string          `json:"source_snapshot_sha256"`
	BusinessDataRead     bool            `json:"business_data_read"`
}

type BaselineTable struct {
	ObjectRef           string          `json:"object_ref"`
	TableEvidenceSHA256 string          `json:"table_evidence_sha256"`
	ComponentHashes     ComponentHashes `json:"component_hashes"`
}

type BaselineSource struct {
	SourceID             string          `json:"source_id"`
	Engine               Engine          `json:"engine"`
	Database             string          `json:"database"`
	Namespaces           []string        `json:"namespaces"`
	IncludeNamespaces    []string        `json:"include_namespaces"`
	ExcludeNamespaces    []string        `json:"exclude_namespaces"`
	IncludeTables        []string        `json:"include_tables"`
	ExcludeTables        []string        `json:"exclude_tables"`
	CaseSemantics        CaseSemantics   `json:"case_semantics"`
	EvidenceVersion      string          `json:"evidence_version"`
	SourceSnapshotSHA256 string          `json:"source_snapshot_sha256"`
	Tables               []BaselineTable `json:"tables"`
}

type Baseline struct {
	Version string           `json:"version"`
	Sources []BaselineSource `json:"sources"`
}

type DriftStatus string

const (
	DriftUnchanged         DriftStatus = "unchanged"
	DriftNew               DriftStatus = "new"
	DriftChanged           DriftStatus = "changed"
	DriftRemoved           DriftStatus = "removed"
	DriftSourceUnavailable DriftStatus = "source_unavailable"
	DriftEvidenceInvalid   DriftStatus = "evidence_invalid"
)

type DriftItem struct {
	ObjectRef          string      `json:"object_ref"`
	Status             DriftStatus `json:"status"`
	BaselineSHA256     string      `json:"baseline_sha256,omitempty"`
	CurrentSHA256      string      `json:"current_sha256,omitempty"`
	ChangedComponents  []string    `json:"changed_components,omitempty"`
	CurrentEvidenceRef string      `json:"current_evidence_ref,omitempty"`
}

type DriftSummary struct {
	Unchanged int `json:"unchanged"`
	New       int `json:"new"`
	Changed   int `json:"changed"`
	Removed   int `json:"removed"`
}

type DriftReport struct {
	Version               int          `json:"version"`
	SourceID              string       `json:"source_id"`
	Engine                Engine       `json:"engine"`
	SourceStatus          string       `json:"source_status"`
	ErrorCode             string       `json:"error_code,omitempty"`
	SourceSnapshotSHA256  string       `json:"source_snapshot_sha256"`
	BaselinePresent       bool         `json:"baseline_present"`
	SourceIdentityChanged bool         `json:"source_identity_changed"`
	Summary               DriftSummary `json:"summary"`
	Items                 []DriftItem  `json:"items"`
	BusinessDataRead      bool         `json:"business_data_read"`
}

type EvidenceBundle struct {
	Version                   string        `json:"version"`
	ReadyFor                  string        `json:"ready_for"`
	ObjectRef                 string        `json:"object_ref"`
	EvidenceVersion           string        `json:"evidence_version"`
	TableEvidenceSHA256       string        `json:"table_evidence_sha256"`
	TableEvidence             TableEvidence `json:"table_evidence"`
	MigrationEvidenceRefs     []string      `json:"migration_evidence_refs"`
	CodeEvidenceRefs          []string      `json:"code_evidence_refs"`
	ExistingDatabaseEntry     *string       `json:"existing_database_entry,omitempty"`
	BusinessDataRead          bool          `json:"business_data_read"`
	SemanticCandidateIncluded bool          `json:"semantic_candidate_included"`
	NextAction                string        `json:"next_action"`
}

type Inspection struct {
	Version           int           `json:"version"`
	SourceID          string        `json:"source_id"`
	Engine            Engine        `json:"engine"`
	Database          string        `json:"database"`
	ServerVersion     string        `json:"server_version,omitempty"`
	CaseSemantics     CaseSemantics `json:"case_semantics"`
	VisibleNamespaces []string      `json:"visible_namespaces"`
	VisibleTables     int           `json:"visible_tables"`
	BusinessDataRead  bool          `json:"business_data_read"`
}
