// Entry removal pipeline and the aoci_remove_entry tool (v2.8 P1, the second half of D75: the execution tool for a human decision).
// Index entry: tools_remove.go[MWR8AM]
//
// Position: a plan/commit staged pipeline of the same family as ApplyUpdateEntry in tools_write.go,
// the single implementation shared by MCP and CLI (against hidden twins). It is its own file rather than part of tools_write.go
// (the split-file precedent, avoiding a whole-file overwrite of an existing large file).
//
// Staged shape (the phase D refactor): the original inline plan/commit was refactored into the explicit planRemoveEntry/commitRemove
// stages, isomorphic with the update pipeline. Motivation: once the concurrency defences were wired in, the critical-section shape of the two pipelines
// had to agree (deliberate isomorphism against hidden divergence); and only the staged form makes the CAS conflict path testable (a test can tamper with the
// index between plan and commit, a window the inline form never offered). removePlan is unexported, so no package outside can
// forge a plan and call commit (the R18 structural guarantee, the same family as planResult).
//
// Concurrency defences (phase D, the same two defences as tools_write.go, see its package comment):
// defence one = the cross-process fs.AcquireIndexLock write lock wraps only commit;
// defence two = the index hash CAS: plan records the sha256 of the index it read, commit re-reads under the lock and compares,
// and a mismatch refuses with write_conflict. A lock timeout maps to write_conflict, a lock IO fault to internal,
// and a failed unlock only warns on stderr without polluting the business result.
//
// Differentiated guardrail (orphanOnly, the core decision of this batch):
//   - the MCP side (the agent) always passes orphanOnly=true: only orphan entries may be removed
//     (the target file is no longer on disk), and an entry for a live file is always refused with a pointer to aoci_report.
//     Reason: the S field is organizational scar tissue, and an agent deleting a live file's scars by mistake costs far more than one leftover orphan;
//     the agent's real removal scenario is exactly cleaning up the deleted orphans that index update reports.
//   - the CLI side (a person) passes orphanOnly=false with full authority: removing a live file's entry is legal
//     (the file then surfaces as Missing, which is the correct semantics: on disk, in the index no more, an entry waiting to be rebuilt).
//
// Baseline discipline: after a successful removal the index's own fingerprint always advances (the D51 family; otherwise every removal
// manufactures a false Stale on aoci.txt); the first argument of baseline.Load/Save is the repository root, not the baseline file path
// (corrected 2026-07-12: the first version passed BaselinePath by mistake, string for string compiled fine but Load
// never found the baseline → the advance was skipped silently → every removal manufactured a false Stale; the tests stayed green because the fixture had no baseline,
// leaving the advance branch at zero coverage; the models server.go/index_header.go, both passing root, were the evidence for the decision);
// the target file's leftover key in the baseline is deliberately not cleaned up: Detect's four states go by the disk snapshot and the index,
// the leftover is harmless, and the baseline package deliberately offers no delete-key API (a baseline only adds and changes, keeping its semantics simple).
package mcptools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// RemoveOutcome is the removal result (rendered on both the MCP and the CLI side)
type RemoveOutcome struct {
	// Rel is the normalized target relative path
	Rel string
	// RemovedLine is the whole removed entry line (echoed for confirmation and audit)
	RemovedLine string
	// DryRun is the dry-run flag (true = not written to disk)
	DryRun bool
	// OwnershipRepair marks the machine-proven ownership-conflict path. It
	// changes only the success rendering; the write still uses this remove
	// pipeline and its existing transaction authorities.
	OwnershipRepair bool
	// PreservedOwner is the deterministic owner whose formal asset is guarded
	// and left untouched while the misplaced Domain Entry is removed.
	PreservedOwner string
	// Superseded reports that a receipt the Volume had moved past was closed
	// because its object was already absent; no index or Baseline bytes moved.
	Superseded bool
	// StaleReceiptDiscarded reports that a receipt bound to a Volume version
	// that no longer existed was discarded before this fresh removal.
	StaleReceiptDiscarded bool
}

