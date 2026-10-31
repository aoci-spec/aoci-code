// Incremental delivery (L5). A long task reads the Whole-Index once and then
// changes it; the next phase wants the system view again and today pays the
// complete scope for it, although the model still holds the previous delivery
// and only the Entries it wrote, and whatever third parties wrote, differ. The
// probe already proves that retained cognition makes a re-delivery
// unnecessary; the delta is what that proof buys: the objects changed since
// the last attested body, and nothing else, attested over exactly that body.
//
// The server keeps, in the Host session, only the base's identities and its
// object-identity/line-digest sequence, never a body. The base moves only on
// a passing Attestation under aligned governance, complete or incremental, is
// dropped by a partial or failed incremental Attestation, and is never
// persisted.
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	overviewDeliveryDelta        = machinecontract.OverviewDeliveryModeDelta
	overviewDeliveryChunkedDelta = machinecontract.OverviewDeliveryModeChunkedDelta
)

// deltaObjectDigest is one object of the canonical sequence as the base
// remembers it: its identity and the digest of its formal line.
type deltaObjectDigest struct {
	Identity   string
	LineSHA256 string
}

// overviewDeltaBase is what an incremental delivery is computed from.
type overviewDeltaBase struct {
	ScopeIdentity     string
	CompositeIdentity string
	RootSHA256        string
	MetaSHA256        string
	Generation        int
	Objects           []deltaObjectDigest
}

// overviewDeltaFacts is the Metadata a delta delivery reports beside the
// ordinary receipt: the base it was computed from and what differs.
type overviewDeltaFacts struct {
	BaseScopeIdentity string
	BaseGeneration    int
	ChangedOrdinals   []int
	AddedOrdinals     []int
	RemovedIdentities []string
	ObjectCount       int
	ScopeObjectCount  int
	HeaderIncluded    bool
}

// overviewDeltaPlan is the comparison of the current canonical sequence with
// the base's: a changed line digest, an identity the base lacks, an identity
// the current sequence lacks.
type overviewDeltaPlan struct {
	changed map[string]bool
	added   map[string]bool
	removed []string
}

func (plan overviewDeltaPlan) deltaCount() int {
	return len(plan.changed) + len(plan.added)
}

func (plan overviewDeltaPlan) exceedsShare(currentObjects int) bool {
	return plan.deltaCount()*100 > machinecontract.OverviewDeltaMaxSharePercent*currentObjects
}

// deltaDigestSequence takes the current canonical object sequence of the view
// in the exact order the Overview body and the Challenge use.
func deltaDigestSequence(set *cognition.Set, view cognition.ScopeView, generation int) *overviewDeltaBase {
	base := &overviewDeltaBase{
		ScopeIdentity: view.ScopeIdentity, CompositeIdentity: set.CompositeIdentity,
		RootSHA256: set.Root.SHA256, MetaSHA256: set.Meta.SHA256, Generation: generation,
		Objects: make([]deltaObjectDigest, 0, view.ObjectCount),
	}
	for _, asset := range view.Assets {
		for _, object := range asset.Objects {
			if object.Entry == nil {
				continue
			}
			digest := sha256.Sum256([]byte(object.Entry.FullLine))
			base.Objects = append(base.Objects, deltaObjectDigest{Identity: object.CanonicalRef, LineSHA256: hex.EncodeToString(digest[:])})
		}
	}
	return base
}

func computeOverviewDelta(base, current *overviewDeltaBase) overviewDeltaPlan {
	plan := overviewDeltaPlan{changed: map[string]bool{}, added: map[string]bool{}, removed: []string{}}
	baseLines := make(map[string]string, len(base.Objects))
	for _, object := range base.Objects {
		baseLines[object.Identity] = object.LineSHA256
	}
	seen := make(map[string]bool, len(current.Objects))
	for _, object := range current.Objects {
		seen[object.Identity] = true
		line, known := baseLines[object.Identity]
		switch {
		case !known:
			plan.added[object.Identity] = true
		case line != object.LineSHA256:
			plan.changed[object.Identity] = true
		}
	}
	for _, object := range base.Objects {
		if !seen[object.Identity] {
			plan.removed = append(plan.removed, object.Identity)
		}
	}
	return plan
}

