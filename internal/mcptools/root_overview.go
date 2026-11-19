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
	// rootOverviewRootPath is the Root of a Volumes v1 repository, the one
	// file this transaction rewrites; a receipt cannot name another.
	rootOverviewRootPath = "aoci.txt"
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
// one of them reads as unauthored. The first line with a prefix is the field,
// for the reader and the writer alike; a later line with the same prefix is
// text nobody authored through this path and stays as it is.
func rootOverviewFields(raw []byte) (project, invariants string) {
	project, invariants = rootOverviewUnauthored, rootOverviewUnauthored
	seenProject, seenInvariants := false, false
	for _, line := range strings.SplitAfter(string(raw), "\n") {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case !seenProject && strings.HasPrefix(line, rootProjectPrefix):
			project, seenProject = strings.TrimSpace(strings.TrimPrefix(line, rootProjectPrefix)), true
		case !seenInvariants && strings.HasPrefix(line, rootInvariantsPrefix):
			invariants, seenInvariants = strings.TrimSpace(strings.TrimPrefix(line, rootInvariantsPrefix)), true
		}
	}
	return project, invariants
}

func rootOverviewAuthored(value string) bool {
	return value != "" && value != rootOverviewUnauthored && value != "MODEL_AUTHORING_REQUIRED"
}

