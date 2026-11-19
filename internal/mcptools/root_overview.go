package mcptools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The Root overview is the two lines of a Volumes v1 Root a model authors:
// what the project is, and the invariants that hold across it. init writes a
// name and "-"; the ordinary init, scan, Maintain route had no way to author
// them afterwards, so every index built that way delivered an empty overview
// with every Overview. An aligned result now invites the model to author
// them, and aoci_update_entry root_overview writes them.
//
// The write is its own small transaction, never part of the Entries batch:
// the Root and the Baseline record of the Root move together under one
// receipt, .aoci/transactions/root-overview.json, which holds both postimages
// and is rolled forward by the next aoci_maintain or aoci_update_entry.
const (
	rootOverviewReceiptName = cognitiontxn.OperationRootOverview + "-overview.json"
	rootProjectPrefix       = "#Project:"
	rootInvariantsPrefix    = "#Global-Invariants:"
	rootOverviewUnauthored  = "-"
)

// rootOverviewIn is the root_overview object of aoci_update_entry.
type rootOverviewIn struct {
	Project          string `json:"project"`
	GlobalInvariants string `json:"global_invariants"`
	RootSHA256       string `json:"root_sha256"`
}

// rootOverviewInvite rides on an aligned result while the Root's invariants
// are unauthored. It is advisory: alignment never depends on it.
type rootOverviewInvite struct {
	RootSHA256               string   `json:"root_sha256"`
	Project                  string   `json:"project"`
	GlobalInvariants         string   `json:"global_invariants"`
	ProjectMaxChars          int      `json:"project_max_chars"`
	GlobalInvariantsMaxChars int      `json:"global_invariants_max_chars"`
	Instructions             []string `json:"instructions"`
}

// rootOverviewResult reports one root_overview submission.
type rootOverviewResult struct {
	Status         string `json:"status"`
	RootSHA256     string `json:"root_sha256"`
	BaselineBound  bool   `json:"baseline_bound"`
	AlreadyApplied bool   `json:"already_applied,omitempty"`
}

type rootOverviewReceipt struct {
	Version             int    `json:"version"`
	RootPath            string `json:"root_path"`
	PreRootSHA256       string `json:"pre_root_sha256"`
	PostRootSHA256      string `json:"post_root_sha256"`
	RootPostimage       string `json:"root_postimage"`
	BaselinePreSHA256   string `json:"baseline_pre_sha256,omitempty"`
	BaselinePostSHA256  string `json:"baseline_post_sha256,omitempty"`
	BaselinePostimage   string `json:"baseline_postimage,omitempty"`
	LedgerTransactionID string `json:"ledger_transaction_id"`
}

func rootOverviewReceiptPath(root string) string {
	return filepath.Join(root, ".aoci", "transactions", rootOverviewReceiptName)
}

// rootOverviewFields reads the two authored lines of a Root. A Root without
// one of them reads as unauthored.
func rootOverviewFields(raw []byte) (project, invariants string) {
	project, invariants = rootOverviewUnauthored, rootOverviewUnauthored
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, rootProjectPrefix):
			project = strings.TrimSpace(strings.TrimPrefix(line, rootProjectPrefix))
		case strings.HasPrefix(line, rootInvariantsPrefix):
			invariants = strings.TrimSpace(strings.TrimPrefix(line, rootInvariantsPrefix))
		}
	}
	return project, invariants
}

func rootOverviewAuthored(value string) bool {
	return value != "" && value != rootOverviewUnauthored && value != "MODEL_AUTHORING_REQUIRED"
}

