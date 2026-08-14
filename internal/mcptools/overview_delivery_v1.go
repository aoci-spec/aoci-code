package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	overviewDeliveryReceiptV1       = machinecontract.OverviewDeliveryReceiptV1
	overviewChunkReceiptV1          = machinecontract.OverviewChunkReceiptV1
	overviewRequestFull             = "full"
	overviewRequestCheckOnly        = "check_only"
	overviewDeliveryFull            = "full"
	overviewDeliveryChunked         = "chunked_full"
	overviewDeliveryAttestation     = "attestation"
	overviewDeliveryCheckpoint      = "checkpoint"
	overviewDeliveryBlocked         = "blocked"
	overviewCurrencyCurrent         = "current"
	overviewCurrencyDirty           = "dirty_or_stale"
	hostDeliveryUnconfirmed         = "host_delivery_unconfirmed"
	hostDeliveryConfirmed           = "host_delivery_confirmed"
	hostDeliveryIncomplete          = "host_delivery_incomplete"
	overviewChunkBodyMarker         = "<<<AOCI_OVERVIEW_CHUNK_BODY/v1>>>"
	overviewEntryExceedsChunkBudget = "overview_entry_exceeds_chunk_budget"
)

type overviewHostConfirmation struct {
	Version           string `json:"version"`
	BodySHA256        string `json:"body_sha256"`
	BodyBytes         int    `json:"body_bytes"`
	EndMarkerObserved bool   `json:"end_marker_observed"`
}

type overviewBodyReceipt struct {
	Version                string `json:"version"`
	RuntimeRepositoryRoot  string `json:"runtime_repository_root"`
	Scope                  string `json:"scope"`
	ScopeIdentity          string `json:"scope_identity"`
	EntryCount             int    `json:"entry_count"`
	BodyUTF8Bytes          int    `json:"body_utf8_bytes"`
	BodySHA256             string `json:"body_sha256"`
	StartMarker            string `json:"start_marker"`
	EndMarker              string `json:"end_marker"`
	ServerDeliveryComplete bool   `json:"server_delivery_complete"`
	HostDeliveryStatus     string `json:"host_delivery_status"`
}

type framedOverviewBody struct {
	Text    string
	Receipt overviewBodyReceipt
}

func frameOverviewBody(root, scope, scopeIdentity string, entryCount int, content string) framedOverviewBody {
	start := fmt.Sprintf("<<<AOCI_OVERVIEW_BODY_BEGIN/v1 scope=%s>>>", scope)
	end := fmt.Sprintf("<<<AOCI_OVERVIEW_BODY_END/v1 scope=%s>>>", scope)
	text := start + "\n" + content
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += end + "\n"
	digest := sha256.Sum256([]byte(text))
	return framedOverviewBody{Text: text, Receipt: overviewBodyReceipt{
		Version: overviewDeliveryReceiptV1, RuntimeRepositoryRoot: root,
		Scope: scope, ScopeIdentity: scopeIdentity, EntryCount: entryCount,
		BodyUTF8Bytes: len(text), BodySHA256: hex.EncodeToString(digest[:]),
		StartMarker: start, EndMarker: end, ServerDeliveryComplete: true,
		HostDeliveryStatus: hostDeliveryUnconfirmed,
	}}
}

func hostDeliveryStatus(confirmation *overviewHostConfirmation, body framedOverviewBody) string {
	if confirmation == nil {
		return hostDeliveryUnconfirmed
	}
	if confirmation.Version == overviewDeliveryReceiptV1 && confirmation.EndMarkerObserved &&
		confirmation.BodySHA256 == body.Receipt.BodySHA256 && confirmation.BodyBytes == body.Receipt.BodyUTF8Bytes {
		return hostDeliveryConfirmed
	}
	return hostDeliveryIncomplete
}