// rewriteRootOverview replaces the two lines and keeps every other byte: each
// line keeps its own ending, so a Root of mixed endings stays mixed. A Root
// that lacks a line gets it after the line that precedes it in the template
// (Locale, then Project), with the ending of that line.
func rewriteRootOverview(raw []byte, project, invariants string) []byte {
	lines := strings.SplitAfter(string(raw), "\n")
	ending := func(line string) string {
		switch {
		case strings.HasSuffix(line, "\r\n"):
			return "\r\n"
		case strings.HasSuffix(line, "\n"):
			return "\n"
		}
		return ""
	}
	replace := func(prefix, value, after string) {
		for index, line := range lines {
			if strings.HasPrefix(line, prefix) {
				lines[index] = prefix + " " + value + ending(line)
				return
			}
		}
		at := -1
		for index, line := range lines {
			if strings.HasPrefix(line, after) {
				at = index
				break
			}
		}
		if at < 0 {
			at = 0
		}
		separator := ending(lines[at])
		if separator == "" {
			// The anchor is the file's unterminated last line: terminate it
			// with the file's own ending and leave the new line last.
			separator = "\n"
			if strings.Contains(string(raw), "\r\n") {
				separator = "\r\n"
			}
			lines[at] += separator
			lines = append(lines[:at+1], append([]string{prefix + " " + value}, lines[at+1:]...)...)
			return
		}
		lines = append(lines[:at+1], append([]string{prefix + " " + value + separator}, lines[at+1:]...)...)
	}
	replace(rootProjectPrefix, project, "#Locale:")
	replace(rootInvariantsPrefix, invariants, rootProjectPrefix)
	return []byte(strings.Join(lines, ""))
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
	// A Root whose bytes are not the ones the Baseline records (a line-ending
	// rewrite reads as aligned and is reported as informational) could not be
	// written: no invitation is made that the write would refuse.
	if loaded.bl != nil {
		if recorded, bound := loaded.bl.Files[rootOverviewRootPath]; bound && recorded.SHA256 != loaded.set.Root.SHA256 {
			return nil
		}
	}
	// The current values are context, not payload: a Root someone filled by
	// hand with a very long line must not swell every aligned response.
	shown := func(value string, limit int) string {
		if utf8.RuneCountInString(value) <= limit {
			return value
		}
		return string([]rune(value)[:limit])
	}
	project = shown(project, machinecontract.RootOverviewProjectMaxChars)
	invariants = shown(invariants, machinecontract.RootOverviewInvariantsMaxChars)
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
// recovered reports that this call first finished an earlier overview write
// that had been cut short, whatever became of its own request.
func applyRootOverview(root, source string, in rootOverviewIn) (written *rootOverviewResult, recovered rootOverviewProgress, fail *Fail) {
	if fail := rootOverviewDirectoriesSafe(root); fail != nil {
		return nil, recovered, fail
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_lock_failed"}
	}
	defer lock.Release()
	if recovered, fail = completeRootOverviewLocked(root, source); fail != nil {
		return nil, recovered, fail
	}
	if other, pendingErr := cognitiontxn.OtherPending(root, rootOverviewReceiptName); pendingErr != nil {
		return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_recovery_read_failed"}
	} else if other != "" {
		return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_other_transaction_pending: " + other}
	}
	loaded, loadFail := loadCognitionCtx(root)
	if loadFail != nil {
		return nil, recovered, loadFail
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 || loaded.cfg.IndexPath != rootOverviewRootPath {
		return nil, recovered, &Fail{Code: errBadArgs, Msg: machinecontract.StatusLegacyReadOnlyMigrateRequired}
	}
	project, invariants := strings.TrimSpace(in.Project), strings.TrimSpace(in.GlobalInvariants)
	for _, finding := range []string{
		rootOverviewValueFinding("project", in.Project, machinecontract.RootOverviewProjectMaxChars),
		rootOverviewValueFinding("global_invariants", in.GlobalInvariants, machinecontract.RootOverviewInvariantsMaxChars),
	} {
		if finding != "" {
			return nil, recovered, &Fail{Code: errCandidateInvalid, Msg: finding, Repairable: true}
		}
	}
	// Aligned comes first, the repeated submission included: an overview is
	// never answered as applied over an index that is not aligned.
	facts, assessErr := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if assessErr != nil || facts == nil || !facts.GovernanceAligned {
		return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_requires_aligned_index"}
	}
	rootRaw, rootSHA := loaded.set.Root.Raw, loaded.set.Root.SHA256
	post := rewriteRootOverview(rootRaw, project, invariants)
	if string(post) == string(rootRaw) {
		// The same overview submitted again, or a receipt just rolled forward.
		return &rootOverviewResult{Status: autoStatusApplied, RootSHA256: rootSHA, AlreadyApplied: true,
			BaselineBound: loaded.bl != nil && loaded.bl.Files[rootOverviewRootPath].SHA256 == rootSHA}, recovered, nil
	}
	if in.RootSHA256 != rootSHA {
		return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_root_changed: current root_sha256=" + rootSHA, Repairable: true}
	}
	receipt := rootOverviewReceipt{Version: 1, RootPath: rootOverviewRootPath, PreRootSHA256: rootSHA,
		PostRootSHA256: indexTextHash(string(post)), RootPostimage: string(post)}
	// The Baseline joins only when it records the Root; several legal layouts
	// never bind it, and an absent record is not enrolled here. Its postimage
	// is decoded from the very bytes whose digest the CAS will demand, so a
	// version another writer put there in between is refused, never overwritten.
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineRaw, readErr := os.ReadFile(baselinePath)
	if readErr == nil {
		state, decodeErr := baseline.Decode(baselineRaw)
		if decodeErr != nil {
			return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_invalid"}
		}
		if recorded, bound := state.Files[rootOverviewRootPath]; bound {
			if recorded.SHA256 != rootSHA {
				return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_root_not_at_baseline"}
			}
			fingerprint := baseline.HashBytes(rootOverviewRootPath, post)
			fingerprint.Role = recorded.Role
			baseline.UpdateOne(state, rootOverviewRootPath, fingerprint)
			state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			baselinePost, marshalErr := baseline.MarshalExact(state)
			if marshalErr != nil {
				return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_baseline_postimage_invalid"}
			}
			receipt.BaselinePreSHA256, receipt.BaselinePostSHA256 = indexTextHash(string(baselineRaw)), indexTextHash(string(baselinePost))
			receipt.BaselinePostimage = string(baselinePost)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, recovered, &Fail{Code: errWriteConflict, Msg: "root_overview_baseline_read_failed"}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_transaction_id_unavailable"}
	}
	receipt.LedgerTransactionID = hex.EncodeToString(nonce[:])
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_receipt_invalid"}
	}
	path := rootOverviewReceiptPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_receipt_save_failed"}
	}
	if err := afs.AtomicWrite(path, append(data, '\n')); err != nil {
		return nil, recovered, &Fail{Code: errInternal, Msg: "root_overview_receipt_save_failed"}
	}
	own, fail := completeRootOverviewLocked(root, source)
	recovered.wrote = recovered.wrote || own.wrote
	if fail != nil {
		return nil, recovered, fail
	}
	return &rootOverviewResult{Status: autoStatusApplied, RootSHA256: receipt.PostRootSHA256, BaselineBound: receipt.BaselinePostimage != ""}, recovered, nil
}

// rootOverviewProgress is what one roll-forward did: whether it changed a
// formal asset, and whether it closed a receipt an earlier call had left. A
// roll-forward that fails after its writes still reports them.
type rootOverviewProgress struct {
	wrote    bool
	finished bool
}

// rootOverviewDirectoriesSafe refuses a .aoci or .aoci/transactions that is
// not a real directory: the receipt and the Baseline are read and written
// through them, and a link would point both somewhere this code never chose.
func rootOverviewDirectoriesSafe(root string) *Fail {
	for _, relative := range []string{".aoci", filepath.Join(".aoci", "transactions")} {
		info, err := os.Lstat(filepath.Join(root, relative))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return &Fail{Code: errPathUnsafe, Msg: "root_overview_runtime_directory_unsafe"}
		}
	}
	return nil
}

