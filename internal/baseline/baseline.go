// The fingerprint model and Baseline reading and writing.
// Index entry: baseline.go[Base.Fp.9.S]
//
// Discipline:
//   - the raw SHA-256 is always the authoritative byte fingerprint for content binding, CAS, and mutual recognition with the platform content_hash;
//   - NormalizedSHA256 expresses only the optional text-equivalence fingerprint after CRLF is folded to LF;
//   - whether line-ending equivalence is allowed is decided explicitly by the judgement layer; this file hides no tolerance policy;
//   - an old Baseline lacking NormalizedSHA256 keeps strict comparison, with no silent migration;
//   - Save rolls a backup of the old Baseline before writing, and the formal write goes through AtomicWrite;
//   - Size is for display only and takes no part in the equivalence judgement.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Fingerprint holds one file's raw byte fingerprint and its optional text-equivalence fingerprint.
type Fingerprint struct {
	// Role is additive Managed Scope state. An omitted role is legacy index,
	// preserving every Baseline v1 reader and old-project behavior.
	Role string `json:"role,omitempty"`
	// SHA256 is the SHA-256 of the raw file bytes; always present, always strict.
	SHA256 string `json:"sha256"`

	// Size is the number of bytes really read, for display and diagnosis only.
	Size int64 `json:"size,omitempty"`

	// NormalizedSHA256 is the SHA-256 after only CRLF is folded to LF.
	//
	// It is produced only when the text sniff passes and the file is within the safe ceiling.
	// A missing field may mean an old Baseline, a binary file, or an oversize file;
	// when either side lacks it the judgement layer must fall back to the strict raw SHA comparison.
	NormalizedSHA256 string `json:"normalized_sha256,omitempty"`

	// FormatSHA256 is the canonical source digest produced by a supported formatter.
	// Today it is produced only for .go files go/format can parse completely; it serves only to prove
	// a gofmt representation change and takes no part in CAS, the Stage source binding, or general semantic equivalence.
	FormatSHA256 string `json:"format_sha256,omitempty"`

	// FormatKind declares the deterministic formatter FormatSHA256 used.
	FormatKind string `json:"format_kind,omitempty"`
}

// Baseline is the full snapshot of .aoci/baseline.json.
type Baseline struct {
	Version           int                        `json:"version"`
	CreatedAt         string                     `json:"created_at"`
	UpdatedAt         string                     `json:"updated_at"`
	Files             map[string]Fingerprint     `json:"files"`
	ManagedScope      *ManagedScopeState         `json:"managed_scope,omitempty"`
	DatabaseCognition *DatabaseCognitionBindings `json:"database_cognition,omitempty"`
}

type ManagedScopeState struct {
	Version              string                  `json:"version"`
	PolicyIdentity       string                  `json:"policy_identity"`
	ObserveChangePolicy  string                  `json:"observe_change_policy"`
	BudgetPolicyIdentity string                  `json:"budget_policy_identity,omitempty"`
	BudgetPolicy         *cognitionbudget.Policy `json:"budget_policy,omitempty"`
	// ApplyAuthorizationMode records the authorization mode in effect for the transaction that produced this receipt.
	// The team configuration only expresses "the expectation"; only the receipt here is the governance posture "currently in effect", so the
	// direction of a mode transition can be proven: a relaxation (review→auto and the like) must be reviewed by a human and cannot
	// approve itself under the new mode. An old receipt lacks the field, the direction cannot be proven, and it is handled as fail-closed.
	ApplyAuthorizationMode string `json:"apply_authorization_mode,omitempty"`
	HighRiskApprovalDigest string `json:"high_risk_approval_digest,omitempty"`
}

// DatabaseCognitionBinding proves which complete model-authored table Entry
// was based on which canonical Table Evidence. It is intentionally stored in
// the existing cognition Baseline, not the independent Database Evidence
// Baseline, so Apply and recovery retain one governance transaction.
type DatabaseCognitionBinding struct {
	ObjectRef           string `json:"object_ref"`
	SourceID            string `json:"source_id"`
	EvidenceVersion     string `json:"evidence_version"`
	TableEvidenceSHA256 string `json:"table_evidence_sha256"`
	EntrySHA256         string `json:"entry_sha256"`
}

