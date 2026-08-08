package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

type cognitionRefreshSession struct {
	mu sync.Mutex

	established    bool
	generation     int
	lastReceipt    cognitionReceipt
	pendingReasons map[string]bool
	pendingEvents  map[string]bool
	consumedEvents map[string]bool
	// governanceSnapshots binds a delivered Whole-Index body to the live,
	// commit-independent governance facts observed for that delivery.
	governanceSnapshots map[string]string
}

func newCognitionRefreshSession() *cognitionRefreshSession {
	return &cognitionRefreshSession{
		pendingReasons:      map[string]bool{},
		pendingEvents:       map[string]bool{},
		consumedEvents:      map[string]bool{},
		governanceSnapshots: map[string]string{},
	}
}

func (session *cognitionRefreshSession) bindVolumeGovernanceSnapshot(
	key, identity string,
	input overviewIn,
) bool {
	if session.governanceSnapshots == nil {
		session.governanceSnapshots = map[string]string{}
	}
	newDelivery := input.Cursor == "" && input.HostConfirmation == nil && input.ModelAttestation == nil
	if newDelivery {
		if len(session.governanceSnapshots) >= 32 {
			session.governanceSnapshots = map[string]string{}
		}
		session.governanceSnapshots[key] = identity
		return true
	}
	expected, exists := session.governanceSnapshots[key]
	if !exists || expected == "" {
		return false
	}
	if expected != identity {
		// Once any continuation or Attestation observes a different snapshot,
		// the delivery stays tainted until a new body starts a new binding.
		session.governanceSnapshots[key] = ""
		return false
	}
	return true
}

type semanticChangeFacts struct {
	Count                  int    `json:"semantic_change_count"`
	Threshold              int    `json:"semantic_change_threshold"`
	PathSetSHA256          string `json:"semantic_change_paths_sha256"`
	SemanticStale          int    `json:"semantic_stale"`
	ActionableMissing      int    `json:"actionable_missing"`
	PendingCuration        int    `json:"pending_curation"`
	FormatOnly             int    `json:"format_only"`
	LineEndingOnly         int    `json:"line_ending_only"`
	CurationExcluded       int    `json:"curation_excluded"`
	TechnicalSkipped       int    `json:"technical_skipped"`
	Orphan                 int    `json:"orphan"`
	Unbaselined            int    `json:"unbaselined"`
	Warnings               int    `json:"warnings"`
	IndexSelfStale         bool   `json:"index_self_stale"`
	GovernanceBlockerCount int    `json:"governance_blocker_count"`
	GovernanceAligned      bool   `json:"governance_aligned"`
	ScopeChangeRequired    bool   `json:"scope_change_required"`
	ScopePolicyIdentity    string `json:"scope_policy_identity,omitempty"`
	ObservedNew            int    `json:"observed_new"`
	ObservedChanged        int    `json:"observed_changed"`
	ObservedRemoved        int    `json:"observed_removed"`
	ObservedPendingReview  int    `json:"observed_pending_review"`
	WholeIndexTokens       int    `json:"whole_index_tokens,omitempty"`
	BudgetMode             string `json:"budget_mode,omitempty"`
	BudgetStatus           string `json:"budget_status,omitempty"`
	BudgetViolationCount   int    `json:"budget_violation_count"`
}

type cognitionRefreshAssessment struct {
	Version          int                 `json:"version"`
	State            string              `json:"state"`
	Reason           string              `json:"reason"`
	Recall           string              `json:"recall"`
	RefreshStatus    string              `json:"refresh_status"`
	RefreshReasons   []string            `json:"refresh_reasons"`
	RefreshEventID   string              `json:"refresh_event_id,omitempty"`
	InitialCognition bool                `json:"initial_cognition"`
	StableCheckpoint bool                `json:"stable_checkpoint"`
	Semantic         semanticChangeFacts `json:"semantic"`
	Receipt          cognitionReceipt    `json:"cognition_receipt"`
	AOCIToolCalls    int                 `json:"aoci_tool_calls"`
	OverviewReads    int                 `json:"overview_reads"`
	LocalRecalls     int                 `json:"local_recalls"`
	NextAction       string              `json:"next_action"`
}