// completeRootOverviewRecovery rolls a pending Root overview receipt forward.
// aoci_maintain and aoci_update_entry call it first, so a write cut short
// heals on the next call instead of standing in the way as a pending receipt.
// It reports whether it finished one.
func completeRootOverviewRecovery(root, source string) (rootOverviewProgress, *Fail) {
	// A repository with no Root overview receipt is none of this code's
	// business: its directories are judged only when there is one to finish.
	if _, err := os.Stat(rootOverviewReceiptPath(root)); errors.Is(err, os.ErrNotExist) {
		return rootOverviewProgress{}, nil
	}
	if fail := rootOverviewDirectoriesSafe(root); fail != nil {
		return rootOverviewProgress{}, fail
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return rootOverviewProgress{}, &Fail{Code: errWriteConflict, Msg: "root_overview_lock_failed"}
	}
	defer lock.Release()
	return completeRootOverviewLocked(root, source)
}

// rootOverviewWriteCAS is the CAS replace, a variable so a test can cut the
// transaction between its two writes.
var rootOverviewWriteCAS = afs.AtomicWriteCAS

// completeRootOverviewLocked advances the receipt, if one is pending, and
// reports whether it finished one. Nothing is written until everything that
// could refuse has been checked: no other receipt may be pending, an exchange
// a crash cut inside either file is settled first, and both files must then
// hold their preimage or their postimage. Any other bytes are a third party's
// and stop it with both files and the receipt as they were.
func completeRootOverviewLocked(root, source string) (progress rootOverviewProgress, fail *Fail) {
	path := rootOverviewReceiptPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return progress, nil
	}
	if err != nil {
		return progress, &Fail{Code: errInternal, Msg: "root_overview_receipt_read_failed"}
	}
	var receipt rootOverviewReceipt
	// The Root of a Volumes v1 repository is aoci.txt at its root and nothing
	// else; a receipt naming another path is not one this code wrote.
	if jsonstrict.RejectDuplicateKeysFold(data) != nil || json.Unmarshal(data, &receipt) != nil || receipt.Version != 1 ||
		receipt.RootPath != rootOverviewRootPath || !validRecoverySHA256(receipt.PreRootSHA256) ||
		indexTextHash(receipt.RootPostimage) != receipt.PostRootSHA256 ||
		(receipt.BaselinePostimage != "" && (indexTextHash(receipt.BaselinePostimage) != receipt.BaselinePostSHA256 || !validRecoverySHA256(receipt.BaselinePreSHA256))) {
		return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_receipt_invalid: " + rootOverviewReceiptName}
	}
	if other, pendingErr := cognitiontxn.OtherPending(root, rootOverviewReceiptName); pendingErr != nil {
		return progress, &Fail{Code: errInternal, Msg: "root_overview_recovery_read_failed"}
	} else if other != "" {
		return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_other_transaction_pending: " + other}
	}
	rootPath := filepath.Join(root, rootOverviewRootPath)
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	type participant struct {
		path, pre, post, image, name string
		atPreimage                   bool
	}
	participants := []*participant{{path: rootPath, pre: receipt.PreRootSHA256, post: receipt.PostRootSHA256, image: receipt.RootPostimage, name: "root"}}
	if receipt.BaselinePostimage != "" {
		participants = append(participants, &participant{path: baselinePath, pre: receipt.BaselinePreSHA256, post: receipt.BaselinePostSHA256, image: receipt.BaselinePostimage, name: "baseline"})
	}
	for _, item := range participants {
		if info, statErr := os.Lstat(item.path); statErr != nil || !info.Mode().IsRegular() {
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_read_failed"}
		}
		if err := afs.RecoverAtomicWriteCAS(item.path); err != nil {
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_exchange_unsettled"}
		}
		current, readErr := os.ReadFile(item.path)
		if readErr != nil {
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_read_failed"}
		}
		switch indexTextHash(string(current)) {
		case item.post:
		case item.pre:
			item.atPreimage = true
		default:
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_third_party_conflict"}
		}
	}
	for _, item := range participants {
		if !item.atPreimage {
			continue
		}
		if err := rootOverviewWriteCAS(item.path, []byte(item.image), item.pre); err != nil {
			// A CAS that failed after its exchange still changed the file.
			if current, readErr := os.ReadFile(item.path); readErr != nil || indexTextHash(string(current)) != item.pre {
				progress.wrote = true
			}
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_write_failed"}
		}
		progress.wrote = true
	}
	loaded, loadFail := loadCognitionCtx(root)
	if loadFail != nil || loaded.set.Root.SHA256 != receipt.PostRootSHA256 {
		return progress, &Fail{Code: errInternal, Msg: "root_overview_postimage_unconfirmed"}
	}
	// Both files are read once more, last of all, before the transaction is
	// declared done: a participant replaced while the other was written, or
	// while the repository was loaded, is a conflict and the receipt stays.
	// A writer that honours neither the index lock nor CAS can still act after
	// this read; what follows it writes only the Ledger and removes the receipt.
	for _, item := range participants {
		current, readErr := os.ReadFile(item.path)
		if readErr != nil || indexTextHash(string(current)) != item.post {
			return progress, &Fail{Code: errWriteConflict, Msg: "root_overview_" + item.name + "_postimage_unconfirmed"}
		}
	}
	event := ledger.Event{Op: "root_overview_apply", Source: source, Result: ledger.ResultOK, AppliedCount: 1,
		RecoveryTransactionID: receipt.LedgerTransactionID, PreIndexSHA256: receipt.PreRootSHA256,
		PostIndexSHA256: receipt.PostRootSHA256, IndexSHA256: receipt.PostRootSHA256, BaselineSHA256: receipt.BaselinePostSHA256}
	if loaded.cfg.LedgerEnabled {
		if err := cognitiontxn.EnsureLedger(root, true, event); err != nil {
			return progress, &Fail{Code: errInternal, Msg: "root_overview_ledger_completion_failed"}
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return progress, &Fail{Code: errInternal, Msg: "root_overview_receipt_cleanup_failed"}
	}
	progress.finished = true
	return progress, nil
}

