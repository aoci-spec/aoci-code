package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func handleVolumeOverview(
	root, mcpServiceVersion string,
	input overviewIn,
	loaded *cognitionRepoCtx,
	refreshSession *cognitionRefreshSession,
	start time.Time,
) *mcp.CallToolResult {
	view, err := loaded.set.Scope(input.Scope)
	if err != nil {
		return errResult(errBadArgs, mcpMessage("mcp.scope.invalid", input.Scope), "")
	}
	hostReasons, eventID, fail := normalizeRefreshInput(input)
	if fail != nil {
		return failResult(fail)
	}
	current := newVolumeCognitionReceipt(root, mcpServiceVersion, loaded.set, view)
	semantic, governanceSnapshot, semanticFail := inspectVolumeGovernance(
		root, loaded, !input.CheckOnly && view.Available,
	)
	if semanticFail != nil {
		return failResult(semanticFail)
	}

	refreshSession.mu.Lock()
	defer refreshSession.mu.Unlock()
	assessment := refreshSession.evaluate(input, current, semantic, hostReasons, eventID)
	if input.CheckOnly {
		var probe *cognitionProbe
		var probeResult *cognitionProbeResult
		if input.Probe || input.ProbeAnswers != nil {
			_, sequence, sequenceErr := buildVolumeOverviewBody(view)
			if sequenceErr != nil {
				return errResult(errInternal, sequenceErr.Error(), "")
			}
			if input.Probe {
				probe = buildCognitionProbe(view.ScopeIdentity, assessment.Receipt.RefreshGeneration, sequence)
			} else {
				probeResult = gradeCognitionProbe(input.ProbeAnswers, view.ScopeIdentity, assessment.Receipt.RefreshGeneration, sequence)
			}
		}
		compact := renderOverviewCheckpoint(
			assessment, input,
			view.EffectiveScope == cognition.ScopeAll,
			probe, probeResult,
		)
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{
			Op: "cognition_check", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent,
			AOCIToolCalls: 1, OutputBytes: len(compact), EstimatedTokens: len(compact) / 3,
			IndexSHA256: view.ScopeIdentity,
		})
		return textResult(compact)
	}
	if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
		return textResult(renderOverviewBlocked(root, "recovery_pending", cognitionStateV2Requested(input)))
	}
	if !view.Available {
		delivered, _ := refreshSession.explicitDeliveryReceipt(
			current, assessment, eventID, false,
		)
		output := renderAbsentVolumeOverview(
			root, loaded.set, view, delivered, assessment,
			cognitionStateV2Requested(input),
		)
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{
			Op: "overview", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent,
			AOCIToolCalls: 1, OverviewReads: 1, IndexSHA256: view.ScopeIdentity,
			OutputBytes: len(output), EstimatedTokens: len(output) / 3,
		})
		return textResult(output)
	}
	body, sequence, sequenceErr := buildVolumeOverviewBody(view)
	if sequenceErr != nil {
		return errResult(errInternal, sequenceErr.Error(), "")
	}
	framed := frameOverviewBody(root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	keyDigest := sha256.Sum256([]byte(view.ScopeIdentity + "\x00" + framed.Receipt.BodySHA256))
	governanceKey := hex.EncodeToString(keyDigest[:])
	if !refreshSession.bindVolumeGovernanceSnapshot(governanceKey, governanceSnapshot.bindingIdentity, input) {
		assessment.Semantic.GovernanceAligned = false
		assessment.Semantic.GovernanceBlockerCount++
	}
	delivered, markDelivered := refreshSession.explicitDeliveryReceipt(
		current, assessment, eventID, true,
	)
	delivered = receiptWithDeliveredVolumes(delivered, view)
	delivery, renderErr := renderOverviewDelivery(overviewRenderContext{
		Root: root, MCPServiceVersion: mcpServiceVersion,
		LayoutMode: string(loaded.set.LayoutMode), RequestedScope: view.RequestedScope,
		EffectiveScope: view.EffectiveScope, AssetState: view.AssetState,
		ScopeIdentity: view.ScopeIdentity, Content: body, ContentBytes: volumeScopeBytes(view),
		EntryCount: view.ObjectCount, SectionCount: countVolumeSections(view),
		EstimatedTokens: volumeScopeBytes(view) / 3,
		Receipt:         delivered, Assessment: assessment, Sequence: sequence,
		Session: refreshSession,
	}, input, loaded.cfg.OverviewDelivery.ChunkTokens)
	if renderErr != nil {
		return errResult(errBadArgs, renderErr.Error(), "")
	}
	if snapshotFail := confirmCognitionSnapshot(root, loaded.set); snapshotFail != nil {
		return failResult(snapshotFail)
	}
	if snapshotFail := confirmVolumeGovernanceSnapshot(root, loaded, governanceSnapshot); snapshotFail != nil {
		return failResult(snapshotFail)
	}
	if input.Cursor == "" && input.HostConfirmation == nil && input.ModelAttestation == nil {
		refreshSession.replaceFrozenOverviewContinuationLocked(
			loaded.cfg.IndexPath, delivery.Frozen, governanceSnapshot,
		)
	}
	refreshSession.recordAttestedDelivery(delivered, delivery.Attestation, markDelivered)
	ledger.Append(root, loaded.cfg.LedgerEnabled, delivery.Facts.ledgerEvent(time.Since(start).Milliseconds()))
	return textResult(delivery.Output)
}

