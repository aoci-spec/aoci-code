// Parallel current batches (L4): one logical plan, up to N sibling batches cut
// from it against one preimage, applied in any order. The plan record keeps
// the identities a sibling was issued against and the preimage bytes; every
// applied sibling leaves the operations it wrote, so the Code Volume a later
// sibling meets is proven by replaying those operations onto the preimage,
// never by comparing a digest predicted at issuance.
package codebatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const (
	ParallelPlanVersion   = "code-cognition-parallel-plan/v1"
	AppliedReceiptVersion = "code-cognition-applied/v1"

	SiblingIssued      = "issued"
	SiblingDelivered   = "delivered"
	SiblingApplied     = "applied"
	SiblingInvalidated = "invalidated"
)

// Sibling is one batch of the logical plan as the wire lists it.
type Sibling struct {
	BatchID  string   `json:"batch_id"`
	Targets  []string `json:"targets"`
	Included int      `json:"included"`
	State    string   `json:"state"`
}

// ParallelPlan is the logical plan on disk: the identities every sibling was
// issued against and the siblings in issuance order. The preimage bytes live
// beside it; both are runtime assets under .aoci and enter no identity.
type ParallelPlan struct {
	Version             string   `json:"version"`
	LogicalPlan         string   `json:"logical_plan"`
	CompositeIdentity   string   `json:"composite_identity"`
	ScopePolicyIdentity string   `json:"scope_policy_identity"`
	CodeVolumePath      string   `json:"code_volume_path"`
	PreimageSHA256      string   `json:"preimage_sha256"`
	RootSHA256          string   `json:"root_sha256"`
	MetaSHA256          string   `json:"meta_sha256"`
	DatabaseSHA256      string   `json:"database_sha256,omitempty"`
	Siblings            []string `json:"siblings"`
	Delivered           []string `json:"delivered,omitempty"`
}

// AppliedOperation is one Entry write a sibling made, exactly as the editor
// made it, so the replay reproduces the bytes.
type AppliedOperation struct {
	Path     string `json:"path"`
	Change   string `json:"change"`
	OldEntry string `json:"old_entry,omitempty"`
	NewEntry string `json:"new_entry"`
}

// AppliedReceipt records one applied sibling: its place in the apply order,
// the Code Volume digests around it, and the operations to replay.
type AppliedReceipt struct {
	Version     string             `json:"version"`
	LogicalPlan string             `json:"logical_plan"`
	BatchID     string             `json:"batch_id"`
	Order       int                `json:"order"`
	PreSHA256   string             `json:"pre_sha256"`
	PostSHA256  string             `json:"post_sha256"`
	Operations  []AppliedOperation `json:"operations"`
}

// LogicalPlanID names the plan every sibling of one preimage belongs to.
func LogicalPlanID(compositeIdentity, scopePolicyIdentity, codeVolumePath, preimageSHA256 string) string {
	return receiptHash(ParallelPlanVersion, compositeIdentity, scopePolicyIdentity, codeVolumePath, preimageSHA256)
}

// BytesSHA256 is the raw digest the plan records compare Volumes by.
func BytesSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func parallelDirectory(root string) string {
	return filepath.Join(root, ".aoci", "drafts", "code-cognition")
}

func parallelPlanPath(root, logical string) string {
	return filepath.Join(parallelDirectory(root), "parallel-"+logical+".json")
}

func preimagePath(root, logical string) string {
	return filepath.Join(parallelDirectory(root), "preimage-"+logical+".bin")
}

func appliedPath(root, batchID string) string {
	return filepath.Join(parallelDirectory(root), "applied-"+batchID+".json")
}