// headerChanged reports whether Root or Meta differ from the base.
func (base *overviewDeltaBase) headerChanged(current *overviewDeltaBase) bool {
	return base.RootSHA256 != current.RootSHA256 || base.MetaSHA256 != current.MetaSHA256
}

// buildVolumeDeltaBody renders the delta: the Root and Meta bytes only when
// their digest differs from the base's, then, Volume by Volume in the current
// formal order, the Volume banner and header line, the enclosing section
// header of each delivered object, and every changed or added Entry exactly
// once. The sequence it returns carries each object's current ordinal and its
// offset in the delta content, so chunk planning and the Challenge work on
// the delta body exactly as they do on a complete one.
func buildVolumeDeltaBody(set *cognition.Set, view cognition.ScopeView, base, current *overviewDeltaBase, plan overviewDeltaPlan) (string, []overviewChallengeTarget, overviewDeltaFacts, error) {
	facts := overviewDeltaFacts{
		BaseScopeIdentity: base.ScopeIdentity, BaseGeneration: base.Generation,
		ChangedOrdinals: []int{}, AddedOrdinals: []int{}, RemovedIdentities: append([]string{}, plan.removed...),
		ScopeObjectCount: view.ObjectCount, HeaderIncluded: base.headerChanged(current),
	}
	var builder strings.Builder
	sequence := make([]overviewChallengeTarget, 0, plan.deltaCount())
	ordinal := 0
	for _, asset := range view.Assets {
		kind := asset.Descriptor.Kind
		if kind == "root" || kind == "meta" {
			if facts.HeaderIncluded {
				writeOverviewAssetBanner(&builder, asset)
				builder.Write(asset.Raw)
				if len(asset.Raw) == 0 || asset.Raw[len(asset.Raw)-1] != '\n' {
					builder.WriteByte('\n')
				}
			}
			continue
		}
		included := 0
		for _, object := range asset.Objects {
			if object.Entry != nil && (plan.changed[object.CanonicalRef] || plan.added[object.CanonicalRef]) {
				included++
			}
		}
		if included == 0 {
			ordinal += countObjectsWithEntries(asset)
			continue
		}
		writeOverviewAssetBanner(&builder, asset)
		if header, _, found := strings.Cut(string(asset.Raw), "\n"); found && strings.HasPrefix(header, "#") {
			builder.WriteString(header)
			builder.WriteByte('\n')
		}
		section := ""
		for _, object := range asset.Objects {
			if object.Entry == nil {
				continue
			}
			ordinal++
			changed, added := plan.changed[object.CanonicalRef], plan.added[object.CanonicalRef]
			if !changed && !added {
				continue
			}
			if header := enclosingSectionHeader(asset, object); header != "" && header != section {
				section = header
				builder.WriteString(section)
				builder.WriteByte('\n')
			}
			sequence = append(sequence, overviewChallengeTarget{
				ContentOffset: builder.Len(), Ordinal: ordinal, ObjectIdentity: object.CanonicalRef,
				Tag: object.Entry.TagsRaw, CoreF: object.Entry.F,
			})
			builder.WriteString(object.Entry.FullLine)
			builder.WriteByte('\n')
			if changed {
				facts.ChangedOrdinals = append(facts.ChangedOrdinals, ordinal)
			} else {
				facts.AddedOrdinals = append(facts.AddedOrdinals, ordinal)
			}
		}
	}
	if len(sequence) != plan.deltaCount() {
		return "", nil, facts, fmt.Errorf("overview_delta_sequence_count_mismatch")
	}
	facts.ObjectCount = len(sequence)
	return builder.String(), sequence, facts, nil
}

