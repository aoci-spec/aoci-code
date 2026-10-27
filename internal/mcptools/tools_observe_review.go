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
	candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: paths, ReviewStatus: scopechange.ReviewStatusReviewed,
			Reviewer: request.ReviewedBy}}
	preview, err := scopechange.Build(root, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), candidates)
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
	return nil
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