// rewriteRootOverview replaces the two lines, keeping every other byte and
// the file's line ending. A Root that lacks a line gets it after the line
// that precedes it in the template (Locale, then Project).
func rewriteRootOverview(raw []byte, project, invariants string) []byte {
	newline := "\n"
	if strings.Contains(string(raw), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	replace := func(prefix, value, after string) {
		for index, line := range lines {
			if strings.HasPrefix(line, prefix) {
				lines[index] = prefix + " " + value
				return
			}
		}
		at := 0
		for index, line := range lines {
			if strings.HasPrefix(line, after) {
				at = index + 1
			}
		}
		if at == 0 {
			at = 1
		}
		if at > len(lines) {
			at = len(lines)
		}
		lines = append(lines[:at], append([]string{prefix + " " + value}, lines[at:]...)...)
	}
	replace(rootProjectPrefix, project, "#Locale:")
	replace(rootInvariantsPrefix, invariants, rootProjectPrefix)
	return []byte(strings.Join(lines, newline))
}

// rootOverviewValueFinding names why one authored value is refused, or "".
func rootOverviewValueFinding(field, value string, maxChars int) string {
	switch {
	case !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00"):
		return "root_overview_" + field + "_not_one_line"
	case !rootOverviewAuthored(strings.TrimSpace(value)):
		return "root_overview_" + field + "_unauthored"
	case utf8.RuneCountInString(strings.TrimSpace(value)) > maxChars:
		return "root_overview_" + field + "_too_long"
	}
	return ""
}

// rootOverviewInvitation answers the invitation an aligned result carries, or
// nil: the index must be aligned, hold Code Entries, and still have its
// invariants unauthored. The caller passes facts it already computed.
func rootOverviewInvitation(loaded *cognitionRepoCtx, facts *volumegovernance.Facts) *rootOverviewInvite {
	if loaded == nil || loaded.set == nil || facts == nil || !facts.GovernanceAligned || facts.CodeEntryCount == 0 ||
		loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil
	}
	project, invariants := rootOverviewFields(loaded.set.Root.Raw)
	if rootOverviewAuthored(invariants) {
		return nil
	}
	return &rootOverviewInvite{
		RootSHA256: loaded.set.Root.SHA256, Project: project, GlobalInvariants: invariants,
		ProjectMaxChars:          machinecontract.RootOverviewProjectMaxChars,
		GlobalInvariantsMaxChars: machinecontract.RootOverviewInvariantsMaxChars,
		Instructions:             []string{writeMessage("root_overview.invite")},
	}
}