func inspectSemanticChanges(
	root string,
	repository *repoCtx,
) (semanticChangeFacts, *Fail) {
	facts := semanticChangeFacts{}
	if repository == nil || repository.cfg == nil {
		return facts, &Fail{Code: errInternal, Msg: mcpMessage("mcp.error.internal_recovered")}
	}
	facts.Threshold = repository.cfg.CognitionRefreshThreshold

	state, err := managedstate.Load(root, repository.cfg)
	if err != nil {
		return facts, &Fail{
			Code: errInternal,
			Msg:  mcpMessage("maintain.snapshot_failed", localeSafeMCPDetail(err.Error())),
		}
	}
	snapshot, warnings := state.Snapshot, state.Warnings
	detected := &baseline.DetectResult{}
	if !state.ScopeChangeRequired {
		detected, err = managedstate.Detect(root, repository.cfg, repository.doc, state)
		if err != nil {
			return facts, &Fail{Code: errInternal, Msg: mcpMessage("maintain.snapshot_failed", localeSafeMCPDetail(err.Error()))}
		}
	}
	classification, _, _, err := curation.BuildClassification(
		root,
		repository.cfg,
		detected.Missing,
	)
	if err != nil {
		return facts, &Fail{
			Code: errIndexInvalid,
			Msg:  mcpMessage("maintain.curation_invalid", localeSafeMCPDetail(err.Error())),
		}
	}

	formatOnlyPaths := formatOnlyCandidates(
		repository.bl,
		snapshot,
		detected.Stale,
		repository.cfg.LineEndingTolerance,
	)
	facts = buildSemanticChangeFacts(
		repository,
		detected,
		classification,
		formatOnlyPaths,
		warnings,
	)
	if !state.Legacy {
		facts.ScopeChangeRequired = state.ScopeChangeRequired
		facts.ScopePolicyIdentity = state.DesiredPolicyIdentity
		facts.ObservedNew, facts.ObservedChanged, facts.ObservedRemoved =
			len(detected.ObservedNew), len(detected.ObservedChanged), len(detected.ObservedRemoved)
		if repository.cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
			facts.ObservedPendingReview = facts.ObservedNew + facts.ObservedChanged + facts.ObservedRemoved
		}
		budget, budgetErr := cognitionbudget.Build(root, []byte(repository.text), repository.cfg.EffectiveCognitionBudget())
		if budgetErr != nil {
			return facts, &Fail{Code: errIndexInvalid, Msg: localeSafeMCPDetail(budgetErr.Error())}
		}
		facts.WholeIndexTokens, facts.BudgetMode, facts.BudgetStatus, facts.BudgetViolationCount =
			budget.WholeIndexTokens, budget.Mode, budget.Status, len(budget.Violations)
		facts.GovernanceBlockerCount += facts.ObservedPendingReview + facts.BudgetViolationCount
		if state.ScopeChangeRequired {
			facts.GovernanceBlockerCount++
		}
		facts.GovernanceAligned = facts.GovernanceBlockerCount == 0
	}
	return facts, nil
}

// buildSemanticChangeFacts is the Legacy counting authority shared by the
// compact Overview check and Maintain. Volumes adapts the shared live
// volumegovernance facts instead. Both count unique current objects, not edits.
func buildSemanticChangeFacts(
	repository *repoCtx,
	detected *baseline.DetectResult,
	classification curation.Classification,
	formatOnlyPaths []string,
	snapshotWarnings []string,
) semanticChangeFacts {
	facts := semanticChangeFacts{
		Threshold: repository.cfg.CognitionRefreshThreshold,
	}
	formatOnlySet := make(map[string]bool, len(formatOnlyPaths))
	for _, path := range formatOnlyPaths {
		formatOnlySet[path] = true
	}

	semanticPaths := map[string]bool{}
	for _, path := range detected.Stale {
		if path == repository.cfg.IndexPath {
			facts.IndexSelfStale = true
			continue
		}
		if formatOnlySet[path] {
			continue
		}
		semanticPaths[path] = true
		facts.SemanticStale++
	}
	for _, path := range classification.Actionable {
		semanticPaths[path] = true
	}
	for _, item := range classification.Pending {
		semanticPaths[item.Path] = true
	}

	pendingSet := make(map[string]bool, len(classification.Pending))
	for _, item := range classification.Pending {
		pendingSet[item.Path] = true
	}
	for _, item := range classification.Skipped {
		if !pendingSet[item.Path] {
			facts.TechnicalSkipped++
		}
	}

	paths := make([]string, 0, len(semanticPaths))
	for path := range semanticPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	digest := sha256.Sum256([]byte(strings.Join(paths, "\x00")))

	facts.Count = len(paths)
	facts.PathSetSHA256 = hex.EncodeToString(digest[:])
	facts.ActionableMissing = len(classification.Actionable)
	facts.PendingCuration = len(classification.Pending)
	facts.FormatOnly = len(formatOnlyPaths)
	facts.LineEndingOnly = len(detected.LineEndingOnly)
	facts.CurationExcluded = len(classification.CurationExcluded)
	facts.Orphan = len(detected.Orphan)
	facts.Unbaselined = len(detected.Unbaselined)
	facts.Warnings = len(snapshotWarnings) + len(detected.Warnings)
	facts.GovernanceBlockerCount = facts.Count + facts.Orphan + facts.Unbaselined + facts.Warnings
	if facts.IndexSelfStale {
		facts.GovernanceBlockerCount++
	}
	facts.GovernanceAligned = facts.GovernanceBlockerCount == 0
	return facts
}