type DatabaseCognitionBindings struct {
	Version string                     `json:"version"`
	Entries []DatabaseCognitionBinding `json:"entries"`
}

// baselinePath returns the absolute path of the Baseline file.
func baselinePath(root string) string {
	return filepath.Join(root, ".aoci", "baseline.json")
}

// nowISO returns the current UTC ISO8601 time.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// NewBaseline builds a brand-new Baseline from the given fingerprint set.
func NewBaseline(files map[string]Fingerprint) *Baseline {
	if files == nil {
		files = map[string]Fingerprint{}
	}

	now := nowISO()

	return &Baseline{
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		Files:     files,
	}
}

// NewBaselineAt constructs the exact Baseline postimage selected by a
// governance Prepare phase. The supplied UTC timestamp is part of that
// postimage and is never regenerated by Apply or recovery.
func NewBaselineAt(files map[string]Fingerprint, timestamp string) (*Baseline, error) {
	if files == nil {
		files = map[string]Fingerprint{}
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || parsed.Location() != time.UTC {
		return nil, fmt.Errorf("baseline timestamp must be RFC3339 UTC")
	}
	return &Baseline{
		Version:   1,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
		Files:     files,
	}, nil
}

// MarshalExact encodes a preselected Baseline without changing timestamps.
// It is the shared serialization authority for digest-bound Prepare, Apply,
// Resume, and recovery comparisons.
func MarshalExact(baselineValue *Baseline) ([]byte, error) {
	if baselineValue == nil {
		return nil, fmt.Errorf("baseline is nil")
	}
	if baselineValue.Version != 1 || strings.TrimSpace(baselineValue.CreatedAt) == "" ||
		strings.TrimSpace(baselineValue.UpdatedAt) == "" || baselineValue.Files == nil {
		return nil, fmt.Errorf("baseline is incomplete")
	}
	if err := normalizeDatabaseCognitionBindings(baselineValue.DatabaseCognition); err != nil {
		return nil, fmt.Errorf("database cognition Baseline is invalid: %w", err)
	}
	if err := validateManagedScope(baselineValue); err != nil {
		return nil, fmt.Errorf("managed scope Baseline is invalid: %w", err)
	}
	data, err := json.MarshalIndent(baselineValue, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize baseline: %w", err)
	}
	return append(data, '\n'), nil
}

// Load reads the Baseline.
//
// It returns (Baseline, whether it exists, error).
// A missing file returns (nil,false,nil) and is not an error.
// The added NormalizedSHA256 uses omitempty, so an old Version=1 file reads compatibly as it is.
func Load(root string) (*Baseline, bool, error) {
	data, err := os.ReadFile(baselinePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf(
			"读取基线失败: %w",
			err,
		)
	}

	// The same bytes decode to the same document: a process that has decoded
	// this Baseline before gets a fresh copy without decoding the file map
	// again, which is most of what a large repository's Load costs.
	if memoized, ok := lookupBaselineMemo(baselinePath(root), data); ok {
		return memoized, true, nil
	}

	var baselineValue Baseline

	if err := json.Unmarshal(data, &baselineValue); err != nil {
		return nil, true, fmt.Errorf(
			"baseline.json 损坏(%v): 请人工核对后再决定是否重建Baseline",
			err,
		)
	}

	if baselineValue.Files == nil {
		baselineValue.Files = map[string]Fingerprint{}
	}
	if err := validateDatabaseCognitionBindings(baselineValue.DatabaseCognition); err != nil {
		return nil, true, fmt.Errorf("database cognition Baseline is invalid: %w", err)
	}
	if err := validateManagedScope(&baselineValue); err != nil {
		return nil, true, fmt.Errorf("managed scope Baseline is invalid: %w", err)
	}
	storeBaselineMemo(baselinePath(root), data, &baselineValue)

	return &baselineValue, true, nil
}

// Save writes the Baseline inside the shared index lock, for callers without an outer governance transaction.
func Save(root string, baselineValue *Baseline) error {
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return fmt.Errorf("获取Baseline共享写锁失败: %w", err)
	}
	saveErr := SaveUnderIndexLock(root, baselineValue)
	releaseErr := lock.Release()
	if saveErr != nil {
		return saveErr
	}
	if releaseErr != nil {
		return fmt.Errorf("释放Baseline共享写锁失败: %w", releaseErr)
	}
	return nil
}

