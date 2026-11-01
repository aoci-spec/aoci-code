package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// aoci_maintain intent=observe_review records the Host's review of the
// Observe evidence a blocked Maintain listed (#151).
//
// The CLI route stays (scope status, then scope acknowledge); this one keeps a
// Host on MCP. The confirmation names the review_digest Maintain published and
// the list is recomputed, so bytes that moved after the listing void the
// confirmation instead of being acknowledged unread. It runs the transaction
// scope acknowledge runs, with the same Baseline, Ledger, CAS, Recovery, and
// authorization posture; a posture that needs a person saves the preview and
// hands the caller the human_approval request instead.

var observeReviewDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// observeReviewListLimit is the longest list Maintain shows in full, and so the
// longest a Host may confirm here. The Scope transaction acknowledges only the
// complete pending set and a Host cannot review what it was not shown, so a
// longer list has no confirmation on MCP: it goes through scope status and
// scope acknowledge, which the blocked Maintain's next_commands carry. A
// variable so a test can shorten it.
var observeReviewListLimit = machinecontract.MaintainTransportListLimit

type observeReviewRequest struct {
	ReviewDigest string `json:"review_digest"`
	ReviewedBy   string `json:"reviewed_by"`
}

// toolCallRequest is an MCP tool call a host can make as written.
type toolCallRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

// observeReviewListing is what a Maintain blocked by observed_pending
// publishes: the pending evidence, the complete count, the digest binding the
// complete list, and the call that confirms it. A list longer than the
// listing shows only its leading items and carries no confirmation.
type observeReviewListing struct {
	ReviewDigest string                              `json:"review_digest"`
	Total        int                                 `json:"total"`
	Evidence     []volumegovernance.ObservedEvidence `json:"evidence"`
	Truncated    bool                                `json:"truncated,omitempty"`
	Confirm      *toolCallRequest                    `json:"confirm_request,omitempty"`
}

type observeReviewResult struct {
	Version             int                   `json:"version"`
	Status              string                `json:"status"`
	FormalWritesStarted bool                  `json:"formal_writes_started"`
	ReviewDigest        string                `json:"review_digest,omitempty"`
	Acknowledged        int                   `json:"acknowledged,omitempty"`
	AcknowledgedPaths   []string              `json:"acknowledged_paths,omitempty"`
	TransactionID       string                `json:"transaction_id,omitempty"`
	ReviewedBy          string                `json:"reviewed_by,omitempty"`
	ObserveReview       *observeReviewListing `json:"observe_review,omitempty"`
	PreviewFile         string                `json:"preview_file,omitempty"`
	HostDialogRequest   *toolCallRequest      `json:"host_dialog_request,omitempty"`
	Message             string                `json:"message"`
	NextAction          string                `json:"next_action"`
	NextCommands        []string              `json:"next_commands,omitempty"`
	// TerminalProof closes a test-only change: when the acknowledgement
	// leaves the repository aligned there is nothing for Maintain to issue,
	// so the review result carries the same proof a final Apply does.
	TerminalProof *TerminalProof `json:"terminal_proof,omitempty"`
}

// observeReviewFor is the listing a blocked Maintain carries, or nil when no
// Observe evidence waits for review.
func observeReviewFor(facts *volumegovernance.Facts) *observeReviewListing {
	if facts == nil || facts.ObservedReviewDigest == "" {
		return nil
	}
	listing := &observeReviewListing{ReviewDigest: facts.ObservedReviewDigest, Total: len(facts.ObservedEvidence),
		Evidence: facts.ObservedEvidence}
	if len(listing.Evidence) > observeReviewListLimit {
		listing.Evidence = append([]volumegovernance.ObservedEvidence{}, listing.Evidence[:observeReviewListLimit]...)
		listing.Truncated = true
		return listing
	}
	listing.Confirm = &toolCallRequest{Tool: "aoci_maintain", Arguments: map[string]any{
		"intent": machinecontract.MaintainIntentObserveReview,
		"observe_review": map[string]any{
			"review_digest": facts.ObservedReviewDigest, "reviewed_by": "{agent}",
		},
	}}
	return listing
}

func validateObserveReviewRequest(input maintainIn) *Fail {
	request := input.ObserveReview
	switch {
	case request == nil:
		return &Fail{Code: errBadArgs, Msg: "observe_review_request_required"}
	case input.Scope != "" || len(input.ObjectRefs) != 0 || input.ApprovalRequest != nil:
		return &Fail{Code: errBadArgs, Msg: "observe_review_takes_only_observe_review"}
	case !observeReviewDigestPattern.MatchString(request.ReviewDigest):
		return &Fail{Code: errBadArgs, Msg: "observe_review_digest_invalid"}
	case !cognitiontxn.ValidAuditActor(request.ReviewedBy):
		return &Fail{Code: errBadArgs, Msg: "observe_review_reviewer_invalid"}
	}
	return nil
}