type overviewCheckpointEnvelope struct {
	RequestMode            string                     `json:"request_mode"`
	DeliveryMode           string                     `json:"delivery_mode"`
	FullTextIncluded       bool                       `json:"full_text_included"`
	Reason                 string                     `json:"reason"`
	ServerDeliveryComplete bool                       `json:"server_delivery_complete"`
	HostDeliveryStatus     string                     `json:"host_delivery_status"`
	Assessment             cognitionRefreshAssessment `json:"assessment"`
	CognitionStateV2       *overviewCognitionStateV2  `json:"cognition_state_v2,omitempty"`
	CognitionProbe         *cognitionProbe            `json:"cognition_probe,omitempty"`
	ProbeResult            *cognitionProbeResult      `json:"probe_result,omitempty"`
}

func renderOverviewCheckpoint(assessment cognitionRefreshAssessment, input overviewIn, fullScope bool,
	probe *cognitionProbe, probeResult *cognitionProbeResult) string {
	envelope := overviewCheckpointEnvelope{
		RequestMode: overviewRequestCheckOnly, DeliveryMode: overviewDeliveryCheckpoint,
		FullTextIncluded: false, Reason: "explicit_check_only",
		ServerDeliveryComplete: true, HostDeliveryStatus: hostDeliveryIncomplete,
		Assessment: assessment, CognitionProbe: probe, ProbeResult: probeResult,
	}
	if cognitionStateV2Requested(input) {
		state := assessOverviewCognitionStateV2(
			true, fullScope, assessment.Semantic.GovernanceAligned, true,
			overviewAttestationResult{
				DeliveryIntegrity: deliveryIntegrityIncomplete,
				ModelAttestation:  modelAttestationNotProvided,
			},
		)
		envelope.CognitionStateV2 = &state
	}
	data, _ := json.Marshal(envelope)
	return string(data)
}

func renderOverviewBlocked(root, reason string, includeCognitionStateV2 bool) string {
	level := noOverviewCognitionLevel()
	values := map[string]any{
		"request_mode": overviewRequestFull, "delivery_mode": overviewDeliveryBlocked,
		"full_text_included": false, "fallback_reason": reason,
		"runtime_repository_root": root, "server_delivery_complete": false,
		"host_delivery_status":          hostDeliveryIncomplete,
		"model_full_cognition_reliable": false,
		"cognition_level":               level.Level,
		"cognition_level_state":         level.State,
		"cognition_level_message":       level.Message,
	}
	if includeCognitionStateV2 {
		values["cognition_state_v2"] = assessOverviewCognitionStateV2(
			false, false, false, true,
			overviewAttestationResult{
				DeliveryIntegrity: deliveryIntegrityIncomplete,
				ModelAttestation:  modelAttestationNotProvided,
			},
		)
	}
	data, _ := json.Marshal(values)
	return string(data)
}

type overviewRenderContext struct {
	Root              string
	MCPServiceVersion string
	LayoutMode        string
	RequestedScope    string
	EffectiveScope    string
	AssetState        string
	ScopeIdentity     string
	Content           string
	ContentBytes      int
	EntryCount        int
	SectionCount      int
	EstimatedTokens   int
	Receipt           cognitionReceipt
	Assessment        cognitionRefreshAssessment
	Sequence          []overviewChallengeTarget
}

type overviewDeliveryFacts struct {
	DeliveryMode       string
	FullTextIncluded   bool
	IndexSHA256        string
	IndexBytes         int
	OutputBytes        int
	EstimatedTokens    int
	SectionCount       int
	EntryCount         int
	SemanticFiles      int
	BodyUTF8Bytes      int
	BodySHA256         string
	HostDeliveryStatus string
	Attestation        overviewAttestationResult
}

type overviewRendered struct {
	Output      string
	Facts       overviewDeliveryFacts
	Attestation overviewAttestationResult
}