// SaveParallelPlan starts a logical plan: it drops every other plan's
// artifacts, retains the preimage bytes, and writes the record.
func SaveParallelPlan(root string, plan ParallelPlan, preimage []byte) error {
	if err := validateParallelPlan(plan); err != nil {
		return err
	}
	if BytesSHA256(preimage) != plan.PreimageSHA256 {
		return fmt.Errorf("code_parallel_plan_preimage_mismatch")
	}
	if err := RemoveParallelPlans(root); err != nil {
		return err
	}
	if err := os.MkdirAll(parallelDirectory(root), 0o755); err != nil {
		return fmt.Errorf("code_candidate_receipt_directory_unavailable")
	}
	if err := afs.AtomicWrite(preimagePath(root, plan.LogicalPlan), preimage); err != nil {
		return fmt.Errorf("code_parallel_plan_write_failed")
	}
	return writeParallelPlan(root, plan)
}

// UpdateParallelPlan rewrites the record of the current plan (new siblings,
// deliveries); the preimage never changes.
func UpdateParallelPlan(root string, plan ParallelPlan) error {
	if err := validateParallelPlan(plan); err != nil {
		return err
	}
	return writeParallelPlan(root, plan)
}

func writeParallelPlan(root string, plan ParallelPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := afs.AtomicWrite(parallelPlanPath(root, plan.LogicalPlan), append(data, '\n')); err != nil {
		return fmt.Errorf("code_parallel_plan_write_failed")
	}
	return nil
}

// LoadParallelPlan returns the one logical plan on record, or nil when none.
// Two records would mean an interrupted start; the newest by name is not
// trusted either, so both are reported as none and the next issuance starts
// afresh over them.
func LoadParallelPlan(root string) (*ParallelPlan, error) {
	entries, err := os.ReadDir(parallelDirectory(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("code_candidate_receipt_unreadable")
	}
	var found *ParallelPlan
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "parallel-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		plan, loadErr := decodeParallelPlan(filepath.Join(parallelDirectory(root), name))
		if loadErr != nil || found != nil {
			return nil, nil
		}
		found = plan
	}
	return found, nil
}