// applyRootOverview writes the two lines. It never plans Entries and takes no
// part in the Entries batch; a repository that is not aligned is refused, so
// an overview is always written over cognition the model has finished.
func applyRootOverview(root, source string, in rootOverviewIn) (*rootOverviewResult, *Fail) {
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, &Fail{Code: errWriteConflict, Msg: "root_overview_lock_failed"}
	}
	defer lock.Release()
	if fail := completeRootOverviewLocked(root, source); fail != nil {
		return nil, fail
	}
	if other, pendingErr := cognitiontxn.OtherPending(root, rootOverviewReceiptName); pendingErr != nil {
		return nil, &Fail{Code: errInternal, Msg: "root_overview_recovery_read_failed"}
	} else if other != "" {
		return nil, &Fail{Code: errWriteConflict, Msg: "root_overview_other_transaction_pending: " + other}
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, &Fail{Code: errBadArgs, Msg: machinecontract.StatusLegacyReadOnlyMigrateRequired}
	}
	project, invariants := strings.TrimSpace(in.Project), strings.TrimSpace(in.GlobalInvariants)
	for _, finding := range []string{
		rootOverviewValueFinding("project", in.Project, machinecontract.RootOverviewProjectMaxChars),
		rootOverviewValueFinding("global_invariants", in.GlobalInvariants, machinecontract.RootOverviewInvariantsMaxChars),
	} {
		if finding != "" {
			return nil, &Fail{Code: errCandidateInvalid, Msg: finding, Repairable: true}
		}
	}
	rootRaw, rootSHA := loaded.set.Root.Raw, loaded.set.Root.SHA256
	post := rewriteRootOverview(rootRaw, project, invariants)
	if string(post) == string(rootRaw) {
		// The same overview submitted again, or a receipt just rolled forward.
		return &rootOverviewResult{Status: autoStatusApplied, RootSHA256: rootSHA, AlreadyApplied: true,
			BaselineBound: loaded.bl != nil && loaded.bl.Files[loaded.cfg.IndexPath].SHA256 == rootSHA}, nil
	}
	if in.RootSHA256 != rootSHA {
		return nil, &Fail{Code: errWriteConflict, Msg: "root_overview_root_changed: current root_sha256=" + rootSHA, Repairable: true}
	}
	facts, assessErr := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if assessErr != nil || !facts.GovernanceAligned {
		return nil, &Fail{Code: errWriteConflict, Msg: "root_overview_requires_aligned_index"}
	}
	receipt := rootOverviewReceipt{Version: 1, RootPath: loaded.cfg.IndexPath, PreRootSHA256: rootSHA,
		PostRootSHA256: indexTextHash(string(post)), RootPostimage: string(post)}
	// The Baseline joins only when it records the Root; several legal layouts
	// never bind it, and an absent record is not enrolled here.
	if loaded.bl != nil {
		if recorded, exists := loaded.bl.Files[loaded.cfg.IndexPath]; exists {
			baselineRaw, readErr := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
			if readErr != nil {
				return nil, &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_read_failed"}
			}
			fingerprint := baseline.HashBytes(loaded.cfg.IndexPath, post)
			fingerprint.Role = recorded.Role
			baseline.UpdateOne(loaded.bl, loaded.cfg.IndexPath, fingerprint)
			loaded.bl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			baselinePost, marshalErr := baseline.MarshalExact(loaded.bl)
			if marshalErr != nil {
				return nil, &Fail{Code: errInternal, Msg: "root_overview_baseline_postimage_invalid"}
			}
			receipt.BaselinePreSHA256, receipt.BaselinePostSHA256 = indexTextHash(string(baselineRaw)), indexTextHash(string(baselinePost))
			receipt.BaselinePostimage = string(baselinePost)
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, &Fail{Code: errInternal, Msg: "root_overview_transaction_id_unavailable"}
	}
	receipt.LedgerTransactionID = hex.EncodeToString(nonce[:])
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, &Fail{Code: errInternal, Msg: "root_overview_receipt_invalid"}
	}
	path := rootOverviewReceiptPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, &Fail{Code: errInternal, Msg: "root_overview_receipt_save_failed"}
	}
	if err := afs.AtomicWrite(path, append(data, '\n')); err != nil {
		return nil, &Fail{Code: errInternal, Msg: "root_overview_receipt_save_failed"}
	}
	if fail := completeRootOverviewLocked(root, source); fail != nil {
		fail.FormalWritesStarted = true
		return nil, fail
	}
	return &rootOverviewResult{Status: autoStatusApplied, RootSHA256: receipt.PostRootSHA256, BaselineBound: receipt.BaselinePostimage != ""}, nil
}

// completeRootOverviewRecovery rolls a pending Root overview receipt forward.
// aoci_maintain and aoci_update_entry call it first, so a write cut short
// heals on the next call instead of standing in the way as a pending receipt.
func completeRootOverviewRecovery(root, source string) *Fail {
	if _, err := os.Stat(rootOverviewReceiptPath(root)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return &Fail{Code: errWriteConflict, Msg: "root_overview_lock_failed"}
	}
	defer lock.Release()
	return completeRootOverviewLocked(root, source)
}

// rootOverviewWriteCAS is the CAS replace, a variable so a test can cut the
// transaction between its two writes.
var rootOverviewWriteCAS = afs.AtomicWriteCAS

