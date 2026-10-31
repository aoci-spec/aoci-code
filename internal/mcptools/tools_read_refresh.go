package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

type cognitionRefreshSession struct {
	mu         sync.Mutex
	deliveries *volumeDeliveryStore

	established    bool
	generation     int
	lastReceipt    cognitionReceipt
	pendingReasons map[string]bool
	pendingEvents  map[string]bool
	consumedEvents map[string]bool
	// governanceSnapshots binds a delivered Whole-Index body to the live,
	// commit-independent governance facts observed for that delivery.
	governanceSnapshots map[string]string
	// alignedIdentity caches the scope identity that "this session's latest expensive governance evaluation proved aligned".
	// The model's own write advances the receipt identity, but the write path proved the new identity aligned on the spot; the cheap
	// read tools can therefore still report refresh_not_required honestly instead of treating their own write as foreign
	// drift. It is only a cache and takes part in no identity derivation or refresh criterion.
	alignedIdentity string
	// deliveryEvidence remembers, per delivered body, the two halves of evidence that have arrived in this session: the host delivery confirmation and
	// the model attestation. Each half is cryptographically bound to the same body (the confirmation to the body sha and byte count, the attestation to
	// the index sha, sequence sha, count, and challenge digest), and arrival order carries no information,
	// so they may be submitted separately and in any order: when one half arrives and the other is already present it latches, exactly equivalent to
	// submitting both in one call. A brand-new complete delivery (carrying neither field, no cursor) resets that
	// body's evidence, so every delivery attempt still collects its own proof.
	deliveryEvidence map[string]*overviewDeliveryEvidence
	// compactionAttempt, capacity, chains, and chainOrder belong to the
	// Host-capacity loop breaker and the delivery chain IDs the Ledger
	// carries (overview_capacity.go).
	compactionAttempt *compactionDeliveryAttempt
	capacity          *hostCapacityVerdict
	chains            map[string]string
	chainOrder        []string
	// authoringContracts remembers, by contract digest, the refresh
	// generation in which this process last sent the complete authoring
	// contract on a Maintain response (maintain_contract.go).
	authoringContracts map[string]int
	// deltaBase and the probe pass belong to incremental delivery (L5): the
	// base a delta is computed from and the machine proof that the Host still
	// holds it (overview_delta_v1.go).
	deltaBase           *overviewDeltaBase
	probePassed         bool
	probePassIdentity   string
	probePassGeneration int
}

// overviewDeliveryEvidence is the half of evidence that has arrived in this session for one body. The attestation keeps
// the raw model report rather than a score: scoring depends on delivery completeness, and when the other half arrives it is re-scored
// by the completeness of that moment, instead of reusing an old conclusion computed under the "unconfirmed" premise.
type overviewDeliveryEvidence struct {
	confirmed   bool
	attestation *overviewModelAttestation
}

