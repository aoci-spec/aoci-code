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
	// alignedIdentity 缓存"本会话最近一次昂贵治理评估证明对齐"的 scope 身份。
	// 模型自己的写入会让收据身份前移, 但写入路径当场证明了新身份对齐; 廉价的
	// 读工具据此仍能诚实报告 refresh_not_required, 而不是把自己的写当成陌生
	// 漂移。它只是缓存, 不参与任何身份推导或刷新判据。
	alignedIdentity string
	// deliveryEvidence 按交付 body 记住本会话已到达的两半证据: 宿主交付确认与
	// 模型认证。两半各自密码学绑定同一 body(确认绑 body sha 与字节数, 认证绑
	// index sha、序列 sha、数量与 challenge digest), 到达先后不携带任何信息,
	// 所以允许分次、任意顺序提交: 任一半到达时若另一半已在场即闩住, 与同一
	// 调用合并提交完全等价。一次全新的完整交付(不带任一字段、无游标)重置该
	// body 的证据, 使每次交付尝试仍各自取证。
	deliveryEvidence map[string]*overviewDeliveryEvidence
	// frozenOverview is one bounded, process-local transport snapshot. A new
	// strict first delivery atomically replaces it; cache misses keep the
	// compatible stateless path. It retains only the opaque governance input
	// observation needed to reject drift; it never retains or reuses a verdict.
	frozenOverview *frozenOverviewContinuation
}

type frozenOverviewContinuation struct {
	indexPath  string
	plan       overviewFrozenChunkPlan
	governance volumeGovernanceSnapshot
	set        *cognition.Set
	eligible   bool
}

// overviewDeliveryEvidence 是一个 body 在本会话内已到达的证据半边。认证保存
// 原始模型报告而非评分结果: 评分依赖交付完整性, 另一半到达时按当时的完整性
// 重新评分, 而不是复用一份在"未确认"前提下算出的旧结论。
type overviewDeliveryEvidence struct {
	confirmed   bool
	attestation *overviewModelAttestation
}

// replaceFrozenOverviewContinuationLocked is called only while session.mu is
// held, after the first delivery has passed its formal and governance snapshot
// confirmations. Passing nil clears any older transport image.
func (session *cognitionRefreshSession) replaceFrozenOverviewContinuationLocked(
	indexPath string,
	plan *overviewFrozenChunkPlan,
	governance volumeGovernanceSnapshot,
	set *cognition.Set,
	eligible bool,
) {
	if session == nil {
		return
	}
	session.frozenOverview = nil
	if plan == nil || set == nil || plan.Context.LayoutMode != string(cognition.LayoutVolumesV1) ||
		governance.observation.Identity() == "" {
		return
	}
	copyPlan := *plan
	copyPlan.Spans = append([]overviewChunkSpan(nil), plan.Spans...)
	copyPlan.Challenge.Ordinals = append([]int(nil), plan.Challenge.Ordinals...)
	if plan.Challenge.Targets != nil {
		copyPlan.Challenge.Targets = make(map[int]overviewChallengeTarget, len(plan.Challenge.Targets))
		for ordinal, target := range plan.Challenge.Targets {
			copyPlan.Challenge.Targets[ordinal] = target
		}
	}
	session.frozenOverview = &frozenOverviewContinuation{
		indexPath: indexPath, plan: copyPlan, governance: governance,
		set: set, eligible: eligible,
	}
}

// frozenOverviewCursor returns the latest immutable delivery image for a
// genuine continuation cursor. Both middle and final Chunks reuse the first
// request's governance assessment; the caller decides which lightweight
// confirmation is required before returning either kind of Chunk.
func (session *cognitionRefreshSession) frozenOverviewCursor(
	cursor string,
) (*frozenOverviewContinuation, bool, bool, error) {
	if session == nil {
		return nil, false, false, nil
	}
	session.mu.Lock()
	frozen := session.frozenOverview
	session.mu.Unlock()
	if frozen == nil || frozen.plan.Context.LayoutMode != string(cognition.LayoutVolumesV1) {
		return nil, false, false, nil
	}
	if !strings.HasPrefix(cursor, frozen.plan.Context.ScopeIdentity+":") {
		return nil, false, false, nil
	}
	chunkIndex, err := overviewChunkIndex(
		frozen.plan.Body.Text, frozen.plan.Spans, cursor,
		frozen.plan.Context.ScopeIdentity, frozen.plan.ChunkTokens,
	)
	if err != nil {
		return nil, false, true, err
	}
	return frozen, chunkIndex == len(frozen.plan.Spans)-1, true, nil
}