func decodeParallelPlan(path string) (*ParallelPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := jsonstrict.RejectDuplicateKeysFold(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan ParallelPlan
	if err := decoder.Decode(&plan); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("code_parallel_plan_trailing_json")
	}
	if err := validateParallelPlan(plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func validateParallelPlan(plan ParallelPlan) error {
	path, pathErr := afs.NormalizeRelPath(plan.CodeVolumePath)
	if plan.Version != ParallelPlanVersion || !validSHA256(plan.LogicalPlan) || !validSHA256(plan.CompositeIdentity) ||
		(plan.ScopePolicyIdentity != "" && !validSHA256(plan.ScopePolicyIdentity)) || pathErr != nil || path != plan.CodeVolumePath ||
		!validSHA256(plan.PreimageSHA256) || !validSHA256(plan.RootSHA256) || !validSHA256(plan.MetaSHA256) ||
		(plan.DatabaseSHA256 != "" && !validSHA256(plan.DatabaseSHA256)) {
		return fmt.Errorf("code_parallel_plan_invalid")
	}
	if plan.LogicalPlan != LogicalPlanID(plan.CompositeIdentity, plan.ScopePolicyIdentity, plan.CodeVolumePath, plan.PreimageSHA256) {
		return fmt.Errorf("code_parallel_plan_id_mismatch")
	}
	seen := map[string]bool{}
	for _, batchID := range plan.Siblings {
		if !validSHA256(batchID) || seen[batchID] {
			return fmt.Errorf("code_parallel_plan_invalid")
		}
		seen[batchID] = true
	}
	for _, batchID := range plan.Delivered {
		if !seen[batchID] {
			return fmt.Errorf("code_parallel_plan_invalid")
		}
	}
	return nil
}

// LoadPreimage returns the Code Volume bytes the plan was issued against.
func LoadPreimage(root string, plan *ParallelPlan) ([]byte, error) {
	data, err := os.ReadFile(preimagePath(root, plan.LogicalPlan))
	if err != nil {
		return nil, fmt.Errorf("code_parallel_plan_preimage_missing")
	}
	if BytesSHA256(data) != plan.PreimageSHA256 {
		return nil, fmt.Errorf("code_parallel_plan_preimage_mismatch")
	}
	return data, nil
}

// RemoveParallelPlans drops every logical plan record, its preimage, and its
// applied receipts: the plan completed or was invalidated. Sibling candidate
// receipts stay, as ordinary batch receipts do; they answer stale by identity.
func RemoveParallelPlans(root string) error {
	entries, err := os.ReadDir(parallelDirectory(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("code_candidate_receipt_unreadable")
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "parallel-") || strings.HasPrefix(name, "preimage-") || strings.HasPrefix(name, "applied-") {
			if err := os.Remove(filepath.Join(parallelDirectory(root), name)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("code_parallel_plan_cleanup_failed")
			}
		}
	}
	return nil
}

// SaveApplied records an applied sibling. A record for the batch that is
// byte-identical is an idempotent replay; any other is a conflict.
func SaveApplied(root string, receipt AppliedReceipt) error {
	if err := validateApplied(receipt); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := appliedPath(root, receipt.BatchID)
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("code_parallel_applied_conflict")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("code_candidate_receipt_directory_unavailable")
	}
	if err := afs.AtomicWrite(path, data); err != nil {
		return fmt.Errorf("code_parallel_applied_write_failed")
	}
	return nil
}

func validateApplied(receipt AppliedReceipt) error {
	if receipt.Version != AppliedReceiptVersion || !validSHA256(receipt.LogicalPlan) || !validSHA256(receipt.BatchID) ||
		receipt.Order < 1 || !validSHA256(receipt.PreSHA256) || !validSHA256(receipt.PostSHA256) || len(receipt.Operations) == 0 {
		return fmt.Errorf("code_parallel_applied_invalid")
	}
	for _, operation := range receipt.Operations {
		path, err := afs.NormalizeRelPath(operation.Path)
		if err != nil || path != operation.Path || operation.NewEntry == "" ||
			(operation.Change != "create" && operation.Change != "update") ||
			(operation.Change == "update") != (operation.OldEntry != "") {
			return fmt.Errorf("code_parallel_applied_invalid")
		}
	}
	return nil
}

// LoadApplied returns the applied receipts of the plan in apply order; a
// gap or a duplicate order is a corrupt record and reads as an error.
func LoadApplied(root string, plan *ParallelPlan) ([]AppliedReceipt, error) {
	applied := []AppliedReceipt{}
	for _, batchID := range plan.Siblings {
		data, err := os.ReadFile(appliedPath(root, batchID))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("code_candidate_receipt_unreadable")
		}
		if err := jsonstrict.RejectDuplicateKeysFold(data); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var receipt AppliedReceipt
		if err := decoder.Decode(&receipt); err != nil {
			return nil, err
		}
		if err := validateApplied(receipt); err != nil || receipt.LogicalPlan != plan.LogicalPlan || receipt.BatchID != batchID {
			return nil, fmt.Errorf("code_parallel_applied_invalid")
		}
		applied = append(applied, receipt)
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].Order < applied[j].Order })
	for position, receipt := range applied {
		if receipt.Order != position+1 {
			return nil, fmt.Errorf("code_parallel_applied_order_invalid")
		}
	}
	return applied, nil
}