func newCognitionRefreshSession() *cognitionRefreshSession {
	return &cognitionRefreshSession{
		deliveries:          newVolumeDeliveryStore(),
		pendingReasons:      map[string]bool{},
		pendingEvents:       map[string]bool{},
		consumedEvents:      map[string]bool{},
		governanceSnapshots: map[string]string{},
		deliveryEvidence:    map[string]*overviewDeliveryEvidence{},
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
	// Interruption is set when this evaluation recorded a compaction reload
	// interrupted by compaction; the handler writes it to the Ledger.
	Interruption *hostCapacityInterruption `json:"-"`
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

// noteAlignedIdentity records or withdraws the aligned cache on paths that already hold session.mu.
func (session *cognitionRefreshSession) noteAlignedIdentity(identity string, aligned bool) {
	if aligned {
		session.alignedIdentity = identity
		return
	}
	if session.alignedIdentity == identity {
		session.alignedIdentity = ""
	}
}

// RecordAlignedIdentity is for governance paths (maintain/write) that do not hold the lock.
func (session *cognitionRefreshSession) RecordAlignedIdentity(identity string, aligned bool) {
	if session == nil || identity == "" {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.noteAlignedIdentity(identity, aligned)
}

// cognitionStatusLine renders one line of cognition status from pure session-level facts (zero filesystem scanning), appended to the
// tail of search/get_entries/header responses.
//
// Design constraint: this line must never provoke an excessive recall. Receipt identity drift is not by itself a refresh reason (rule §2),
// so when the identity has moved past what the session knows, this line does not fabricate a refresh_status verdict; it only uses the independent
// checkpoint=recommended to suggest one cheap check_only, forming the three-step ladder line → checkpoint → recall,
// each step blocking the abuse of the next. The normal output is refresh_not_required, which is
// the licence that "no recall is needed".
func (session *cognitionRefreshSession) cognitionStatusLine(current cognitionReceipt) string {
	if session == nil {
		return ""
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	generation := session.generation
	pending := session.pendingReasonList()
	identityMatches := session.established && receiptReliable(session.lastReceipt) &&
		receiptIdentityMatches(session.lastReceipt, current)
	const checkpointHint = "; next: call aoci_overview with check_only=true for the exact checkpoint"
	const overviewHint = "; next: call no-argument aoci_overview and follow next_cursor until completed=true"
	switch {
	case !session.established:
		return fmt.Sprintf("cognition: refresh_status=%s generation=%d%s\n",
			machinecontract.RefreshStatusReadyForOverview, generation, overviewHint)
	case len(pending) > 0 && identityMatches:
		return fmt.Sprintf("cognition: refresh_status=%s generation=%d reasons=%s%s\n",
			machinecontract.RefreshStatusReadyForOverview, generation, strings.Join(pending, ","), overviewHint)
	case len(pending) > 0:
		return fmt.Sprintf("cognition: refresh_status=%s generation=%d reasons=%s%s\n",
			machinecontract.RefreshStatusRequired, generation, strings.Join(pending, ","), checkpointHint)
	case identityMatches:
		return fmt.Sprintf("cognition: refresh_status=%s generation=%d\n",
			machinecontract.RefreshStatusNotRequired, generation)
	case session.alignedIdentity != "" && session.alignedIdentity == current.ScopeIdentity:
		return fmt.Sprintf("cognition: refresh_status=%s generation=%d\n",
			machinecontract.RefreshStatusNotRequired, generation)
	default:
		// The identity has moved past what the session knows: fabricate no verdict, only suggest one cheap checkpoint.
		return fmt.Sprintf("cognition: checkpoint=recommended generation=%d%s\n",
			generation, checkpointHint)
	}
}

// sessionCognitionSuffix produces the session cognition line suffix for Volumes v1 read tool responses.
// The Legacy layout, or a missing session, returns an empty string and the read tool outputs as it is.
func sessionCognitionSuffix(root, serviceVersion string, set *cognition.Set, session *cognitionRefreshSession) string {
	if session == nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return ""
	}
	return session.cognitionStatusLine(newVolumeCognitionReceipt(root, serviceVersion, set, mustVolumeScope(set)))
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
	// Read before this request's reasons join the pending set: a fresh
	// compaction declaration over an open compaction reload is the
	// interruption the Host-capacity verdict records.
	interruption := session.noteCompactionDeclaration(hostReasons, eventID)
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

	// The expensive evaluation has already computed the alignment fact; cache it for the cheap read-tool line to reuse (the caller holds the lock).
	session.noteAlignedIdentity(current.ScopeIdentity, facts.GovernanceAligned && facts.Count == 0)

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
	// invalid is reserved for identity breaks: no delivery in this session, or a
	// receipt whose repository, service, or index no longer matches. A reliable
	// receipt with a pending refresh — a dirty tree deferring until stable, a
	// declared compaction, misaligned governance — is delivered cognition under
	// drift, which the runtime contract says stays described by its state and is
	// never demoted to no-cognition. This defaulted to invalid once, and a host
	// that had just passed a 10/10 attestation read "invalid" the moment its
	// working tree carried in-flight edits, which prompts a pointless 40k-token
	// redelivery instead of the maintain cycle the state was pointing at.
	state := cognitionStateInvalid
	if identityReliable {
		state = cognitionStateUncertain
	}
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
		Interruption:     interruption,
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

// mergeDeliveryEvidence merges the half of evidence this call carries into the session memory and returns the host delivery status and
// model report in effect for that body: the half this call did not carry is filled from memory if it arrived earlier for the same
// body. Only a confirmation that hostDeliveryStatus validated as confirmed against the current framed body
// is remembered; a report is kept as it is, and the caller re-scores it against the current Challenge, where an old report
// with a mismatching identity is refused by the envelope validation.
//
// As with the other delivery-path methods in this file, the caller (the Overview handler) already holds session.mu.
func (session *cognitionRefreshSession) mergeDeliveryEvidence(
	bodySHA256 string,
	hostStatus string,
	report *overviewModelAttestation,
) (string, *overviewModelAttestation) {
	if session == nil || bodySHA256 == "" {
		return hostStatus, report
	}
	if session.deliveryEvidence == nil {
		session.deliveryEvidence = map[string]*overviewDeliveryEvidence{}
	}
	evidence := session.deliveryEvidence[bodySHA256]
	if evidence == nil {
		evidence = &overviewDeliveryEvidence{}
		session.deliveryEvidence[bodySHA256] = evidence
	}
	// The half this call explicitly carries always wins as the latest (a mismatching confirmation means the host now
	// says "not fully received", overriding an earlier confirmation); memory only fills the half this call did not carry.
	switch hostStatus {
	case hostDeliveryConfirmed:
		evidence.confirmed = true
	case hostDeliveryIncomplete:
		evidence.confirmed = false
	default:
		if evidence.confirmed {
			hostStatus = hostDeliveryConfirmed
		}
	}
	if report != nil {
		evidence.attestation = report
	} else {
		report = evidence.attestation
	}
	return hostStatus, report
}

// resetDeliveryEvidence discards the old evidence of that body when a brand-new complete delivery starts:
// every delivery attempt collects its own proof, and an old confirmation does not testify for a new transmission.
func (session *cognitionRefreshSession) resetDeliveryEvidence(bodySHA256 string) {
	if session == nil {
		return
	}
	delete(session.deliveryEvidence, bodySHA256)
}

// recordAttestedDelivery consumes one complete, host-confirmed delivery
// attempt. A failed or partial model Attestation advances the generation but
// deliberately records an uncertain receipt, preventing refresh loops without
// granting permission to claim complete system cognition. The attestation
// result already reflects merged session evidence, so a confirmation and an
// Attestation submitted in separate calls latch here exactly like one call.
func (session *cognitionRefreshSession) recordAttestedDelivery(
	delivered cognitionReceipt,
	attestation overviewAttestationResult,
	eligible bool,
) {
	if !eligible || !attestation.ReportProvided ||
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
	// The pending compaction events are consumed, so no reload is open.
	session.compactionAttempt = nil
	session.established = true
	session.generation = receipt.RefreshGeneration
	session.lastReceipt = receipt
	session.pendingReasons = map[string]bool{}
	session.pendingEvents = map[string]bool{}
}