// renderOverviewDelivery chooses the only two body delivery forms: one full
// response for a small body, or a deterministic Entry-boundary chunk chain.
// Attestation reuses the same body identity but never retransmits cognition.
func renderOverviewDelivery(ctx overviewRenderContext, input overviewIn, chunkTokens int) (overviewRendered, error) {
	if len(ctx.Sequence) != ctx.EntryCount {
		return overviewRendered{}, fmt.Errorf("overview_cognition_sequence_count_mismatch")
	}
	challenge := buildOverviewChallenge(ctx.ScopeIdentity, ctx.Sequence)
	framed := frameOverviewBody(ctx.Root, ctx.EffectiveScope, ctx.ScopeIdentity, ctx.EntryCount, ctx.Content)
	hostStatus := hostDeliveryStatus(input.HostConfirmation, framed)
	attestation := assessOverviewAttestation(
		challenge, ctx.EntryCount, ctx.EstimatedTokens,
		hostStatus, ctx.Assessment.Semantic.GovernanceAligned, input.ModelAttestation,
	)
	facts := overviewDeliveryFacts{
		IndexSHA256: ctx.ScopeIdentity, IndexBytes: ctx.ContentBytes,
		EstimatedTokens: ctx.EstimatedTokens, SectionCount: ctx.SectionCount,
		EntryCount: ctx.EntryCount, SemanticFiles: ctx.Assessment.Semantic.Count,
		BodyUTF8Bytes: framed.Receipt.BodyUTF8Bytes, BodySHA256: framed.Receipt.BodySHA256,
		HostDeliveryStatus: hostStatus, Attestation: attestation,
	}

	if input.HostConfirmation != nil || input.ModelAttestation != nil {
		facts.DeliveryMode = overviewDeliveryAttestation
		facts.FullTextIncluded = false
		output := renderOverviewMetadata(
			ctx, challenge, facts, framed, attestation, true, true,
			cognitionStateV2Requested(input),
		)
		facts.OutputBytes = len(output)
		return overviewRendered{Output: output, Facts: facts, Attestation: attestation}, nil
	}

	if len(framed.Text)/3 <= chunkTokens && input.Cursor == "" {
		facts.DeliveryMode = overviewDeliveryFull
		facts.FullTextIncluded = true
		metadata := renderOverviewMetadata(
			ctx, challenge, facts, framed, attestation, false, false,
			cognitionStateV2Requested(input),
		)
		level := assessOverviewCognitionLevel(
			true, attestation.DeliveryIntegrity,
			attestation.ModelAttestation, ctx.Assessment.Semantic.GovernanceAligned,
		)
		// Keep the complete localized explanation in the response, but outside
		// the pre-body transport envelope so localized prose does not consume
		// its cross-platform path-length headroom. Chunk and Attestation
		// responses remain unchanged.
		output := metadata + "\n" + framed.Text + renderOverviewCognitionLevelMessage(level)
		facts.OutputBytes = len(output)
		return overviewRendered{Output: output, Facts: facts, Attestation: attestation}, nil
	}

	cursor := input.Cursor
	if cursor == "" {
		cursor = "start"
	}
	output, err := renderOverviewChunk(
		ctx, framed, challenge, cursor, chunkTokens, attestation,
		cognitionStateV2Requested(input),
	)
	if err != nil {
		return overviewRendered{}, err
	}
	facts.DeliveryMode = overviewDeliveryChunked
	facts.FullTextIncluded = false
	facts.OutputBytes = len(output)
	return overviewRendered{Output: output, Facts: facts, Attestation: attestation}, nil
}