// completeRootOverviewLocked advances the receipt, if one is pending: each
// file is written when it holds its preimage and accepted when it already
// holds its postimage. Any other bytes are a third party's and stop it, with
// the receipt left in place.
func completeRootOverviewLocked(root, source string) *Fail {
	path := rootOverviewReceiptPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &Fail{Code: errInternal, Msg: "root_overview_receipt_read_failed"}
	}
	var receipt rootOverviewReceipt
	if jsonstrict.RejectDuplicateKeysFold(data) != nil || json.Unmarshal(data, &receipt) != nil || receipt.Version != 1 ||
		receipt.RootPath == "" || !validRecoverySHA256(receipt.PreRootSHA256) ||
		indexTextHash(receipt.RootPostimage) != receipt.PostRootSHA256 ||
		(receipt.BaselinePostimage != "" && indexTextHash(receipt.BaselinePostimage) != receipt.BaselinePostSHA256) {
		return &Fail{Code: errWriteConflict, Msg: "root_overview_receipt_invalid: " + rootOverviewReceiptName}
	}
	rootPath := filepath.Join(root, filepath.FromSlash(receipt.RootPath))
	current, err := os.ReadFile(rootPath)
	if err != nil {
		return &Fail{Code: errWriteConflict, Msg: "root_overview_root_read_failed"}
	}
	switch indexTextHash(string(current)) {
	case receipt.PostRootSHA256:
	case receipt.PreRootSHA256:
		if err := rootOverviewWriteCAS(rootPath, []byte(receipt.RootPostimage), receipt.PreRootSHA256); err != nil {
			return &Fail{Code: errWriteConflict, Msg: "root_overview_root_write_failed"}
		}
	default:
		return &Fail{Code: errWriteConflict, Msg: "root_overview_root_third_party_conflict"}
	}
	if receipt.BaselinePostimage != "" {
		baselinePath := filepath.Join(root, ".aoci", "baseline.json")
		baselineRaw, err := os.ReadFile(baselinePath)
		if err != nil {
			return &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_read_failed"}
		}
		switch indexTextHash(string(baselineRaw)) {
		case receipt.BaselinePostSHA256:
		case receipt.BaselinePreSHA256:
			if err := rootOverviewWriteCAS(baselinePath, []byte(receipt.BaselinePostimage), receipt.BaselinePreSHA256); err != nil {
				return &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_write_failed"}
			}
		default:
			return &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_third_party_conflict"}
		}
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil || loaded.set.Root.SHA256 != receipt.PostRootSHA256 {
		return &Fail{Code: errInternal, Msg: "root_overview_postimage_unconfirmed"}
	}
	event := ledger.Event{Op: "root_overview_apply", Source: source, Result: ledger.ResultOK, AppliedCount: 1,
		RecoveryTransactionID: receipt.LedgerTransactionID, PreIndexSHA256: receipt.PreRootSHA256,
		PostIndexSHA256: receipt.PostRootSHA256, IndexSHA256: receipt.PostRootSHA256, BaselineSHA256: receipt.BaselinePostSHA256}
	if loaded.cfg.LedgerEnabled {
		if err := cognitiontxn.EnsureLedger(root, true, event); err != nil {
			return &Fail{Code: errInternal, Msg: "root_overview_ledger_completion_failed"}
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "root_overview_receipt_cleanup_failed"}
	}
	return nil
}

// handleRootOverviewUpdate answers aoci_update_entry root_overview: the write,
// then the same terminal_proof a final Apply carries, since the Root's bytes
// moved and the Baseline record with them.
func handleRootOverviewUpdate(root, mcpServiceVersion string, in rootOverviewIn, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	start := time.Now()
	written, fail := applyRootOverview(root, ledger.SourceAgent, in)
	metrics := autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1}
	if fail != nil {
		status := autoStatusStopped
		if fail.Repairable {
			status = autoStatusRepairRequired
		}
		appendAutoFinalizeEvent(root, status, metrics)
		return textResult(renderAutoResult(autoResult{
			Version: 1, Status: status, Attempted: 1, FormalWritesStarted: fail.FormalWritesStarted,
			Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion), Metrics: metrics,
			Findings:   machineFindings{genericMachineFinding(fail.Code, "["+fail.Code+"] "+fail.Msg)},
			RetryScope: []string{}, NextAction: fail.Msg,
		}))
	}
	appendAutoFinalizeEvent(root, autoStatusApplied, metrics)
	proof, _ := BuildTerminalProof(root)
	applied := 1
	if written.AlreadyApplied {
		applied = 0
	}
	result := autoResult{
		Version: 1, Status: autoStatusApplied, Aligned: proof.Aligned, Attempted: 1, Applied: applied,
		FormalWritesStarted: !written.AlreadyApplied, Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion),
		Metrics: metrics, Findings: machineFindings{}, RetryScope: []string{}, NextAction: "none",
		TerminalProof: proof, RootOverviewResult: written,
	}
	applyAutoRefreshOutcome(&result, refreshSession)
	return textResult(renderAutoResult(result))
}