// removePlan is the removal plan (unexported): plan produces it, commit consumes it, and no package outside can forge it (R18).
type removePlan struct {
	// out is the rendering-facing result skeleton
	out *RemoveOutcome
	// newText is the whole index text after the removal (the material to write)
	newText string
	// rc is the repository context the plan phase read
	rc *repoCtx
	// indexHash is the sha256 of the whole index text the plan read (the CAS credential; the algorithm shares indexTextHash with the update pipeline)
	indexHash string
	// alreadyApplied means the index already sits at the removal postimage bound by a persistent recovery intent, and this round only completes the Baseline.
	alreadyApplied bool
	// superseded marks a receipt the index or Volume has moved past while its
	// object is already absent: the commit closes the receipt and writes
	// nothing else.
	superseded bool
	// staleReceipt is the receipt the plan judged stale (superseded or to be
	// discarded); the commit re-reads it under the lock and acts only if it is
	// still that receipt and the target still matches neither of its images.
	staleReceipt *removeRecovery
	// orphanOnly demands that both the plan and the locked commit prove the target path really does not exist.
	orphanOnly bool
	// volumeMode reuses this established explicit-remove pipeline for one
	// proven Code or Database Cognition orphan. Root and Meta remain guards.
	volumeMode         bool
	objectRef          string
	volumeID           string
	guardSHA256        map[string]string
	evidenceIdentity   string
	baselinePreSHA256  string
	baselinePostSHA256 string
	baselinePostimage  string
}

var writeRemoveIndex = fs.AtomicWriteCAS
var removeRecoveryFile = os.Remove
var lstatRemoveTarget = os.Lstat

type removeRecovery struct {
	Version            int               `json:"version"`
	Rel                string            `json:"rel"`
	RemovedLine        string            `json:"removed_line"`
	PreIndexSHA256     string            `json:"pre_index_sha256"`
	PostIndexSHA256    string            `json:"post_index_sha256"`
	Completed          bool              `json:"completed,omitempty"`
	VolumeMode         bool              `json:"volume_mode,omitempty"`
	ObjectRef          string            `json:"object_ref,omitempty"`
	VolumeID           string            `json:"volume_id,omitempty"`
	VolumePath         string            `json:"volume_path,omitempty"`
	VolumePostimage    string            `json:"volume_postimage,omitempty"`
	GuardSHA256        map[string]string `json:"guard_sha256,omitempty"`
	EvidenceIdentity   string            `json:"evidence_identity,omitempty"`
	BaselinePreSHA256  string            `json:"baseline_pre_sha256,omitempty"`
	BaselinePostSHA256 string            `json:"baseline_post_sha256,omitempty"`
	BaselinePostimage  string            `json:"baseline_postimage,omitempty"`
	OwnershipRepair    bool              `json:"ownership_repair,omitempty"`
	PreservedOwner     string            `json:"preserved_owner,omitempty"`
}

// removeEntryArgs are the inputs of aoci_remove_entry (an In struct lets go-sdk derive the schema and validate up front)
type removeEntryArgs struct {
	Path string `json:"path"`
}

// registerRemoveTool registers aoci_remove_entry (mounted from RunStdio in server.go).
// The agent side is always orphanOnly=true (the guardrail in the package comment); the handler is wrapped by guard to recover from panics.
func registerRemoveTool(
	srv *mcp.Server,
	root string,
	descriptions mcpToolDescriptions,
	inputSchemas mcpInputSchemas,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "aoci_remove_entry",
		Description: descriptions[textassets.ContractMCPRemoveDescription],
		InputSchema: inputSchemas["aoci_remove_entry"],
	}, func(ctx context.Context, req *mcp.CallToolRequest, in removeEntryArgs) (*mcp.CallToolResult, any, error) {
		res := guard(func() *mcp.CallToolResult {
			out, fail := ApplyRemoveEntry(root, in.Path, "agent", true, false)
			if fail != nil {
				return failResult(fail)
			}
			return textResult(RenderRemoveOutcome(out))
		})
		return res, nil, nil
	})
}