// handleRootOverviewUpdate answers aoci_update_entry root_overview: the write,
// then the same terminal_proof a final Apply carries, since the Root's bytes
// moved and the Baseline record with them.
func handleRootOverviewUpdate(root, mcpServiceVersion string, in rootOverviewIn, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	start := time.Now()
	written, recovered, fail := applyRootOverview(root, ledger.SourceAgent, in)
	metrics := autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1}
	if fail != nil {
		status := autoStatusStopped
		if fail.Repairable {
			status = autoStatusRepairRequired
		}
		appendAutoFinalizeEvent(root, status, metrics)
		// A request refused after this call finished an earlier overview
		// write still changed the formal assets, and says so.
		return rootOverviewProgressFields(textResult(renderAutoResult(autoResult{
			Version: 1, Status: status, Attempted: 1, FormalWritesStarted: fail.FormalWritesStarted || recovered.wrote,
			Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion), Metrics: metrics,
			Findings:   machineFindings{genericMachineFinding(fail.Code, "["+fail.Code+"] "+fail.Msg)},
			RetryScope: []string{}, NextAction: fail.Msg,
		})), recovered)
	}
	appendAutoFinalizeEvent(root, autoStatusApplied, metrics)
	proof, _ := BuildTerminalProof(root)
	applied := 1
	if written.AlreadyApplied {
		applied = 0
	}
	result := autoResult{
		Version: 1, Status: autoStatusApplied, Aligned: proof.Aligned, Attempted: 1, Applied: applied,
		FormalWritesStarted: recovered.wrote, Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion),
		Metrics: metrics, Findings: machineFindings{}, RetryScope: []string{}, NextAction: "none",
		TerminalProof: proof, RootOverviewResult: written,
	}
	applyAutoRefreshOutcome(&result, refreshSession)
	return rootOverviewProgressFields(textResult(renderAutoResult(result)), recovered)
}