func renderOverviewMetadata(
	ctx overviewRenderContext,
	challenge overviewChallenge,
	facts overviewDeliveryFacts,
	framed framedOverviewBody,
	attestation overviewAttestationResult,
	attestationOnly bool,
	includeCognitionLevelMessage bool,
	includeCognitionStateV2 bool,
) string {
	receipt := ctx.Receipt
	reliable := attestation.CognitionAssimilation == cognitionAssimilationComplete
	state := cognitionStateUncertain
	if !ctx.Assessment.Semantic.GovernanceAligned {
		state = cognitionStateInvalid
	}
	if reliable {
		state = cognitionStateValid
	}
	receipt = receiptWithState(receipt, state, reliable)
	receiptJSON, _ := json.Marshal(receipt)
	receiptLabel := "cognition_receipt"
	if receipt.Version == 2 {
		receiptLabel = "cognition_receipt_v2"
	}
	currency := overviewCurrencyCurrent
	deliveryGuidance := machinecontract.OverviewDeliveryGuidanceNone
	refreshStatus := ctx.Assessment.RefreshStatus
	if !ctx.Assessment.Semantic.GovernanceAligned {
		currency = overviewCurrencyDirty
		deliveryGuidance = machinecontract.OverviewDeliveryGuidanceCurrentSource
	} else if attestationOnly &&
		attestation.DeliveryIntegrity == deliveryIntegrityConfirmed &&
		attestation.ModelAttestation != modelAttestationNotProvided {
		refreshStatus = machinecontract.RefreshStatusNotRequired
		if !reliable {
			deliveryGuidance = machinecontract.OverviewDeliveryGuidanceSourceBound
		}
	}
	completed := !attestationOnly
	deliveryMode := facts.DeliveryMode
	if attestationOnly {
		completed = reliable
	}
	level := assessOverviewCognitionLevel(
		true, attestation.DeliveryIntegrity,
		attestation.ModelAttestation, ctx.Assessment.Semantic.GovernanceAligned,
	)
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"AOCI Overview Metadata:\nrequest_mode: full\ndelivery_mode: %s\ncompleted: %t\n"+
			"continuation_required: false\nfull_text_included: %t\nruntime_repository_root: %s\n"+
			"mcp_service_version: %s\nindex_sha256: %s\nentry_count: %d\nsection_count: %d\n"+
			"estimated_tokens: %d\nindex_bytes: %d\nbody_utf8_bytes: %d\nbody_sha256: %s\n"+
			"server_delivery_complete: true\ndelivery_receipt_version: %s\nhost_delivery_status: %s\ngovernance_aligned: %t\n"+
			"cognition_currency: %s\ndelivery_guidance: %s\nrefresh_generation: %d\nrefresh_status: %s\n"+
			"cognition_state: %s\n"+
			"attestation_contract_version: %s\nchallenge_index_sha256: %s\n"+
			"challenge_entry_sequence_sha256: %s\nchallenge_entry_count: %d\n"+
			"challenge_digest: %s\nchallenge_ordinals: %s\n"+
			"challenge_count: %d\ncognition_level: %d\ncognition_level_state: %s\n"+
			"model_full_cognition_reliable: %t\n",
		deliveryMode, completed, facts.FullTextIncluded,
		overviewMetadataLineValue(ctx.Root), overviewMetadataLineValue(ctx.MCPServiceVersion), ctx.ScopeIdentity,
		ctx.EntryCount, ctx.SectionCount, ctx.EstimatedTokens, ctx.ContentBytes,
		framed.Receipt.BodyUTF8Bytes, framed.Receipt.BodySHA256,
		overviewDeliveryReceiptV1, facts.HostDeliveryStatus, ctx.Assessment.Semantic.GovernanceAligned,
		currency, deliveryGuidance, receipt.RefreshGeneration, refreshStatus, receipt.State,
		modelCognitionAttestationV1, challenge.IndexSHA256,
		challenge.EntrySequenceSHA256, challenge.EntryCount, challenge.Digest,
		formatChallengeOrdinals(challenge.Ordinals), len(challenge.Ordinals),
		level.Level, level.State, reliable,
	)
	if includeCognitionLevelMessage {
		builder.WriteString(renderOverviewCognitionLevelMessage(level))
	}
	if ctx.LayoutMode != "legacy-monolithic" {
		fmt.Fprintf(&builder,
			"layout_mode: %s\nrequested_scope: %s\neffective_scope: %s\nscope_available: true\nasset_state: %s\nmodel_scope_cognition_reliable: %t\n",
			ctx.LayoutMode, ctx.RequestedScope, ctx.EffectiveScope, ctx.AssetState, receipt.ModelScopeReliable,
		)
	}
	if attestationOnly {
		fmt.Fprintf(&builder,
			"delivery_integrity: %s\nmodel_attestation: %s\ncognition_assimilation: %s\n"+
				"reported_entry_count: %d\nreported_estimated_tokens: %d\ncoverage_percent: %.2f\n"+
				"system_mastery_percent: %.2f\nconfidence_percent: %.2f\ntruncation_detected: %t\n"+
				"unseen_sections: %s\nuncertainty_reasons: %s\nchallenge_passed: %d/%d\n",
			attestation.DeliveryIntegrity, attestation.ModelAttestation, attestation.CognitionAssimilation,
			attestation.ReportedEntryCount, attestation.ReportedEstimatedTokens, attestation.CoveragePercent,
			attestation.SystemMasteryPercent, attestation.ConfidencePercent, attestation.TruncationDetected,
			formatAttestationList(attestation.UnseenSections), formatAttestationList(attestation.UncertaintyReasons),
			attestation.ChallengePassed, attestation.ChallengeTotal,
		)
	}
	if includeCognitionStateV2 {
		state := assessOverviewCognitionStateV2(
			true, overviewContextIsFullScope(ctx), ctx.Assessment.Semantic.GovernanceAligned,
			attestationOnly && attestation.ReportProvided, attestation,
		)
		encoded, _ := json.Marshal(state)
		fmt.Fprintf(&builder, "cognition_state_v2: %s\n", encoded)
	}
	// 咨询事实: 磁盘二进制已替换而本进程未重启。只在漂移时出现, 提醒这份认知
	// 来自一个即将过时的服务进程。
	if serviceBinaryReplacedOnDisk() {
		builder.WriteString("service_binary_replaced_on_disk: true\n")
	}
	fmt.Fprintf(&builder, "%s: %s\n", receiptLabel, receiptJSON)
	return builder.String()
}