func handleObserveReview(root string, input maintainIn) *mcp.CallToolResult {
	start := time.Now()
	if fail := validateObserveReviewRequest(input); fail != nil {
		return failResult(fail)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return failResult(fail)
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
		return failResult(legacyReadOnlyFail())
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_governance_facts_invalid"})
	}
	if facts.ManagedScope.ScopeChangeRequired {
		return failResult(&Fail{Code: errBadArgs, Msg: "observe_review_requires_active_policy",
			Hint: mcpMessage("mcp.observe_review.hint.activate_first")})
	}
	if len(facts.ObservedEvidence) > observeReviewListLimit {
		return failResult(&Fail{Code: errBadArgs, Msg: "observe_review_list_exceeds_mcp_listing",
			Hint: mcpMessage("mcp.observe_review.hint.use_terminal", len(facts.ObservedEvidence), observeReviewListLimit)})
	}
	request := input.ObserveReview
	result := &observeReviewResult{Version: 1, ReviewedBy: request.ReviewedBy}
	switch {
	case facts.ObservedReviewDigest == "":
		result.Status, result.Message = machinecontract.ObserveReviewStatusNotPending, mcpMessage("mcp.observe_review.not_pending")
		result.NextAction = machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch
	case facts.ObservedReviewDigest != request.ReviewDigest:
		result.Status, result.Message = machinecontract.ObserveReviewStatusStale, mcpMessage("mcp.observe_review.stale")
		result.NextAction, result.ObserveReview = machinecontract.ObserveReviewActionReviewCurrent, observeReviewFor(facts)
	default:
		if fail := acknowledgeObservedEvidence(root, loaded, facts, request, result); fail != nil {
			return failResult(fail)
		}
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "observe_review", Source: ledger.SourceAgent,
		Result: ledger.ResultOK, PathsCount: len(facts.ObservedEvidence), DurationMs: time.Since(start).Milliseconds(),
		GovernanceResult: result.Status, NextAction: result.NextAction, FormalWritesStarted: ledgerFlag(result.FormalWritesStarted)})
	data, err := json.Marshal(result)
	if err != nil {
		return errResult(errInternal, "observe_review_result_invalid", "")
	}
	return textResult(string(data) + "\n")
}

// acknowledgeObservedEvidence runs the scope acknowledge transaction over the
// reviewed list. The preview is checked against the reviewed bytes before
// anything is written: a file rewritten between the digest check and the
// preview would otherwise be acknowledged unread, so that answer is stale too
// and lists what is there now.
func acknowledgeObservedEvidence(root string, loaded *cognitionRepoCtx, facts *volumegovernance.Facts,
	request *observeReviewRequest, result *observeReviewResult) *Fail {
	paths := make([]string, 0, len(facts.ObservedEvidence))
	for _, item := range facts.ObservedEvidence {
		paths = append(paths, item.Path)
	}
	preview, err := scopechange.Build(root, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		observeReviewCandidates(facts.ObservedEvidence, request.ReviewedBy))
	if err != nil {
		return &Fail{Code: errBadArgs, Msg: localeSafeMCPDetail(err.Error())}
	}
	if !previewAcknowledgesReviewedBytes(preview, facts.ObservedEvidence) {
		result.Status, result.Message = machinecontract.ObserveReviewStatusStale, mcpMessage("mcp.observe_review.stale")
		result.NextAction = machinecontract.ObserveReviewActionReviewCurrent
		if current, err := volumegovernance.Assess(root, loaded.cfg, loaded.set); err == nil {
			result.ObserveReview = observeReviewFor(current)
		}
		return nil
	}
	if preview.Plan.InteractionRequired {
		previewPath, previewRel, saveErr := saveObserveReviewPreview(root, preview)
		if saveErr != nil {
			return &Fail{Code: errInternal, Msg: "observe_review_preview_save_failed"}
		}
		approvalPath := filepath.Join(filepath.Dir(previewPath), "approval.json")
		result.Status, result.PreviewFile = machinecontract.ObserveReviewStatusHumanApprovalRequired, previewRel
		result.ReviewDigest = request.ReviewDigest
		result.Message = mcpMessage("mcp.observe_review.human_approval_required", previewRel)
		result.NextAction = machinecontract.ObserveReviewActionHumanApproval
		if loaded.cfg.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation) && scopechange.HostDialogEligible(preview) {
			result.HostDialogRequest = &toolCallRequest{Tool: "aoci_maintain", Arguments: map[string]any{
				"intent": machinecontract.MaintainIntentHumanApproval,
				"approval_request": map[string]any{
					"kind": machinecontract.HumanApprovalKindScopeChange, "preview_file": previewRel,
				},
			}}
		}
		result.NextCommands = humanApprovalCommands(
			[]string{"--repo", root, "scope", "approve", "--preview-file", previewPath, "--actor", "<id>", "--out-file", approvalPath},
			[]string{"--repo", root, "scope", "apply", "--preview-file", previewPath, "--approval-file", approvalPath})
		return nil
	}
	applied, err := scopechange.Apply(root, preview, nil)
	if err != nil {
		return &Fail{Code: errBadArgs, Msg: localeSafeMCPDetail(err.Error())}
	}
	result.Status, result.FormalWritesStarted = machinecontract.ObserveReviewStatusAcknowledged, true
	result.ReviewDigest, result.Acknowledged, result.AcknowledgedPaths = request.ReviewDigest, len(paths), paths
	result.TransactionID = applied.TransactionID
	result.Message = mcpMessage("mcp.observe_review.acknowledged", len(paths))
	result.NextAction = machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch
	if proof, _ := BuildTerminalProof(root); proof != nil && proof.Aligned {
		result.TerminalProof = proof
		result.Message = mcpMessage("mcp.observe_review.acknowledged_aligned", len(paths))
		result.NextAction = "none"
	}
	return nil
}