func renderAbsentVolumeOverview(
	root string,
	set *cognition.Set,
	view cognition.ScopeView,
	receipt cognitionReceipt,
	assessment cognitionRefreshAssessment,
	includeCognitionStateV2 bool,
) string {
	receipt = receiptWithState(receipt, cognitionStateUncertain, false)
	receiptJSON, _ := json.Marshal(receipt)
	level := noOverviewCognitionLevel()
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"AOCI Overview Metadata:\nrequest_mode: full\nlayout_mode: %s\nrequested_scope: %s\neffective_scope: %s\nscope_available: %t\nasset_state: %s\ndelivery_mode: scope_absent\ncompleted: false\ncontinuation_required: false\nfull_text_included: false\nfallback_reason: requested_object_volume_absent\nroot_sha256: %s\nmeta_sha256: %s\nscope_object_count: %d\nscope_identity: %s\ncomposite_identity: %s\ncognition_level: %d\ncognition_level_state: %s\ncognition_level_message: %s\nmodel_scope_cognition_reliable: false\nmodel_full_cognition_reliable: false\nrefresh_status: %s\nsemantic_change_count: %d\ncognition_receipt_v2: %s\n",
		set.LayoutMode, view.RequestedScope, view.EffectiveScope, view.Available, view.AssetState,
		set.Root.SHA256, set.Meta.SHA256, view.ObjectCount,
		view.ScopeIdentity, set.CompositeIdentity, level.Level, level.State,
		overviewMetadataLineValue(level.Message), assessment.RefreshStatus,
		assessment.Semantic.Count, receiptJSON)
	if includeCognitionStateV2 {
		state := assessOverviewCognitionStateV2(
			false, false, assessment.Semantic.GovernanceAligned, true,
			overviewAttestationResult{
				DeliveryIntegrity: deliveryIntegrityIncomplete,
				ModelAttestation:  modelAttestationNotProvided,
			},
		)
		encoded, _ := json.Marshal(state)
		fmt.Fprintf(&builder, "cognition_state_v2: %s\n", encoded)
	}
	return builder.String()
}

func buildVolumeOverviewBody(view cognition.ScopeView) (string, []overviewChallengeTarget, error) {
	var builder strings.Builder
	sequence := make([]overviewChallengeTarget, 0, view.ObjectCount)
	for _, asset := range view.Assets {
		fmt.Fprintf(&builder, "──────────────────────────────\nAOCI Cognition Asset: id=%s kind=%s path=%s sha256=%s object_count=%d\n──────────────────────────────\n",
			asset.Descriptor.ID, asset.Descriptor.Kind, asset.Descriptor.Path, asset.SHA256, asset.ObjectCount)
		assetStart := builder.Len()
		builder.Write(asset.Raw)
		raw := string(asset.Raw)
		for _, object := range asset.Objects {
			if object.Entry == nil {
				continue
			}
			offset, err := overviewObjectLineOffset(raw, object.SourceLineNumber, object.Entry.FullLine)
			if err != nil {
				return "", nil, err
			}
			sequence = append(sequence, overviewChallengeTarget{
				ContentOffset: assetStart + offset, ObjectIdentity: object.CanonicalRef,
				Tag: object.Entry.TagsRaw, CoreF: object.Entry.F,
			})
		}
		if len(asset.Raw) == 0 || asset.Raw[len(asset.Raw)-1] != '\n' {
			builder.WriteByte('\n')
		}
	}
	if len(sequence) != view.ObjectCount {
		return "", nil, fmt.Errorf("overview_cognition_sequence_count_mismatch")
	}
	return builder.String(), sequence, nil
}

func volumeScopeBytes(view cognition.ScopeView) int {
	total := 0
	for _, asset := range view.Assets {
		total += len(asset.Raw)
	}
	return total
}

func countVolumeSections(view cognition.ScopeView) int {
	total := 0
	for _, asset := range view.Assets {
		if asset.Document == nil {
			continue
		}
		for _, section := range asset.Document.Sections {
			if section.AbsPath != "" {
				total++
			}
		}
	}
	return total
}