// frozenOverviewProof returns the latest body-bound delivery image. Input
// identity is graded by the existing Host-confirmation and Attestation code;
// this lookup only supplies the immutable body that those envelopes name.
func (session *cognitionRefreshSession) frozenOverviewProof() (*frozenOverviewContinuation, bool) {
	if session == nil {
		return nil, false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.frozenOverview == nil ||
		session.frozenOverview.plan.Context.LayoutMode != string(cognition.LayoutVolumesV1) {
		return nil, false
	}
	return session.frozenOverview, true
}

func newCognitionRefreshSession() *cognitionRefreshSession {
	return &cognitionRefreshSession{
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

// noteAlignedIdentity 在已持有 session.mu 的路径上记录或撤销对齐缓存。
func (session *cognitionRefreshSession) noteAlignedIdentity(identity string, aligned bool) {
	if aligned {
		session.alignedIdentity = identity
		return
	}
	if session.alignedIdentity == identity {
		session.alignedIdentity = ""
	}
}

// RecordAlignedIdentity 供未持锁的治理路径(maintain/写入)调用。
func (session *cognitionRefreshSession) RecordAlignedIdentity(identity string, aligned bool) {
	if session == nil || identity == "" {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.noteAlignedIdentity(identity, aligned)
}

// cognitionStatusLine 用纯会话级事实(零文件系统扫描)渲染一行认知状态, 贴在
// search/get_entries/header 响应尾部。
//
// 设计约束: 这一行绝不能诱发过度召回。收据身份漂移本身不是刷新理由(规则 §2),
// 所以身份越过会话所知时, 这一行不伪造 refresh_status 判定, 只用独立的
// checkpoint=recommended 建议一次廉价的 check_only —— 形成 行→checkpoint→召回
// 的三级阶梯, 每级挡住下一级的滥用。常态输出是 refresh_not_required, 它是
// "不需要召回"的许可证。
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
		// 身份越过了会话所知: 不伪造判定, 只建议一次廉价 checkpoint。
		return fmt.Sprintf("cognition: checkpoint=recommended generation=%d%s\n",
			generation, checkpointHint)
	}
}

// sessionCognitionSuffix 为 Volumes v1 读工具响应生成会话认知行后缀。
// Legacy 布局或缺会话时返回空串, 读工具原样输出。
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

	// 昂贵评估已经算出对齐事实, 缓存给廉价的读工具行复用(调用方持锁)。
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

// mergeDeliveryEvidence 把本次调用携带的证据半边并入会话记忆, 并返回对该
// body 生效的宿主交付状态与模型报告: 本次没带的那一半, 若此前已针对同一 body
// 到达, 就从记忆补上。只有经 hostDeliveryStatus 对当前 framed body 校验为
// confirmed 的确认才会被记住; 报告则原样保存, 由调用方对当前 Challenge 重新
// 评分, 身份不符的旧报告会在那里被 envelope 校验拒绝。
//
// 与本文件其他交付路径方法一样, 调用方(Overview 处理器)已持有 session.mu。
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
	// 本次调用明确携带的一半永远以最新为准(一份不匹配的确认表示宿主现在
	// 说"没收全", 它压过更早的确认); 记忆只填补本次没带的那一半。
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

// resetDeliveryEvidence 在一次全新的完整交付开始时丢弃该 body 的旧证据:
// 每次交付尝试各自取证, 旧确认不为新一轮传输作证。
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
	session.established = true
	session.generation = receipt.RefreshGeneration
	session.lastReceipt = receipt
	session.pendingReasons = map[string]bool{}
	session.pendingEvents = map[string]bool{}
}