// Replay applies the operations of the applied receipts, in order, to the
// preimage with the same editor the writes used, and returns the bytes.
func Replay(repoRoot string, preimage []byte, applied []AppliedReceipt) ([]byte, error) {
	text := string(preimage)
	for _, receipt := range applied {
		if BytesSHA256([]byte(text)) != receipt.PreSHA256 {
			return nil, fmt.Errorf("code_parallel_replay_preimage_mismatch")
		}
		for _, operation := range receipt.Operations {
			var next string
			var err error
			if operation.Change == "create" {
				next, err = index.InsertEntry(text, operation.Path, operation.NewEntry, repoRoot)
			} else {
				next, err = index.ReplaceEntryForPath(text, repoRoot, operation.Path, operation.OldEntry, operation.NewEntry)
			}
			if err != nil {
				return nil, fmt.Errorf("code_parallel_replay_failed")
			}
			text = next
		}
		if BytesSHA256([]byte(text)) != receipt.PostSHA256 {
			return nil, fmt.Errorf("code_parallel_replay_postimage_mismatch")
		}
	}
	return []byte(text), nil
}

// ReplayProves reports whether the plan preimage plus the applied siblings
// reproduce the Code Volume digest currentSHA256 byte for byte.
func ReplayProves(root, repoRoot string, plan *ParallelPlan, currentSHA256 string) (bool, error) {
	preimage, err := LoadPreimage(root, plan)
	if err != nil {
		return false, err
	}
	applied, err := LoadApplied(root, plan)
	if err != nil {
		return false, err
	}
	if len(applied) == 0 {
		return plan.PreimageSHA256 == currentSHA256, nil
	}
	if applied[len(applied)-1].PostSHA256 == currentSHA256 {
		// The last receipt already names this digest; the replay below is
		// what proves the chain is honest, so it still runs.
		replayed, replayErr := Replay(repoRoot, preimage, applied)
		if replayErr != nil {
			return false, replayErr
		}
		return BytesSHA256(replayed) == currentSHA256, nil
	}
	return false, nil
}

// AppliedBatchIDs lists the siblings that have an applied receipt.
func AppliedBatchIDs(root string, plan *ParallelPlan) map[string]bool {
	applied := map[string]bool{}
	for _, batchID := range plan.Siblings {
		if _, err := os.Stat(appliedPath(root, batchID)); err == nil {
			applied[batchID] = true
		}
	}
	return applied
}

// CutSibling selects the leading candidates, in plan order, that no issued
// sibling holds, as one batch under the count cap and the byte budget.
func CutSibling(ordered []Candidate, held map[string]bool, limit int, budget Budget) []Candidate {
	free := make([]Candidate, 0, len(ordered))
	for _, candidate := range ordered {
		if !held[candidate.ObjectRef] {
			free = append(free, candidate)
		}
	}
	if len(free) == 0 {
		return nil
	}
	return free[:PrefixWithinBudget(free, limit, budget)]
}

// BuildSiblingPlan issues one sibling of the logical plan: its receipt binds
// the plan preimage, carries the logical plan, and is a complete batch of
// the selected candidates. all is the complete plan order the batch was cut
// from; Remaining is the caller's to fill, since it counts every sibling.
func BuildSiblingPlan(root, logical, compositeIdentity, scopePolicyIdentity, codeVolumePath, preimageSHA256 string, all, selected []Candidate, limit int, budget Budget) (Plan, error) {
	if limit < 1 || len(selected) == 0 || !validSHA256(logical) {
		return Plan{}, fmt.Errorf("code_candidate_batch_limit_invalid")
	}
	ordered, _ := Select(all, 0)
	if err := validateCandidates(ordered); err != nil {
		return Plan{}, err
	}
	chosen, _ := Select(selected, 0)
	plan, err := savePlan(root, compositeIdentity, scopePolicyIdentity, codeVolumePath, preimageSHA256, ordered, chosen, limit, logical)
	if err != nil {
		return Plan{}, err
	}
	plan.TransportBudgetBytes = budget.Total
	if plan.TransportBudgetBytes == 0 {
		plan.TransportBudgetBytes = budget.Bytes
	}
	return plan, nil
}