// planRemoveEntry is the first half of the removal pipeline: path normalization → load → locate → guardrail → text transformation (in memory), pure reading with zero side effects.
func planRemoveEntry(root, rawPath string, orphanOnly bool) (*removePlan, *Fail) {
	trimmed := strings.TrimSpace(rawPath)
	if strings.HasPrefix(trimmed, "code:") || cognition.IsCanonicalDatabaseRef(trimmed) {
		return planVolumeRemoveEntry(root, trimmed, orphanOnly)
	}
	rel, err := fs.NormalizeRelPath(rawPath)
	if err != nil {
		return nil, &Fail{
			Code: "path_unsafe",
			Msg: writeMessage(
				"remove.path_invalid",
				localizeRelPathError(err),
			),
			Hint: writeMessage("remove.hint.relative_path"),
		}
	}

	// A plain repository-relative path under a Volumes v1 layout names the
	// Code object code:<path>; route it to the Volume pipeline instead of the
	// Legacy loader, whose volume_read_only refusal never named that spelling
	// (#112).
	if ref, isVolume := volumeCodeRef(root, rel); isVolume {
		return planVolumeRemoveEntry(root, ref, orphanOnly)
	}

	// Not a Volumes object: a Legacy single-file layout, read-only since
	// 0.2.0, or no layout at all. Let the Legacy loader name an absent or
	// Volumes index as before, and refuse the Legacy write itself.
	if _, fail := loadRepoCtx(root); fail != nil {
		return nil, fail
	}
	return nil, legacyReadOnlyFail()
}

// volumeCodeRef maps a plain repository-relative path onto its canonical Code
// identity when the repository is a Volumes v1 layout, so `--path dir/file.go`
// and `path: "dir/file.go"` reach the same guarded Volume pipeline as
// `code:dir/file.go`. Any load failure answers false and the Legacy path keeps
// its own diagnostics.
func volumeCodeRef(root, rel string) (string, bool) {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return "", false
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return "", false
	}
	return "code:" + rel, true
}

func removeRecoveryPath(root, rel string) string {
	digest := sha256.Sum256([]byte(rel))
	return filepath.Join(root, ".aoci", "transactions", "remove-"+hex.EncodeToString(digest[:])+".json")
}

func saveRemoveRecovery(root string, recovery removeRecovery) error {
	path := removeRecoveryPath(root, recovery.Rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recovery, "", "  ")
	if err != nil {
		return err
	}
	return fs.AtomicWrite(path, append(data, '\n'))
}