// withRootOverviewProgress marks a result of another request (an Entries
// batch, a Maintain) whose call first rolled a cut Root overview write
// forward: the formal assets changed in this call, whatever the request's own
// outcome, and the result says so. A call that rolled nothing forward is left
// as it is.
func withRootOverviewProgress(result *mcp.CallToolResult, progress rootOverviewProgress) *mcp.CallToolResult {
	if !progress.wrote && !progress.finished {
		return result
	}
	return rootOverviewProgressFields(result, progress)
}

// rootOverviewProgressFields states on a result what this call did to the
// Root overview transaction: root_overview_recovered (it closed a receipt an
// earlier call had left) and root_overview_formal_writes (it changed a formal
// asset), with formal_writes_started true whenever the latter is. The fields
// are appended, so the order a result leads with is untouched.
func rootOverviewProgressFields(result *mcp.CallToolResult, progress rootOverviewProgress) *mcp.CallToolResult {
	if result == nil || len(result.Content) != 1 {
		return result
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return result
	}
	flag := map[bool]string{true: "true", false: "false"}
	text := strings.TrimRight(content.Text, "\n")
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") && json.Valid([]byte(text)) {
		appended := `,"root_overview_recovered":` + flag[progress.finished] + `,"root_overview_formal_writes":` + flag[progress.wrote]
		if progress.wrote {
			if strings.Contains(text, `"formal_writes_started":`) {
				text = strings.Replace(text, `"formal_writes_started":false`, `"formal_writes_started":true`, 1)
			} else {
				appended += `,"formal_writes_started":true`
			}
		}
		content.Text = text[:len(text)-1] + appended + "}"
		return result
	}
	content.Text = text + "\nroot_overview_recovered: " + flag[progress.finished] + "\nroot_overview_formal_writes: " + flag[progress.wrote]
	return result
}