// observeReviewCandidates is the Scope candidate set that acknowledges the
// listed evidence: no Entry, no disposition, the complete path list reviewed.
func observeReviewCandidates(evidence []volumegovernance.ObservedEvidence, reviewer string) scopechange.CandidateSet {
	paths := make([]string, 0, len(evidence))
	for _, item := range evidence {
		paths = append(paths, item.Path)
	}
	return scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: paths, ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: reviewer}}
}

// observeReviewRidesWithBatch reports whether the pending Observe review may
// be confirmed by the model inside its update batch (2026-11-01): the
// listing fits the transport, no Scope Change is pending, and the
// authorization posture lets the model acknowledge on its own. A posture
// that needs a person keeps the review a stop, and the person answers in the
// dialog or the terminal as before.
func observeReviewRidesWithBatch(root string, facts *volumegovernance.Facts) bool {
	if facts == nil || facts.ObservedReviewDigest == "" || facts.ManagedScope.ScopeChangeRequired ||
		len(facts.ObservedEvidence) > observeReviewListLimit {
		return false
	}
	preview, err := scopechange.Build(root, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		observeReviewCandidates(facts.ObservedEvidence, "host_model"))
	return err == nil && !preview.Plan.InteractionRequired
}

// observeReviewForBatch is the listing a Maintain carries when the review
// rides with the batch: the same evidence and digest, confirmed through the
// batch's own observe_review field instead of a second call.
func observeReviewForBatch(facts *volumegovernance.Facts) *observeReviewListing {
	listing := observeReviewFor(facts)
	if listing == nil || listing.Confirm == nil {
		return listing
	}
	listing.Confirm = &toolCallRequest{Tool: "aoci_update_entry", Arguments: map[string]any{
		"observe_review": map[string]any{"review_digest": facts.ObservedReviewDigest, "reviewed_by": "{agent}"},
	}}
	return listing
}

// batchObserveReview is what an update batch reports about the Observe
// review it confirmed on the way in.
type batchObserveReview struct {
	Status            string   `json:"status"`
	ReviewDigest      string   `json:"review_digest"`
	ReviewedBy        string   `json:"reviewed_by,omitempty"`
	Acknowledged      int      `json:"acknowledged,omitempty"`
	AcknowledgedPaths []string `json:"acknowledged_paths,omitempty"`
	TransactionID     string   `json:"transaction_id,omitempty"`
}