func renderOverviewCognitionLevelMessage(level overviewCognitionLevel) string {
	return fmt.Sprintf("cognition_level_message: %s\n", overviewMetadataLineValue(level.Message))
}

type overviewChunkSpan struct {
	Start, End   int
	FirstOrdinal int
	LastOrdinal  int
}

func renderOverviewChunk(
	ctx overviewRenderContext,
	body framedOverviewBody,
	challenge overviewChallenge,
	cursor string,
	chunkTokens int,
	attestation overviewAttestationResult,
	includeCognitionStateV2 bool,
) (string, error) {
	contentStart := len(body.Receipt.StartMarker) + 1
	spans, err := planOverviewChunks(body.Text, chunkTokens, contentStart, ctx.Sequence)
	if err != nil {
		return "", err
	}
	chunkIndex := 0
	previousSHA := ""
	if cursor != "start" {
		nextOrdinal, prior, decodeErr := decodeOverviewCursor(cursor, ctx.ScopeIdentity, chunkTokens)
		if decodeErr != nil {
			return "", decodeErr
		}
		previousSHA = prior
		chunkIndex = -1
		for index, span := range spans {
			if span.FirstOrdinal == nextOrdinal {
				chunkIndex = index
				break
			}
		}
		if chunkIndex <= 0 {
			return "", fmt.Errorf("overview_cursor_out_of_order")
		}
		priorSpan := spans[chunkIndex-1]
		digest := sha256.Sum256([]byte(body.Text[priorSpan.Start:priorSpan.End]))
		if previousSHA != hex.EncodeToString(digest[:]) {
			return "", fmt.Errorf("overview_cursor_chain_mismatch")
		}
	}

	span := spans[chunkIndex]
	chunk := body.Text[span.Start:span.End]
	digest := sha256.Sum256([]byte(chunk))
	chunkSHA := hex.EncodeToString(digest[:])
	completed := chunkIndex == len(spans)-1
	next := ""
	if !completed {
		nextOrdinal := spans[chunkIndex+1].FirstOrdinal
		next = encodeOverviewCursor(ctx.ScopeIdentity, chunkTokens, nextOrdinal, chunkSHA)
	}
	metadata := map[string]any{
		"receipt_version":               overviewChunkReceiptV1,
		"request_mode":                  overviewRequestFull,
		"delivery_mode":                 overviewDeliveryChunked,
		"index_sha256":                  ctx.ScopeIdentity,
		"scope":                         ctx.EffectiveScope,
		"chunk_tokens":                  chunkTokens,
		"chunk_index":                   chunkIndex + 1,
		"chunk_count":                   len(spans),
		"first_entry_ordinal":           span.FirstOrdinal,
		"last_entry_ordinal":            span.LastOrdinal,
		"chunk_body_bytes":              len(chunk),
		"chunk_estimated_tokens":        len(chunk) / 3,
		"chunk_sha256":                  chunkSHA,
		"next_cursor":                   next,
		"completed":                     completed,
		"continuation_required":         !completed,
		"completed_marker":              completed && strings.Contains(chunk, body.Receipt.EndMarker),
		"model_full_cognition_reliable": false,
	}
	level := assessOverviewCognitionLevel(
		true, deliveryIntegrityFromHostStatus(hostDeliveryUnconfirmed),
		modelAttestationNotProvided, ctx.Assessment.Semantic.GovernanceAligned,
	)
	metadata["cognition_level"] = level.Level
	metadata["cognition_level_state"] = level.State
	metadata["cognition_level_message"] = level.Message
	if includeCognitionStateV2 {
		metadata["cognition_state_v2"] = assessOverviewCognitionStateV2(
			true, overviewContextIsFullScope(ctx), ctx.Assessment.Semantic.GovernanceAligned,
			false, attestation,
		)
	}
	if chunkIndex == 0 {
		metadata["runtime_repository_root"] = ctx.Root
		metadata["entry_count"] = ctx.EntryCount
		metadata["section_count"] = ctx.SectionCount
		metadata["estimated_tokens"] = ctx.EstimatedTokens
		metadata["index_bytes"] = ctx.ContentBytes
		metadata["body_utf8_bytes"] = body.Receipt.BodyUTF8Bytes
		metadata["body_sha256"] = body.Receipt.BodySHA256
		metadata["governance_aligned"] = ctx.Assessment.Semantic.GovernanceAligned
	}
	if completed {
		metadata["attestation_contract_version"] = modelCognitionAttestationV1
		metadata["challenge_version"] = challenge.Version
		metadata["challenge_index_sha256"] = challenge.IndexSHA256
		metadata["challenge_entry_sequence_sha256"] = challenge.EntrySequenceSHA256
		metadata["challenge_entry_count"] = challenge.EntryCount
		metadata["challenge_digest"] = challenge.Digest
		metadata["challenge_ordinals"] = challenge.Ordinals
		metadata["challenge_count"] = len(challenge.Ordinals)
		metadata["attestation_prompt"] = attestationPromptForCognitionState(includeCognitionStateV2)
		metadata["host_delivery_confirmation_version"] = overviewDeliveryReceiptV1
	}
	encoded, _ := json.Marshal(metadata)
	return string(encoded) + "\n" + overviewChunkBodyMarker + "\n" + chunk, nil
}