// enclosingSectionHeader is the header line of the directory section the
// object's Entry sits in: the last section that starts at or before its line.
func enclosingSectionHeader(asset *cognition.Asset, object cognition.Object) string {
	if asset.Document == nil {
		return object.SourceSection
	}
	header := object.SourceSection
	for _, section := range asset.Document.Sections {
		if section.AbsPath == "" || section.StartLine > object.SourceLineNumber {
			continue
		}
		header = section.HeaderLine
	}
	return header
}

func writeOverviewAssetBanner(builder *strings.Builder, asset *cognition.Asset) {
	fmt.Fprintf(builder, "──────────────────────────────\nAOCI Cognition Asset: id=%s kind=%s path=%s sha256=%s object_count=%d\n──────────────────────────────\n",
		asset.Descriptor.ID, asset.Descriptor.Kind, asset.Descriptor.Path, asset.SHA256, asset.ObjectCount)
}

func countObjectsWithEntries(asset *cognition.Asset) int {
	count := 0
	for _, object := range asset.Objects {
		if object.Entry != nil {
			count++
		}
	}
	return count
}

// buildDeltaChallenge draws the Challenge from the delivered objects only, by
// the same stable-key selection, reported at their current ordinals, while
// the identity facts the Attestation must echo stay the current complete
// sequence's: Index SHA-256, Entry-sequence SHA-256, and Entry count.
func buildDeltaChallenge(scopeIdentity string, full, delta []overviewChallengeTarget) overviewChallenge {
	entrySequenceSHA256 := overviewEntrySequenceSHA256(full)
	picks := stratifiedChallengeOrdinals(delta)
	ordinals := make([]int, 0, len(picks))
	byOrdinal := make(map[int]overviewChallengeTarget, len(picks))
	for _, pick := range picks {
		target := delta[pick-1]
		ordinals = append(ordinals, target.Ordinal)
		byOrdinal[target.Ordinal] = target
	}
	payload, _ := json.Marshal(struct {
		Version             string `json:"version"`
		IndexSHA256         string `json:"index_sha256"`
		EntrySequenceSHA256 string `json:"entry_sequence_sha256"`
		EntryCount          int    `json:"entry_count"`
		Ordinals            []int  `json:"ordinals"`
	}{overviewChallengeVersion, scopeIdentity, entrySequenceSHA256, len(full), ordinals})
	digest := sha256.Sum256(payload)
	return overviewChallenge{
		Version: overviewChallengeVersion, IndexSHA256: scopeIdentity,
		EntrySequenceSHA256: entrySequenceSHA256, EntryCount: len(full),
		Digest: hex.EncodeToString(digest[:]), Ordinals: ordinals, Targets: byOrdinal,
	}
}

// noteProbePass remembers a passing probe over one index identity and
// generation: the machine proof that the Host still holds the base. The
// caller holds session.mu.
func (session *cognitionRefreshSession) noteProbePass(identity string, generation int) {
	session.probePassIdentity, session.probePassGeneration, session.probePassed = identity, generation, true
}

// deltaFallbackReason names why a delta_since request answers the complete
// scope, or returns "" when the delta may be served. The caller holds
// session.mu.
func (session *cognitionRefreshSession) deltaFallbackReason(since, identity string, generation int) string {
	base := session.deltaBase
	switch {
	case base == nil || base.ScopeIdentity != since:
		return machinecontract.OverviewFallbackDeltaBaseUnknown
	case !session.established || !receiptReliable(session.lastReceipt):
		return machinecontract.OverviewFallbackDeltaBaseUnreliable
	case session.compactionPending():
		return machinecontract.OverviewFallbackDeltaCompactionPending
	case !session.probePassed || session.probePassIdentity != identity || session.probePassGeneration != generation:
		return machinecontract.OverviewFallbackDeltaProbeRequired
	}
	return ""
}