func normalizeRefreshInput(
	input overviewIn,
) ([]string, string, *Fail) {
	eventID := strings.TrimSpace(input.RefreshEventID)
	if utf8.RuneCountInString(eventID) > 128 || strings.ContainsFunc(eventID, unicode.IsControl) {
		return nil, "", &Fail{Code: errBadArgs, Msg: mcpMessage("overview.refresh.event_id_invalid")}
	}

	allowed := map[string]bool{
		machinecontract.RefreshReasonContextCompaction: true,
		machinecontract.RefreshReasonPhaseTransition:   true,
	}
	seen := map[string]bool{}
	reasons := []string{}
	for _, raw := range input.RefreshReasons {
		reason := strings.TrimSpace(raw)
		if !allowed[reason] {
			return nil, "", &Fail{
				Code: errBadArgs,
				Msg:  mcpMessage("overview.refresh.reason_invalid", reason),
			}
		}
		if !seen[reason] {
			seen[reason] = true
			reasons = append(reasons, reason)
		}
	}

	// Legacy model_cognition_state=invalid represents compaction only when it
	// invalidates a previously reliable complete receipt. Initial establishment
	// and post-governance uncertain receipts must not fabricate a host event.
	if input.ModelState == cognitionStateInvalid && input.Receipt != nil &&
		(input.Receipt.ModelFullReliable || input.Receipt.ModelScopeReliable) &&
		!seen[machinecontract.RefreshReasonContextCompaction] {
		reasons = append(reasons, machinecontract.RefreshReasonContextCompaction)
		seen[machinecontract.RefreshReasonContextCompaction] = true
		if eventID == "" {
			eventID = legacyRefreshEventID(input.Receipt)
		}
	}
	if len(reasons) > 0 && eventID == "" {
		return nil, "", &Fail{Code: errBadArgs, Msg: mcpMessage("overview.refresh.event_id_required")}
	}
	return orderedRefreshReasons(reasons), eventID, nil
}

func legacyRefreshEventID(receipt *cognitionReceipt) string {
	data, _ := json.Marshal(receipt)
	digest := sha256.Sum256(append([]byte("legacy-host-model-invalid\x00"), data...))
	return "legacy-" + hex.EncodeToString(digest[:8])
}

func receiptIdentityMatches(left, right cognitionReceipt) bool {
	if left.Version != right.Version ||
		left.RuntimeRepositoryRoot != right.RuntimeRepositoryRoot ||
		left.MCPServiceVersion != right.MCPServiceVersion ||
		left.Scope != right.Scope {
		return false
	}
	if left.Version == 2 {
		return left.LayoutMode == right.LayoutMode &&
			left.RequestedScope == right.RequestedScope &&
			left.EffectiveScope == right.EffectiveScope &&
			left.ScopeIdentity == right.ScopeIdentity
	}
	return left.Version == right.Version &&
		left.RuntimeRepositoryRoot == right.RuntimeRepositoryRoot &&
		left.IndexSHA256 == right.IndexSHA256 &&
		left.MCPServiceVersion == right.MCPServiceVersion &&
		left.Scope == right.Scope
}