func planOverviewChunks(
	text string,
	chunkTokens int,
	contentStart int,
	sequence []overviewChallengeTarget,
) ([]overviewChunkSpan, error) {
	if chunkTokens < machinecontract.OverviewChunkTokensMin || chunkTokens > machinecontract.OverviewChunkTokensMax {
		return nil, fmt.Errorf("overview_chunk_tokens_invalid")
	}
	maxBytes := chunkTokens * 3
	entryOffsets := make([]int, len(sequence))
	previous := -1
	for ordinal, object := range sequence {
		offset := contentStart + object.ContentOffset
		if offset < contentStart || offset >= len(text) || offset <= previous {
			return nil, fmt.Errorf("overview_cognition_sequence_offset_invalid")
		}
		entryOffsets[ordinal] = offset
		previous = offset
	}
	for index, start := range entryOffsets {
		end := len(text)
		if newline := strings.IndexByte(text[start:], '\n'); newline >= 0 {
			end = start + newline + 1
		}
		if (end-start)/3 > chunkTokens {
			return nil, fmt.Errorf("%s(ordinal=%d,estimated_tokens=%d,chunk_tokens=%d)",
				overviewEntryExceedsChunkBudget, index+1, (end-start)/3, chunkTokens)
		}
	}
	candidates := make([]int, 0, len(entryOffsets)+1)
	for _, offset := range entryOffsets {
		if offset > 0 {
			candidates = append(candidates, offset)
		}
	}
	candidates = append(candidates, len(text))
	boundaries := []int{0}
	start := 0
	lastFit := 0
	for position := 0; position < len(candidates); {
		candidate := candidates[position]
		if candidate-start <= maxBytes {
			lastFit = candidate
			position++
			continue
		}
		if lastFit == start {
			return nil, fmt.Errorf("overview_chunk_budget_exceeded")
		}
		boundaries = append(boundaries, lastFit)
		start = lastFit
	}
	if boundaries[len(boundaries)-1] != len(text) {
		boundaries = append(boundaries, len(text))
	}
	spans := make([]overviewChunkSpan, 0, len(boundaries)-1)
	for index := 0; index < len(boundaries)-1; index++ {
		span := overviewChunkSpan{Start: boundaries[index], End: boundaries[index+1]}
		for ordinal, offset := range entryOffsets {
			if offset >= span.Start && offset < span.End {
				if span.FirstOrdinal == 0 {
					span.FirstOrdinal = ordinal + 1
				}
				span.LastOrdinal = ordinal + 1
			}
		}
		spans = append(spans, span)
	}
	return spans, nil
}