// anchorDeltaBase moves the base to the current sequence at the session's
// current generation. The caller holds session.mu.
func (session *cognitionRefreshSession) anchorDeltaBase(current *overviewDeltaBase) {
	if current == nil {
		return
	}
	anchored := *current
	anchored.Objects = append([]deltaObjectDigest{}, current.Objects...)
	anchored.Generation = session.generation
	session.deltaBase = &anchored
}

// settleDeltaBase applies one scored proof to the base: a passing Attestation
// under aligned governance anchors the base at the delivered sequence,
// complete or incremental; a partial or failed incremental Attestation drops
// it so the next request is complete. A dirty delivery moves nothing. The
// caller holds session.mu and has already recorded the attested delivery.
func (session *cognitionRefreshSession) settleDeltaBase(ctx overviewRenderContext, attestation overviewAttestationResult) {
	if !attestation.ReportProvided || attestation.DeliveryIntegrity != deliveryIntegrityConfirmed {
		return
	}
	pass := attestation.ModelAttestation == modelAttestationPass && ctx.Assessment.Semantic.GovernanceAligned
	if ctx.Delta != nil {
		if pass {
			session.anchorDeltaBase(ctx.Anchor)
		} else if attestation.ModelAttestation != modelAttestationPass {
			session.deltaBase = nil
		}
		return
	}
	if pass && ctx.EffectiveScope == cognition.ScopeAll {
		session.anchorDeltaBase(ctx.Anchor)
	}
}

// renderOverviewDeltaEmpty answers a delta with nothing to deliver: no body,
// no Challenge, no proof to submit. The base is re-anchored at the current
// scope identity by the caller and nothing advances.
func renderOverviewDeltaEmpty(ctx overviewRenderContext, receipt cognitionReceipt, facts overviewDeltaFacts, includeCognitionStateV2 bool) string {
	receiptJSON, _ := json.Marshal(receipt)
	currency := overviewCurrencyCurrent
	if !ctx.Assessment.Semantic.GovernanceAligned {
		currency = overviewCurrencyDirty
	}
	level := assessOverviewCognitionLevel(true, deliveryIntegrityConfirmed, modelAttestationPass, ctx.Assessment.Semantic.GovernanceAligned)
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"AOCI Overview Metadata:\nrequest_mode: full\ndelivery_mode: %s\ncompleted: true\ncontinuation_required: false\n"+
			"full_text_included: false\nruntime_repository_root: %s\nmcp_service_version: %s\nindex_sha256: %s\nentry_count: 0\n"+
			"governance_aligned: %t\ncognition_currency: %s\nrefresh_generation: %d\nrefresh_status: %s\ncognition_state: %s\n"+
			"cognition_level: %d\ncognition_level_state: %s\nmodel_full_cognition_reliable: %t\n",
		overviewDeliveryDelta, overviewMetadataLineValue(ctx.Root), overviewMetadataLineValue(ctx.MCPServiceVersion), ctx.ScopeIdentity,
		ctx.Assessment.Semantic.GovernanceAligned, currency, receipt.RefreshGeneration, ctx.Assessment.RefreshStatus, receipt.State,
		level.Level, level.State, receiptReliable(receipt),
	)
	builder.WriteString(renderOverviewDeltaMetadata(facts))
	fmt.Fprintf(&builder, "layout_mode: %s\nrequested_scope: %s\neffective_scope: %s\nscope_available: true\nasset_state: %s\nmodel_scope_cognition_reliable: %t\n",
		ctx.LayoutMode, ctx.RequestedScope, ctx.EffectiveScope, ctx.AssetState, receipt.ModelScopeReliable)
	if includeCognitionStateV2 {
		state := assessOverviewCognitionStateV2(true, true, ctx.Assessment.Semantic.GovernanceAligned, false,
			overviewAttestationResult{DeliveryIntegrity: deliveryIntegrityConfirmed, ModelAttestation: modelAttestationNotProvided})
		encoded, _ := json.Marshal(state)
		fmt.Fprintf(&builder, "cognition_state_v2: %s\n", encoded)
	}
	fmt.Fprintf(&builder, "cognition_receipt_v2: %s\n", receiptJSON)
	return builder.String()
}