// ParallelIdentities are the formal digests a logical plan is issued against
// and a sibling is judged by: a change to any of them that no sibling's
// Apply explains invalidates every outstanding sibling.
type ParallelIdentities struct {
	CompositeIdentity   string
	ScopePolicyIdentity string
	CodeVolumePath      string
	CodeVolumeSHA256    string
	RootSHA256          string
	MetaSHA256          string
	DatabaseSHA256      string
}

// PlanHolds reports whether the plan was issued against the same formal
// assets the identities describe, apart from the Code Volume itself, whose
// current bytes the replay judges.
func (plan *ParallelPlan) PlanHolds(ids ParallelIdentities) bool {
	return plan != nil && plan.ScopePolicyIdentity == ids.ScopePolicyIdentity && plan.CodeVolumePath == ids.CodeVolumePath &&
		plan.RootSHA256 == ids.RootSHA256 && plan.MetaSHA256 == ids.MetaSHA256 && plan.DatabaseSHA256 == ids.DatabaseSHA256
}

// NewParallelPlan is the record a logical plan starts with.
func NewParallelPlan(ids ParallelIdentities) ParallelPlan {
	return ParallelPlan{Version: ParallelPlanVersion,
		LogicalPlan:       LogicalPlanID(ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256),
		CompositeIdentity: ids.CompositeIdentity, ScopePolicyIdentity: ids.ScopePolicyIdentity, CodeVolumePath: ids.CodeVolumePath,
		PreimageSHA256: ids.CodeVolumeSHA256, RootSHA256: ids.RootSHA256, MetaSHA256: ids.MetaSHA256, DatabaseSHA256: ids.DatabaseSHA256,
		Siblings: []string{}}
}

// HasSibling reports whether the plan issued batchID.
func (plan *ParallelPlan) HasSibling(batchID string) bool {
	for _, sibling := range plan.Siblings {
		if sibling == batchID {
			return true
		}
	}
	return false
}

// ReceiptSourcesCurrent reports whether every target of the receipt still has
// the bytes it was issued against.
func ReceiptSourcesCurrent(root string, receipt Receipt) bool {
	return receiptSourcesCurrent(root, receipt)
}

// ValidateParallelSubmission is ValidateSubmission for a sibling of a logical
// plan: the receipt is accepted when the plan still holds and the current
// Code Volume is the plan preimage or a postimage the applied siblings
// reproduce by replay. allowPostimage is the recovery path's own proof and
// skips the replay as it skips the identity checks of a single batch.
func ValidateParallelSubmission(root, repoRoot, batchID string, ids ParallelIdentities, submissions []Submission, allowPostimage bool) (Receipt, *ParallelPlan, error) {
	receipt, err := LoadReceipt(root, batchID)
	if err != nil {
		return Receipt{}, nil, err
	}
	if receipt.LogicalPlan == "" {
		return Receipt{}, nil, fmt.Errorf("code_candidate_batch_not_parallel")
	}
	plan, err := LoadParallelPlan(root)
	if err != nil {
		return Receipt{}, nil, err
	}
	if plan == nil || plan.LogicalPlan != receipt.LogicalPlan || !plan.HasSibling(batchID) || receipt.CodeVolumePath != plan.CodeVolumePath {
		return Receipt{}, nil, fmt.Errorf("code_candidate_plan_stale")
	}
	if !allowPostimage {
		if !plan.PlanHolds(ids) {
			return Receipt{}, nil, fmt.Errorf("code_candidate_plan_stale")
		}
		proven, replayErr := ReplayProves(root, repoRoot, plan, ids.CodeVolumeSHA256)
		if replayErr != nil || !proven {
			return Receipt{}, nil, fmt.Errorf("code_candidate_volume_preimage_stale")
		}
	}
	if AppliedBatchIDs(root, plan)[batchID] && !allowPostimage {
		return Receipt{}, nil, fmt.Errorf("code_candidate_batch_already_applied")
	}
	if err := matchSubmission(root, receipt, submissions); err != nil {
		return Receipt{}, nil, err
	}
	return receipt, plan, nil
}