func encodeOverviewCursor(identity string, chunkTokens, nextOrdinal int, previousSHA string) string {
	return identity + ":" + strconv.Itoa(chunkTokens) + ":" + strconv.Itoa(nextOrdinal) + ":" + previousSHA
}

func decodeOverviewCursor(cursor, currentIdentity string, currentTokens int) (int, string, error) {
	parts := strings.Split(cursor, ":")
	if len(parts) != 4 {
		return 0, "", fmt.Errorf("overview_cursor_invalid")
	}
	if parts[0] != currentIdentity {
		return 0, "", fmt.Errorf("overview_snapshot_changed")
	}
	tokens, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, "", fmt.Errorf("overview_cursor_invalid")
	}
	if tokens != currentTokens {
		return 0, "", fmt.Errorf("overview_chunk_tokens_changed")
	}
	ordinal, err := strconv.Atoi(parts[2])
	if err != nil || ordinal < 1 || len(parts[3]) != 64 {
		return 0, "", fmt.Errorf("overview_cursor_invalid")
	}
	return ordinal, parts[3], nil
}

func countOverviewDimensions(document *index.Document) (int, int) {
	if document == nil {
		return 0, 0
	}
	sections, entries := 0, 0
	for _, section := range document.Sections {
		if section.AbsPath == "" {
			continue
		}
		sections++
		entries += len(section.Entries)
	}
	return sections, entries
}

func overviewMetadataLineValue(value string) string {
	return strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(value)
}

func overviewFallbackDisplayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (facts overviewDeliveryFacts) ledgerEvent(durationMs int64) ledger.Event {
	fullTextIncluded := facts.FullTextIncluded
	overviewReads := 0
	if facts.FullTextIncluded || facts.DeliveryMode == overviewDeliveryChunked {
		overviewReads = 1
	}
	return ledger.Event{
		Op: "overview", DurationMs: durationMs, Source: ledger.SourceAgent,
		DeliveryMode: facts.DeliveryMode, FullTextIncluded: &fullTextIncluded,
		IndexSHA256: facts.IndexSHA256, IndexBytes: facts.IndexBytes,
		OutputBytes: facts.OutputBytes, EstimatedTokens: facts.EstimatedTokens,
		SectionCount: facts.SectionCount, EntryCount: facts.EntryCount,
		SemanticFiles: facts.SemanticFiles, AOCIToolCalls: 1, OverviewReads: overviewReads,
	}
}