// SaveUnderIndexLock writes the Baseline when the caller already holds the shared index lock.
// The old file is rolled into a backup before the write, and both the backup and the formal file go through AtomicWrite. The caller must
// wrap Load/change/Save completely in one lock domain, so scan and legal write entries such as Entries do not overwrite each other.
func SaveUnderIndexLock(root string, baselineValue *Baseline) error {
	if err := normalizeDatabaseCognitionBindings(baselineValue.DatabaseCognition); err != nil {
		return fmt.Errorf("database cognition Baseline is invalid: %w", err)
	}
	path := baselinePath(root)

	if err := os.MkdirAll(
		filepath.Dir(path),
		0o755,
	); err != nil {
		return fmt.Errorf(
			"创建 .aoci 目录失败: %w",
			err,
		)
	}

	if old, err := os.ReadFile(path); err == nil {
		if err := afs.AtomicWrite(
			path+".bak",
			old,
		); err != nil {
			return fmt.Errorf(
				"备份旧基线失败: %w",
				err,
			)
		}
	}

	baselineValue.UpdatedAt = nowISO()

	data, err := MarshalExact(baselineValue)
	if err != nil {
		return fmt.Errorf("序列化基线失败: %w", err)
	}

	return afs.AtomicWrite(
		path,
		data,
	)
}

// FindDatabaseCognitionBinding returns a copy of the binding for objectRef.
func FindDatabaseCognitionBinding(baselineValue *Baseline, objectRef string) (DatabaseCognitionBinding, bool) {
	if baselineValue == nil || baselineValue.DatabaseCognition == nil {
		return DatabaseCognitionBinding{}, false
	}
	for _, binding := range baselineValue.DatabaseCognition.Entries {
		if binding.ObjectRef == objectRef {
			return binding, true
		}
	}
	return DatabaseCognitionBinding{}, false
}