func loadRemoveRecovery(root, rel string) (*removeRecovery, error) {
	data, err := os.ReadFile(removeRecoveryPath(root, rel))
	if err != nil {
		return nil, err
	}
	var recovery removeRecovery
	if err := json.Unmarshal(data, &recovery); err != nil {
		return nil, err
	}
	if recovery.Version != 1 || recovery.Rel != rel || recovery.RemovedLine == "" ||
		!validRecoverySHA256(recovery.PreIndexSHA256) ||
		!validRecoverySHA256(recovery.PostIndexSHA256) {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	if recovery.VolumeMode && (recovery.ObjectRef != recovery.Rel || recovery.VolumeID == "" || recovery.VolumePath == "" ||
		recovery.VolumePostimage == "" || len(recovery.GuardSHA256) == 0 || recovery.EvidenceIdentity == "" ||
		!validRecoverySHA256(recovery.BaselinePreSHA256) || !validRecoverySHA256(recovery.BaselinePostSHA256) ||
		recovery.BaselinePostimage == "") {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	if recovery.OwnershipRepair && (recovery.PreservedOwner == "" || recovery.PreservedOwner == recovery.VolumeID) {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	return &recovery, nil
}

// ApplyRemoveEntry removes the index entry of the given file (a thin plan/commit composition, the single implementation for both sides).
// With orphanOnly=true a target file still on disk is refused (the MCP/agent guardrail);
// with dryRun=true it returns after plan without writing, advancing, or recording.
func ApplyRemoveEntry(root, rawPath, source string, orphanOnly, dryRun bool) (*RemoveOutcome, *Fail) {
	if fail := validateWriteMessages(requiredRemoveMessages); fail != nil {
		return nil, fail
	}
	p, fail := planRemoveEntry(root, rawPath, orphanOnly)
	if fail != nil {
		if !dryRun {
			if cleanupFail := cleanupCompletedRemoveRecovery(root, rawPath); cleanupFail != nil {
				return nil, cleanupFail
			}
		}
		return nil, fail
	}
	p.out.DryRun = dryRun
	if dryRun {
		return p.out, nil
	}
	// planRemoveEntry answers a Volumes plan or a refusal: the Legacy commit
	// left with the Legacy authoring surfaces (0.2.0).
	if fail := commitVolumeRemove(root, source, p); fail != nil {
		return nil, fail
	}
	return p.out, nil
}

func cleanupCompletedRemoveRecovery(root, rawPath string) *Fail {
	rel, err := fs.NormalizeRelPath(rawPath)
	if err != nil {
		return nil
	}
	recovery, err := loadRemoveRecovery(root, rel)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"remove.completed_recovery_read_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	if recovery == nil || !recovery.Completed {
		return nil
	}
	if err := removeRecoveryFile(removeRecoveryPath(root, rel)); err != nil && !os.IsNotExist(err) {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"remove.completed_recovery_cleanup_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	return nil
}

// RenderRemoveOutcome renders the removal result wording (shared by both sides).
// The preview branch does not print "baseline advanced" (a wording defect the 2026-07-12 CLI smoke run proved: a dry run claimed an advance it never made).
func RenderRemoveOutcome(o *RemoveOutcome) string {
	if o.DryRun && o.Superseded {
		return writeMessage("remove.preview_superseded", o.Rel, o.RemovedLine)
	}
	if o.DryRun {
		return writeMessage("remove.preview", o.Rel, o.RemovedLine)
	}
	if o.Superseded {
		return writeMessage("remove.recovery_superseded", o.Rel, o.RemovedLine)
	}
	prefix := ""
	if o.StaleReceiptDiscarded {
		prefix = writeMessage("remove.stale_receipt_discarded", o.Rel) + "\n"
	}
	if o.OwnershipRepair {
		return prefix + writeMessage("remove.ownership_repair_applied", o.Rel, o.PreservedOwner, true, true, o.RemovedLine)
	}
	return prefix + writeMessage("remove.applied", o.Rel, o.RemovedLine)
}

// receiptKey is the identity a removal's receipt is filed under: the canonical
// object reference in Volumes, the repository-relative path in Legacy.
func (p *removePlan) receiptKey() string {
	if p.volumeMode {
		return p.objectRef
	}
	return p.out.Rel
}

// rejectOtherRemovePending fails closed on every pending receipt except this
// removal's own: a foreign file, another object's receipt, or an unfinished
// batch means the state is not one this write can prove it is extending.
func rejectOtherRemovePending(root string, p *removePlan) *Fail {
	other, err := cognitiontxn.OtherPending(root, filepath.Base(removeRecoveryPath(root, p.receiptKey())))
	if err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_read_failed", localeSafeWriteDetail(err.Error()))}
	}
	if other != "" {
		return &Fail{Code: errWriteConflict, Msg: "remove_other_transaction_pending: " + other,
			Hint: writeMessage("remove.hint.inspect_recovery")}
	}
	return nil
}

// closeSupersededRemove finishes a receipt the index or Volume has moved past
// while its object is already absent: the Ledger line first, so a retry after
// a later failure meets an equal terminal event, then the completion marker,
// then the file. Nothing else is written; the transactions that moved past
// the receipt already governed the index and the Baseline, and a receipt's
// stale postimages must never be written over a newer state. The Ledger id
// carries the receipt's preimage so a later removal of a re-created object
// never collides with this one.
func closeSupersededRemove(root, source string, p *removePlan, objectAbsent func() *Fail) *Fail {
	if fail := objectAbsent(); fail != nil {
		return fail
	}
	recovery, fail := reloadStaleReceipt(root, p)
	if fail != nil {
		return fail
	}
	if recovery == nil {
		return nil
	}
	key := p.receiptKey()
	event := ledger.Event{Op: "remove_entry", Source: source, Result: ledger.ResultOK, RecoveredCount: 1, DuplicateApplies: 1,
		RecoveryTransactionID: removeTransactionID(root, key) + "-superseded-" + shortSHA(recovery.PreIndexSHA256),
		PreIndexSHA256:        recovery.PreIndexSHA256, PostIndexSHA256: recovery.PostIndexSHA256, FallbackReason: "receipt_superseded"}
	if p.volumeMode {
		// Volumes completion is a terminal Ledger event, as its ordinary commit
		// writes; Legacy never required the Ledger and keeps the tolerant append.
		if err := cognitiontxn.EnsureLedger(root, p.rc.cfg.LedgerEnabled, event); err != nil {
			return &Fail{Code: errInternal, Msg: "remove_ledger_completion_failed: " + localeSafeWriteDetail(err.Error()),
				Hint: writeMessage("remove.hint.retry_recovery")}
		}
	} else {
		ledger.Append(root, p.rc.cfg.LedgerEnabled, event)
	}
	recovery.Completed = true
	if err := saveRemoveRecovery(root, *recovery); err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.completion_marker_failed", localeSafeWriteDetail(err.Error())),
			Hint: writeMessage("remove.hint.retry_recovery")}
	}
	if err := removeRecoveryFile(removeRecoveryPath(root, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "remove_recovery_cleanup_failed"}
	}
	return nil
}