func (session *cognitionRefreshSession) adoptReliableReceipt(
	input *cognitionReceipt,
	current cognitionReceipt,
	modelState string,
) {
	if session.established || input == nil || modelState != cognitionStateValid ||
		!receiptIdentityMatches(*input, current) {
		return
	}
	session.established = true
	session.generation = input.RefreshGeneration
	// Preserve the v1 compatibility contract in which the Host's explicit
	// model_cognition_state=valid establishes an otherwise matching receipt.
	session.lastReceipt = receiptWithState(*input, cognitionStateValid, true)
	session.consumeEvent(input.LastRefreshEventID)
}

func receiptReliable(receipt cognitionReceipt) bool {
	if receipt.Version == 2 {
		return receipt.ModelScopeReliable
	}
	return receipt.ModelFullReliable
}

func (session *cognitionRefreshSession) pendingReasonList() []string {
	reasons := make([]string, 0, len(session.pendingReasons))
	for reason := range session.pendingReasons {
		reasons = append(reasons, reason)
	}
	return orderedRefreshReasons(reasons)
}

func (session *cognitionRefreshSession) addHostReasons(reasons []string, eventID string) {
	if len(reasons) == 0 || session.consumedEvents[eventID] {
		return
	}
	for _, reason := range reasons {
		session.pendingReasons[reason] = true
	}
	session.pendingEvents[eventID] = true
}

func (session *cognitionRefreshSession) consumeEvent(eventID string) {
	if eventID == "" {
		return
	}
	if len(session.consumedEvents) >= 256 {
		session.consumedEvents = map[string]bool{}
	}
	session.consumedEvents[eventID] = true
}

func (session *cognitionRefreshSession) evaluate(
	input overviewIn,
	current cognitionReceipt,
	facts semanticChangeFacts,
	hostReasons []string,
	eventID string,
) cognitionRefreshAssessment {
	session.adoptReliableReceipt(input.Receipt, current, input.ModelState)
	initial := !session.established
	hadPending := len(session.pendingReasons) > 0

	// A phase transition with no semantic change is acknowledged but does not
	// create a full-refresh obligation. When combined with another reason it is
	// retained as evidence for the single merged refresh.
	phaseOnlyClean := facts.Count == 0 && len(hostReasons) == 1 &&
		hostReasons[0] == machinecontract.RefreshReasonPhaseTransition
	if phaseOnlyClean && !hadPending {
		session.consumeEvent(eventID)
	} else {
		session.addHostReasons(hostReasons, eventID)
	}
	if facts.Count >= facts.Threshold {
		session.pendingReasons[machinecontract.RefreshReasonSemanticThreshold] = true
	}

	reasons := session.pendingReasonList()
	stable := input.StableCheckpoint != nil && *input.StableCheckpoint
	status := machinecontract.RefreshStatusNotRequired

	switch {
	case initial && !facts.GovernanceAligned:
		if facts.Count > 0 && !stable {
			status = machinecontract.RefreshStatusDeferredUntilStable
		} else {
			status = machinecontract.RefreshStatusRequired
		}
	case initial:
		status = machinecontract.RefreshStatusReadyForOverview
	case !receiptIdentityMatches(session.lastReceipt, current):
		// A different scope or changed Volume identity requires one delivery,
		// but does not invent a durable refresh reason or governance state.
		status = machinecontract.RefreshStatusReadyForOverview
	case len(reasons) == 0:
		status = machinecontract.RefreshStatusNotRequired
	case facts.Count > 0 && !stable:
		status = machinecontract.RefreshStatusDeferredUntilStable
	case !facts.GovernanceAligned:
		status = machinecontract.RefreshStatusRequired
	default:
		status = machinecontract.RefreshStatusReadyForOverview
	}

	identityReliable := session.established && receiptReliable(session.lastReceipt) &&
		receiptIdentityMatches(session.lastReceipt, current)
	state := cognitionStateInvalid
	recall := cognitionRecallNone
	reliable := false
	if status == machinecontract.RefreshStatusNotRequired {
		if identityReliable {
			state = cognitionStateValid
			reliable = true
		} else {
			state = cognitionStateUncertain
		}
	}
	if status == machinecontract.RefreshStatusReadyForOverview {
		recall = cognitionRecallFull
	}

	receipt := receiptWithRefresh(
		receiptWithState(current, state, reliable),
		session.generation,
		session.lastReceipt.LastRefreshEventID,
		reasons,
	)
	return cognitionRefreshAssessment{
		Version:          1,
		State:            state,
		Reason:           status,
		Recall:           recall,
		RefreshStatus:    status,
		RefreshReasons:   reasons,
		RefreshEventID:   eventID,
		InitialCognition: initial,
		StableCheckpoint: stable,
		Semantic:         facts,
		Receipt:          receipt,
		AOCIToolCalls:    1,
		OverviewReads:    0,
		LocalRecalls:     0,
		NextAction:       refreshNextAction(status),
	}
}