// confirmObserveReviewForBatch runs, before any Entry of the batch is
// planned, the review the batch carries: the digest is checked against the
// current listing and the acknowledge transaction runs under the same
// posture rules as the intent. It answers the outcome to report beside the
// batch, or the result that ends the call: a stale digest is a zero-write
// repair finding with the current listing, and a posture that needs a person
// stops with the approval routes. Nothing pending is a no-op, not a fault.
func confirmObserveReviewForBatch(root, mcpServiceVersion string, request *observeReviewRequest) (*batchObserveReview, *mcp.CallToolResult) {
	if fail := validateObserveReviewRequest(maintainIn{ObserveReview: request}); fail != nil {
		return nil, failResult(fail)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, failResult(fail)
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, failResult(legacyReadOnlyFail())
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return nil, failResult(&Fail{Code: errInternal, Msg: "volumes_governance_facts_invalid"})
	}
	if facts.ManagedScope.ScopeChangeRequired {
		return nil, failResult(&Fail{Code: errBadArgs, Msg: "observe_review_requires_active_policy",
			Hint: mcpMessage("mcp.observe_review.hint.activate_first")})
	}
	if facts.ObservedReviewDigest == "" {
		return &batchObserveReview{Status: machinecontract.ObserveReviewStatusNotPending, ReviewDigest: request.ReviewDigest, ReviewedBy: request.ReviewedBy}, nil
	}
	stale := func(current *volumegovernance.Facts) *mcp.CallToolResult {
		finding := LocalizeRepairFindings([]cognition.RepairFinding{{Field: "observe_review.review_digest",
			RuleCode: "observe_review_stale", Code: "observe_review_stale", Expected: current.ObservedReviewDigest,
			Actual: request.ReviewDigest, Cause: mcpMessage("mcp.observe_review.stale")}})
		return textResult(renderAutoResult(autoResult{Version: 1, Status: autoStatusRepairRequired, Attempted: 0, Applied: 0,
			Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion), Findings: machineFindings(finding),
			RetryScope: []string{"observe_review"}, NextAction: machinecontract.ObserveReviewActionReviewCurrent,
			ObserveReviewCurrent: observeReviewForBatch(current)}))
	}
	if facts.ObservedReviewDigest != request.ReviewDigest {
		return nil, stale(facts)
	}
	if len(facts.ObservedEvidence) > observeReviewListLimit {
		return nil, failResult(&Fail{Code: errBadArgs, Msg: "observe_review_list_exceeds_mcp_listing",
			Hint: mcpMessage("mcp.observe_review.hint.use_terminal", len(facts.ObservedEvidence), observeReviewListLimit)})
	}
	outcome := &observeReviewResult{Version: 1, ReviewedBy: request.ReviewedBy}
	if fail := acknowledgeObservedEvidence(root, loaded, facts, request, outcome); fail != nil {
		return nil, failResult(fail)
	}
	switch outcome.Status {
	case machinecontract.ObserveReviewStatusAcknowledged:
		return &batchObserveReview{Status: outcome.Status, ReviewDigest: outcome.ReviewDigest, ReviewedBy: request.ReviewedBy,
			Acknowledged: outcome.Acknowledged, AcknowledgedPaths: outcome.AcknowledgedPaths, TransactionID: outcome.TransactionID}, nil
	case machinecontract.ObserveReviewStatusStale:
		current, assessErr := volumegovernance.Assess(root, loaded.cfg, loaded.set)
		if assessErr != nil {
			current = facts
		}
		return nil, stale(current)
	default:
		// A person has to approve the acknowledgement: nothing was written, and
		// the batch waits for the same routes the intent offers.
		return nil, textResult(renderAutoResult(autoResult{Version: 1, Status: autoStatusStopped,
			Receipt:    currentWriteCognitionReceipt(root, mcpServiceVersion),
			Findings:   genericMachineFindings([]string{"observe_review_" + outcome.Status}),
			NextAction: outcome.NextAction, NextCommands: outcome.NextCommands, HostDialogRequest: outcome.HostDialogRequest}))
	}
}

func previewAcknowledgesReviewedBytes(preview *scopechange.Preview, evidence []volumegovernance.ObservedEvidence) bool {
	for _, item := range evidence {
		fingerprint, present := preview.Baseline.Files[item.Path]
		if item.Change == volumegovernance.ObservedChangeRemoved {
			if present {
				return false
			}
			continue
		}
		if !present || fingerprint.SHA256 != item.SHA256 {
			return false
		}
	}
	return true
}

// saveObserveReviewPreview keeps a preview that needs a person where a paused
// activation keeps its own, in a fresh directory that never replaces one
// under review.
func saveObserveReviewPreview(root string, preview *scopechange.Preview) (string, string, error) {
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/scope-change"); err != nil {
		return "", "", err
	}
	directory, err := os.MkdirTemp(filepath.Join(root, ".aoci", "scope-change"), "observe-")
	if err != nil {
		return "", "", err
	}
	encoded, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(directory, "preview.json")
	if err := afs.AtomicCreateCASMode(path, append(encoded, '\n'), humanApprovalArtifactMode); err != nil {
		return "", "", err
	}
	return path, ".aoci/scope-change/" + filepath.Base(directory) + "/preview.json", nil
}