// discardStaleReceipt drops, under the lock, a receipt the plan judged stale
// because its object is still present: it binds an index or Volume version
// that no longer exists, and this call is the fresh removal decision. The
// Ledger records the discard with the receipt's images before the file goes,
// so the removed line it carried is not lost with it.
func discardStaleReceipt(root, source string, p *removePlan) *Fail {
	recovery, fail := reloadStaleReceipt(root, p)
	if fail != nil {
		return fail
	}
	if recovery == nil {
		return nil
	}
	raw, readErr := os.ReadFile(p.rc.paths.IndexPath)
	if readErr != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.cas_read_failed", localeSafeWriteDetail(readErr.Error()))}
	}
	if indexTextHash(string(raw)) != p.indexHash {
		// The object the plan judged present with its recorded line is only
		// proven by the target still being the plan's preimage.
		return &Fail{Code: errWriteConflict, Msg: writeMessage("remove.cas_stale"), Hint: writeMessage("remove.hint.replan")}
	}
	key := p.receiptKey()
	ledger.Append(root, p.rc.cfg.LedgerEnabled, ledger.Event{Op: "remove_entry", Source: source, Result: ledger.ResultOK,
		RecoveryTransactionID: removeTransactionID(root, key) + "-discarded-" + shortSHA(recovery.PreIndexSHA256),
		PreIndexSHA256:        recovery.PreIndexSHA256, PostIndexSHA256: recovery.PostIndexSHA256, FallbackReason: "stale_receipt_discarded"})
	if err := removeRecoveryFile(removeRecoveryPath(root, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "remove_stale_receipt_discard_failed"}
	}
	return nil
}

// reloadStaleReceipt re-reads the receipt the plan judged stale and proves,
// under the lock, that it is still that receipt and that the index or Volume
// still matches neither of its images. A receipt that now matches one is not
// stale: the ordinary resume arms own it on the next call.
func reloadStaleReceipt(root string, p *removePlan) (*removeRecovery, *Fail) {
	judged := p.staleReceipt
	current, err := loadRemoveRecovery(root, p.receiptKey())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_read_failed", localeSafeWriteDetail(err.Error()))}
	}
	if judged == nil || current.PreIndexSHA256 != judged.PreIndexSHA256 || current.PostIndexSHA256 != judged.PostIndexSHA256 {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_receipt_changed_since_plan", Hint: writeMessage("remove.hint.replan")}
	}
	raw, readErr := os.ReadFile(p.rc.paths.IndexPath)
	if readErr != nil {
		return nil, &Fail{Code: errInternal, Msg: writeMessage("remove.cas_read_failed", localeSafeWriteDetail(readErr.Error()))}
	}
	if hash := indexTextHash(string(raw)); hash == current.PreIndexSHA256 || hash == current.PostIndexSHA256 {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_receipt_no_longer_stale", Hint: writeMessage("remove.hint.replan")}
	}
	return current, nil
}

func removeTransactionID(root, key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(removeRecoveryPath(root, key)), "remove-"), ".json")
}

func shortSHA(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