func refreshNextAction(status string) string {
	switch status {
	case machinecontract.RefreshStatusDeferredUntilStable:
		return mcpMessage("overview.refresh.deferred")
	case machinecontract.RefreshStatusRequired:
		return mcpMessage("overview.refresh.maintain_required")
	case machinecontract.RefreshStatusReadyForOverview:
		return mcpMessage("overview.refresh.ready")
	default:
		return mcpMessage("overview.refresh.not_required")
	}
}

func (session *cognitionRefreshSession) deliveredReceipt(
	current cognitionReceipt,
	eventID string,
) cognitionReceipt {
	if eventID == "" {
		eventID = session.pendingEventReceiptID()
	}
	return receiptWithRefresh(
		receiptWithState(current, cognitionStateValid, true),
		session.generation+1,
		eventID,
		nil,
	)
}

// explicitDeliveryReceipt separates delivery completeness from currency. A
// complete formal body can be returned while source governance is dirty, but
// that body must not establish or clear a reliable model receipt.
func (session *cognitionRefreshSession) explicitDeliveryReceipt(
	current cognitionReceipt,
	assessment cognitionRefreshAssessment,
	eventID string,
	complete bool,
) (cognitionReceipt, bool) {
	if complete && assessment.Semantic.GovernanceAligned {
		return session.deliveredReceipt(current, eventID), true
	}
	state := cognitionStateUncertain
	if !assessment.Semantic.GovernanceAligned {
		state = cognitionStateInvalid
	}
	receipt := receiptWithRefresh(
		receiptWithState(current, state, false),
		session.generation,
		session.lastReceipt.LastRefreshEventID,
		assessment.RefreshReasons,
	)
	return receipt, false
}

func (session *cognitionRefreshSession) pendingEventReceiptID() string {
	events := make([]string, 0, len(session.pendingEvents))
	for eventID := range session.pendingEvents {
		events = append(events, eventID)
	}
	sort.Strings(events)
	switch len(events) {
	case 0:
		return ""
	case 1:
		return events[0]
	default:
		digest := sha256.Sum256([]byte(strings.Join(events, "\x00")))
		return "merged-" + hex.EncodeToString(digest[:8])
	}
}

// recordAttestedDelivery consumes one complete, host-confirmed delivery
// attempt. A failed or partial model Attestation advances the generation but
// deliberately records an uncertain receipt, preventing refresh loops without
// granting permission to claim complete system cognition.
func (session *cognitionRefreshSession) recordAttestedDelivery(
	delivered cognitionReceipt,
	input overviewIn,
	attestation overviewAttestationResult,
	eligible bool,
) {
	if !eligible || input.ModelAttestation == nil ||
		attestation.DeliveryIntegrity != deliveryIntegrityConfirmed {
		return
	}
	reliable := attestation.CognitionAssimilation == cognitionAssimilationComplete
	state := cognitionStateUncertain
	if reliable {
		state = cognitionStateValid
	}
	session.markDeliveryAttempt(receiptWithState(delivered, state, reliable))
}

func (session *cognitionRefreshSession) markDeliveryAttempt(receipt cognitionReceipt) {
	for eventID := range session.pendingEvents {
		session.consumeEvent(eventID)
	}
	session.established = true
	session.generation = receipt.RefreshGeneration
	session.lastReceipt = receipt
	session.pendingReasons = map[string]bool{}
	session.pendingEvents = map[string]bool{}
}