// UpdateDatabaseCognitionBinding advances one table binding in the same
// in-memory Baseline that owns cognition file fingerprints. The caller remains
// responsible for saving it under the existing governance lock.
func UpdateDatabaseCognitionBinding(baselineValue *Baseline, binding DatabaseCognitionBinding) error {
	if baselineValue == nil {
		return fmt.Errorf("baseline is nil")
	}
	if baselineValue.DatabaseCognition == nil {
		baselineValue.DatabaseCognition = &DatabaseCognitionBindings{
			Version: machinecontract.DatabaseCognitionBindingVersion,
			Entries: []DatabaseCognitionBinding{},
		}
	}
	replaced := false
	for index := range baselineValue.DatabaseCognition.Entries {
		if baselineValue.DatabaseCognition.Entries[index].ObjectRef == binding.ObjectRef {
			baselineValue.DatabaseCognition.Entries[index] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		baselineValue.DatabaseCognition.Entries = append(baselineValue.DatabaseCognition.Entries, binding)
	}
	return normalizeDatabaseCognitionBindings(baselineValue.DatabaseCognition)
}

// RemoveDatabaseCognitionBinding removes one explicitly governed orphan from
// the existing Database Cognition binding set. It does not delete Evidence or
// a database object and the caller remains responsible for the surrounding
// transaction and formal Volume update.
func RemoveDatabaseCognitionBinding(baselineValue *Baseline, objectRef string) error {
	if baselineValue == nil || baselineValue.DatabaseCognition == nil {
		return fmt.Errorf("database cognition binding is unavailable")
	}
	found := false
	entries := make([]DatabaseCognitionBinding, 0, len(baselineValue.DatabaseCognition.Entries))
	for _, binding := range baselineValue.DatabaseCognition.Entries {
		if binding.ObjectRef == objectRef {
			found = true
			continue
		}
		entries = append(entries, binding)
	}
	if !found {
		return fmt.Errorf("database cognition binding is missing")
	}
	baselineValue.DatabaseCognition.Entries = entries
	return normalizeDatabaseCognitionBindings(baselineValue.DatabaseCognition)
}

func normalizeDatabaseCognitionBindings(bindings *DatabaseCognitionBindings) error {
	if bindings == nil {
		return nil
	}
	sort.Slice(bindings.Entries, func(i, j int) bool {
		return bindings.Entries[i].ObjectRef < bindings.Entries[j].ObjectRef
	})
	return validateDatabaseCognitionBindings(bindings)
}

func validateDatabaseCognitionBindings(bindings *DatabaseCognitionBindings) error {
	if bindings == nil {
		return nil
	}
	if bindings.Version != machinecontract.DatabaseCognitionBindingVersion {
		return fmt.Errorf("unsupported binding version %q", bindings.Version)
	}
	previous := ""
	for _, binding := range bindings.Entries {
		parts := strings.Split(strings.TrimPrefix(binding.ObjectRef, "database://"), "/")
		validObjectRef := strings.HasPrefix(binding.ObjectRef, "database://") && len(parts) == 3 &&
			parts[0] == binding.SourceID && parts[0] != "" && parts[1] != "" && parts[2] != ""
		if binding.ObjectRef <= previous || !validObjectRef ||
			binding.SourceID == "" || binding.EvidenceVersion == "" ||
			!validBaselineSHA256(binding.TableEvidenceSHA256) || !validBaselineSHA256(binding.EntrySHA256) {
			return fmt.Errorf("invalid or duplicate binding for %q", binding.ObjectRef)
		}
		previous = binding.ObjectRef
	}
	return nil
}

func validBaselineSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func EffectiveRole(fingerprint Fingerprint) string {
	if fingerprint.Role == "" {
		return machinecontract.ScopeRoleIndex
	}
	return fingerprint.Role
}

func validateManagedScope(value *Baseline) error {
	if value == nil {
		return fmt.Errorf("baseline is nil")
	}
	for path, fingerprint := range value.Files {
		role := EffectiveRole(fingerprint)
		if role != machinecontract.ScopeRoleIndex && role != machinecontract.ScopeRoleObserve {
			return fmt.Errorf("invalid role for %q", path)
		}
	}
	if value.ManagedScope == nil {
		return nil
	}
	state := value.ManagedScope
	if state.Version != machinecontract.ManagedScopeBaselineV1 || !validBaselineSHA256(state.PolicyIdentity) ||
		(state.BudgetPolicyIdentity != "" && !validBaselineSHA256(state.BudgetPolicyIdentity)) ||
		(state.HighRiskApprovalDigest != "" && !validBaselineSHA256(state.HighRiskApprovalDigest)) ||
		(state.ObserveChangePolicy != machinecontract.ObserveChangeReviewRequired &&
			state.ObserveChangePolicy != machinecontract.ObserveChangeInformational) {
		return fmt.Errorf("managed scope state is incomplete")
	}
	if state.BudgetPolicy != nil {
		identity, err := cognitionbudget.Identity(*state.BudgetPolicy)
		if err != nil || identity != state.BudgetPolicyIdentity {
			return fmt.Errorf("managed scope budget policy binding is invalid")
		}
	}
	return nil
}

// UpdateOne advances the fingerprint of a single file.
//
// The caller calls it after the formal asset write and is responsible for the following Save.
func UpdateOne(
	baselineValue *Baseline,
	relPath string,
	fingerprint Fingerprint,
) {
	if baselineValue.Files == nil {
		baselineValue.Files = map[string]Fingerprint{}
	}

	baselineValue.Files[relPath] = fingerprint
}