// renderOverviewDeltaMetadata is the delta's own Metadata block, shared by the
// full-form metadata, the first chunk, and the empty delta.
func renderOverviewDeltaMetadata(facts overviewDeltaFacts) string {
	removed, _ := json.Marshal(facts.RemovedIdentities)
	return fmt.Sprintf(
		"delta_base_scope_identity: %s\ndelta_base_refresh_generation: %d\ndelta_object_count: %d\n"+
			"delta_changed_ordinals: %s\ndelta_added_ordinals: %s\ndelta_removed_identities: %s\n"+
			"delta_header_included: %t\nscope_object_count: %d\n",
		facts.BaseScopeIdentity, facts.BaseGeneration, facts.ObjectCount,
		formatChallengeOrdinals(facts.ChangedOrdinals), formatChallengeOrdinals(facts.AddedOrdinals), removed,
		facts.HeaderIncluded, facts.ScopeObjectCount,
	)
}

func (facts overviewDeltaFacts) chunkMetadata(metadata map[string]any) {
	metadata["delta_base_scope_identity"] = facts.BaseScopeIdentity
	metadata["delta_base_refresh_generation"] = facts.BaseGeneration
	metadata["delta_object_count"] = facts.ObjectCount
	metadata["delta_changed_ordinals"] = facts.ChangedOrdinals
	metadata["delta_added_ordinals"] = facts.AddedOrdinals
	metadata["delta_removed_identities"] = facts.RemovedIdentities
	metadata["delta_header_included"] = facts.HeaderIncluded
	metadata["scope_object_count"] = facts.ScopeObjectCount
}

// recordOverviewDeltaEvent writes the Ledger record of one delta delivery:
// the base it was computed from, the counts, and its chain.
func recordOverviewDeltaEvent(root string, enabled bool, indexSHA256, chainID, mode string, facts overviewDeltaFacts) {
	ledger.Append(root, enabled, ledger.Event{
		Op: "overview_delta_delivered", Source: ledger.SourceAgent, IndexSHA256: indexSHA256,
		DeliveryMode: mode, DeliveryChainID: chainID, EntryCount: facts.ObjectCount,
		PathsCount:    len(facts.ChangedOrdinals) + len(facts.AddedOrdinals) + len(facts.RemovedIdentities),
		Result:        fmt.Sprintf("base=%s changed=%d added=%d removed=%d", facts.BaseScopeIdentity, len(facts.ChangedOrdinals), len(facts.AddedOrdinals), len(facts.RemovedIdentities)),
		AOCIToolCalls: 1,
	})
}

// deltaSinceInputFault names the bad-argument reason of a delta_since request
// that cannot be served: with check_only, a probe, a cursor, a proof half, or
// a scope other than all it is a combination error, and the identity must be
// a scope_identity digest.
func deltaSinceInputFault(input overviewIn) string {
	scope := strings.ToLower(strings.TrimSpace(input.Scope))
	if input.CheckOnly || input.Probe || input.ProbeAnswers != nil || input.Cursor != "" ||
		input.HostConfirmation != nil || input.ModelAttestation != nil || (scope != "" && scope != cognition.ScopeAll) {
		return "overview_delta_since_invalid_combination"
	}
	if len(input.DeltaSince) != 64 || strings.Trim(strings.ToLower(input.DeltaSince), "0123456789abcdef") != "" {
		return "overview_delta_since_invalid"
	}
	return ""
}

// chunkedDeliveryMode reports whether a delivery mode is a chunk chain.
func chunkedDeliveryMode(mode string) bool {
	return mode == overviewDeliveryChunked || mode == overviewDeliveryChunkedDelta
}

// completeDeliveryMode reports whether a delivery mode carried a complete
// (non-delta) body: the reads the Ledger counts as a full Overview.
func completeDeliveryMode(mode string) bool {
	return mode == overviewDeliveryFull || mode == overviewDeliveryChunked
}
