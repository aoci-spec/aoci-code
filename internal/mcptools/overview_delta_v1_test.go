package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Locks: the delta is the comparison of two digest sequences: a changed line
// is changed, an identity the base lacks is added, an identity the current
// sequence lacks is removed, in sequence order; the share bound counts the
// changed and added objects against the current count.
func TestComputeOverviewDeltaAndShare(t *testing.T) {
	base := &overviewDeltaBase{Objects: []deltaObjectDigest{{"code:a.go", "1"}, {"code:b.go", "2"}, {"code:c.go", "3"}, {"code:d.go", "4"}}}
	current := &overviewDeltaBase{Objects: []deltaObjectDigest{{"code:a.go", "1"}, {"code:b.go", "22"}, {"code:e.go", "5"}, {"code:d.go", "4"}}}
	plan := computeOverviewDelta(base, current)
	if !plan.changed["code:b.go"] || len(plan.changed) != 1 || !plan.added["code:e.go"] || len(plan.added) != 1 ||
		strings.Join(plan.removed, ",") != "code:c.go" || plan.deltaCount() != 2 {
		t.Fatalf("delta = %+v", plan)
	}
	if plan.exceedsShare(4) {
		t.Fatal("2 of 4 is within the 50 percent share")
	}
	if !plan.exceedsShare(3) {
		t.Fatal("2 of 3 is past the 50 percent share")
	}
	if same := computeOverviewDelta(base, base); same.deltaCount() != 0 || len(same.removed) != 0 {
		t.Fatalf("an unchanged sequence has no delta: %+v", same)
	}
}

// Locks: the delta Challenge draws its ordinals from the delivered objects
// only, reported at their current ordinals, while the identity facts the
// Attestation must echo stay the complete sequence's.
func TestDeltaChallengeBindsTheCurrentSequenceAndDeltaOrdinals(t *testing.T) {
	full := make([]overviewChallengeTarget, 0, 30)
	for index := 0; index < 30; index++ {
		full = append(full, overviewChallengeTarget{Ordinal: index + 1, ObjectIdentity: fmt.Sprintf("code:f%02d.go", index), Tag: "CD5T", CoreF: fmt.Sprintf("unit %02d", index)})
	}
	delta := []overviewChallengeTarget{full[4], full[11], full[27]}
	challenge := buildDeltaChallenge(strings.Repeat("a", 64), full, delta)
	if challenge.EntryCount != 30 || challenge.EntrySequenceSHA256 != overviewEntrySequenceSHA256(full) || challenge.IndexSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("the Challenge echoes the complete sequence facts: %+v", challenge)
	}
	if fmt.Sprint(challenge.Ordinals) != "[5 12 28]" {
		t.Fatalf("ordinals are the delivered objects' current ordinals: %v", challenge.Ordinals)
	}
	for _, ordinal := range challenge.Ordinals {
		if challenge.Targets[ordinal].ObjectIdentity != full[ordinal-1].ObjectIdentity {
			t.Fatalf("target %d is not the current object", ordinal)
		}
	}
	report := completeAttestation(challenge, 3, 100)
	if result := assessOverviewAttestation(challenge, 3, 100, hostDeliveryConfirmed, true, report); result.ModelAttestation != modelAttestationPass {
		t.Fatalf("answers at the current ordinals pass: %+v", result)
	}
}

// Locks: delta_since is refused with check_only, a probe, a cursor, a proof
// half, or a scope other than all, and must be a scope identity digest.
func TestDeltaSinceInputFaults(t *testing.T) {
	identity := strings.Repeat("b", 64)
	for name, input := range map[string]overviewIn{
		"check_only": {DeltaSince: identity, CheckOnly: true},
		"probe":      {DeltaSince: identity, CheckOnly: true, Probe: true},
		"cursor":     {DeltaSince: identity, Cursor: "v2:x"},
		"proof":      {DeltaSince: identity, HostConfirmation: &overviewHostConfirmation{}},
		"scope":      {DeltaSince: identity, Scope: "code"},
	} {
		if deltaSinceInputFault(input) != "overview_delta_since_invalid_combination" {
			t.Fatalf("%s: combination accepted", name)
		}
	}
	if deltaSinceInputFault(overviewIn{DeltaSince: "short"}) != "overview_delta_since_invalid" {
		t.Fatal("a malformed identity accepted")
	}
	if deltaSinceInputFault(overviewIn{DeltaSince: identity, Scope: "all"}) != "" {
		t.Fatal("a well-formed all-scope request refused")
	}
	root := buildRepo(t)
	session := connectMCPClient(t, root)
	if out := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": identity}); !strings.Contains(out, "overview_delta_requires_volumes_v1") {
		t.Fatalf("a Legacy layout served a delta: %s", out[:200])
	}
}

// deltaVolumesFixture builds a small aligned Volumes repository whose complete
// Overview fits one response, so every delivery in the flow is full-form.
func deltaVolumesFixture(t *testing.T, files int) (string, *mcp.ClientSession) {
	t.Helper()
	root := buildManyFileVolumesRepo(t, files)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OverviewDelivery.ChunkTokens = machinecontract.OverviewChunkTokensMax
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	alignVolumes(t, session, fixtureEntryLine)
	return root, session
}

// alignVolumes authors every issued batch with lineFor until Maintain reports
// aligned.
func alignVolumes(t *testing.T, session *mcp.ClientSession, lineFor func(string) string) {
	t.Helper()
	for round := 0; round < 20; round++ {
		response := maintainJSON(t, session, map[string]any{})
		if response["status"] == autoStatusApplied && response["aligned"] == true {
			return
		}
		if response["status"] != autoStatusRepairRequired {
			t.Fatalf("maintain did not issue a batch: status=%v next=%v", response["status"], response["next_action"])
		}
		plan := codePlanOf(t, response)
		entries := []map[string]any{}
		for _, candidate := range response["candidates"].([]any) {
			item := candidate.(map[string]any)
			entries = append(entries, map[string]any{"path": item["path"], "source_sha256": item["source_sha256"],
				"candidate_id": item["candidate_id"], "new_entry": lineFor(item["path"].(string))})
		}
		applied := decodeMaintainJSON(t, callVolumeTool(t, session, "aoci_update_entry", map[string]any{"code_batch_id": plan["batch_id"], "entries": entries}))
		if applied["status"] != autoStatusApplied {
			t.Fatalf("apply failed: %v", applied)
		}
	}
	t.Fatal("the fixture did not align")
}

func currentVolumeSequence(t *testing.T, root string) []overviewChallengeTarget {
	t.Helper()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope("all")
	if err != nil {
		t.Fatal(err)
	}
	_, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}

func parseOrdinalList(value string) []int {
	ordinals := []int{}
	for _, part := range strings.Split(value, ",") {
		if number, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			ordinals = append(ordinals, number)
		}
	}
	return ordinals
}

func metadataInt(t *testing.T, output, key string) int {
	t.Helper()
	value, err := strconv.Atoi(overviewMetadataValue(t, output, key))
	if err != nil {
		t.Fatalf("%s is not an integer in %q", key, overviewMetadataValue(t, output, key))
	}
	return value
}

// attestDelivered submits both proof halves for a full-form delivery, answering
// the Challenge from the current in-process sequence, with wrong core F answers
// when wrong is set; it returns the proof response.
func attestDelivered(t *testing.T, session *mcp.ClientSession, root, delivered string, wrong bool) string {
	t.Helper()
	sequence := currentVolumeSequence(t, root)
	answers := []map[string]any{}
	for _, ordinal := range parseOrdinalList(overviewMetadataValue(t, delivered, "challenge_ordinals")) {
		target := sequence[ordinal-1]
		coreF := target.CoreF
		if wrong {
			coreF = "something entirely different and unrelated"
		}
		answers = append(answers, map[string]any{"ordinal": ordinal, "object_identity": target.ObjectIdentity, "tag": target.Tag, "core_f": coreF})
	}
	attestation := map[string]any{
		"version":                   modelCognitionAttestationV1,
		"index_sha256":              overviewMetadataValue(t, delivered, "challenge_index_sha256"),
		"entry_sequence_sha256":     overviewMetadataValue(t, delivered, "challenge_entry_sequence_sha256"),
		"entry_count":               metadataInt(t, delivered, "challenge_entry_count"),
		"challenge_digest":          overviewMetadataValue(t, delivered, "challenge_digest"),
		"reported_entry_count":      metadataInt(t, delivered, "entry_count"),
		"reported_estimated_tokens": metadataInt(t, delivered, "estimated_tokens"),
		"coverage_percent":          100, "system_mastery_percent": 90, "confidence_percent": 90,
		"truncation_detected": false, "unseen_sections": []string{}, "uncertainty_reasons": []string{},
		"challenge_answers": answers,
	}
	confirmation := map[string]any{
		"version": overviewDeliveryReceiptV1, "body_sha256": overviewMetadataValue(t, delivered, "body_sha256"),
		"body_bytes": metadataInt(t, delivered, "body_utf8_bytes"), "end_marker_observed": true,
	}
	return callVolumeTool(t, session, "aoci_overview", map[string]any{"host_delivery_confirmation": confirmation, "model_cognition_attestation": attestation})
}

// passProbe asks for the two-question probe and answers it from the current
// in-process sequence.
func passProbe(t *testing.T, session *mcp.ClientSession, root string) {
	t.Helper()
	var issued struct {
		Probe *cognitionProbe `json:"cognition_probe"`
	}
	if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_overview", map[string]any{"check_only": true, "probe": true})), &issued); err != nil || issued.Probe == nil {
		t.Fatalf("probe not issued: %v", err)
	}
	sequence := currentVolumeSequence(t, root)
	answers := []map[string]any{}
	for _, ordinal := range issued.Probe.Ordinals {
		target := sequence[ordinal-1]
		answers = append(answers, map[string]any{"ordinal": ordinal, "object_identity": target.ObjectIdentity, "tag": target.Tag, "core_f": target.CoreF})
	}
	var graded struct {
		Result *cognitionProbeResult `json:"probe_result"`
	}
	raw := callVolumeTool(t, session, "aoci_overview", map[string]any{"check_only": true,
		"probe_answers": map[string]any{"version": cognitionProbeV1, "digest": issued.Probe.Digest, "answers": answers}})
	if err := json.Unmarshal([]byte(raw), &graded); err != nil || graded.Result == nil || graded.Result.Result != probeResultPass {
		t.Fatalf("probe did not pass: %s", raw[:200])
	}
}

// reviseSources rewrites the named fixture files (and creates the ones that
// do not exist), dated out of the fingerprint-cache window, then aligns the
// index with lineFor.
func reviseSources(t *testing.T, session *mcp.ClientSession, root string, lineFor func(string) string, rels ...string) {
	t.Helper()
	when := time.Now().Add(-10 * time.Second)
	for index, rel := range rels {
		path := filepath.Join(root, filepath.FromSlash(rel))
		body := fmt.Sprintf("package mod000\n\n// revised %d at %d\nfunc Revised%d() int { return %d }\n", index, when.UnixNano(), index, index)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	alignVolumes(t, session, lineFor)
}

func ordinalOf(t *testing.T, sequence []overviewChallengeTarget, identity string) int {
	t.Helper()
	for index, target := range sequence {
		if target.ObjectIdentity == identity {
			return index + 1
		}
	}
	t.Fatalf("%s is not in the sequence", identity)
	return 0
}

// Locks the L5 contract end to end on one Host session: a delta is served only
// after a passing complete Attestation and a probe pass over the current
// identity; it carries exactly the changed and added Entries at their current
// ordinals and nothing else; its Attestation passes against the complete
// identity facts and re-anchors the base; a second delta covers only the newer
// writes; an empty delta re-anchors without a proof; a partial delta
// Attestation drops the base; a pending compaction and a delta past half the
// objects answer the complete scope with the named reason; and the
// combination faults are refused.
func TestOverviewDeltaDeliversOnlyWhatChangedSinceTheAttestedBase(t *testing.T) {
	root, session := deltaVolumesFixture(t, 10)
	revised := func(path string) string {
		base := filepath.Base(path)
		return fmt.Sprintf("%s[CD5T]: F:Provides the revised fixture unit %s | R:- | A:- | S:-", base, strings.TrimSuffix(base, ".go"))
	}

	full := callVolumeTool(t, session, "aoci_overview", map[string]any{})
	if overviewMetadataValue(t, full, "delivery_mode") != overviewDeliveryFull || overviewMetadataValue(t, full, "full_text_included") != "true" {
		t.Fatalf("the fixture must deliver full-form: %s", full[:300])
	}
	base := overviewMetadataValue(t, full, "index_sha256")
	if proof := attestDelivered(t, session, root, full, false); !strings.Contains(proof, "model_attestation: pass") || !strings.Contains(proof, "model_full_cognition_reliable: true") {
		t.Fatalf("the complete Attestation did not pass: %s", proof[:400])
	}

	// Without a probe pass the request answers the complete scope.
	noProbe := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": base})
	if overviewMetadataValue(t, noProbe, "delivery_mode") != overviewDeliveryFull || overviewMetadataValue(t, noProbe, "fallback_reason") != machinecontract.OverviewFallbackDeltaProbeRequired {
		t.Fatalf("no probe pass must fall back to the complete scope: mode=%s reason=%s", overviewMetadataValue(t, noProbe, "delivery_mode"), overviewMetadataValue(t, noProbe, "fallback_reason"))
	}

	// Nothing changed: an empty delta, no body, no Challenge, no proof.
	passProbe(t, session, root)
	empty := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": base})
	if overviewMetadataValue(t, empty, "delivery_mode") != overviewDeliveryDelta || overviewMetadataValue(t, empty, "delta_object_count") != "0" ||
		overviewMetadataValue(t, empty, "completed") != "true" || strings.Contains(empty, "challenge_digest") || strings.Contains(empty, "<<<AOCI_OVERVIEW_BODY_BEGIN") ||
		overviewMetadataValue(t, empty, "model_full_cognition_reliable") != "true" {
		t.Fatalf("an unchanged base answers an empty delta: %s", empty[:400])
	}

	// One Entry revised and one added: the delta is exactly those two.
	reviseSources(t, session, root, revised, "pkg/mod000/file000.go", "pkg/mod000/file010.go")
	sequence := currentVolumeSequence(t, root)
	changedOrdinal := ordinalOf(t, sequence, "code:pkg/mod000/file000.go")
	addedOrdinal := ordinalOf(t, sequence, "code:pkg/mod000/file010.go")
	passProbe(t, session, root)
	delta := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": base})
	if overviewMetadataValue(t, delta, "delivery_mode") != overviewDeliveryDelta || overviewMetadataValue(t, delta, "full_text_included") != "true" {
		t.Fatalf("the delta was not served: %s", delta[:400])
	}
	if overviewMetadataValue(t, delta, "delta_object_count") != "2" || metadataInt(t, delta, "entry_count") != 2 ||
		overviewMetadataValue(t, delta, "delta_changed_ordinals") != strconv.Itoa(changedOrdinal) ||
		overviewMetadataValue(t, delta, "delta_added_ordinals") != strconv.Itoa(addedOrdinal) ||
		overviewMetadataValue(t, delta, "delta_removed_identities") != "[]" ||
		metadataInt(t, delta, "scope_object_count") != len(sequence) || metadataInt(t, delta, "challenge_entry_count") != len(sequence) ||
		overviewMetadataValue(t, delta, "delta_base_scope_identity") != base {
		t.Fatalf("delta facts are wrong: %s", delta[:900])
	}
	body := delta[strings.Index(delta, "<<<AOCI_OVERVIEW_BODY_BEGIN"):]
	if !strings.Contains(body, "file000.go[CD5T]: F:Provides the revised fixture unit file000") || !strings.Contains(body, "file010.go[CD5T]") {
		t.Fatalf("the delta body misses a delivered Entry:\n%s", body)
	}
	if strings.Contains(body, "file001.go[") || strings.Contains(body, "file005.go[") {
		t.Fatalf("the delta body carries an unchanged Entry:\n%s", body)
	}
	for _, ordinal := range parseOrdinalList(overviewMetadataValue(t, delta, "challenge_ordinals")) {
		if ordinal != changedOrdinal && ordinal != addedOrdinal {
			t.Fatalf("the Challenge drew ordinal %d outside the delta", ordinal)
		}
	}
	afterFirst := overviewMetadataValue(t, delta, "index_sha256")
	if proof := attestDelivered(t, session, root, delta, false); !strings.Contains(proof, "model_attestation: pass") || !strings.Contains(proof, "model_full_cognition_reliable: true") || !strings.Contains(proof, "delivery_mode: attestation") {
		t.Fatalf("the delta Attestation did not pass: %s", proof[:500])
	}

	// A second write after the re-anchor: the next delta covers only it.
	reviseSources(t, session, root, revised, "pkg/mod000/file001.go")
	sequence = currentVolumeSequence(t, root)
	passProbe(t, session, root)
	second := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": afterFirst})
	if overviewMetadataValue(t, second, "delivery_mode") != overviewDeliveryDelta || overviewMetadataValue(t, second, "delta_object_count") != "1" ||
		overviewMetadataValue(t, second, "delta_changed_ordinals") != strconv.Itoa(ordinalOf(t, sequence, "code:pkg/mod000/file001.go")) {
		t.Fatalf("the second delta is not computed from the new base: %s", second[:600])
	}
	// The old base identity is no longer the base.
	stale := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": base})
	if overviewMetadataValue(t, stale, "fallback_reason") != machinecontract.OverviewFallbackDeltaBaseUnknown {
		t.Fatalf("a superseded base must read unknown: %s", overviewMetadataValue(t, stale, "fallback_reason"))
	}
	// A partial delta Attestation drops the base.
	passProbe(t, session, root)
	second = callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": afterFirst})
	if proof := attestDelivered(t, session, root, second, true); !strings.Contains(proof, "model_attestation: partial") && !strings.Contains(proof, "model_attestation: fail") {
		t.Fatalf("wrong answers must not pass: %s", proof[:400])
	}
	passProbe(t, session, root)
	dropped := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": afterFirst})
	if overviewMetadataValue(t, dropped, "delivery_mode") != overviewDeliveryFull || overviewMetadataValue(t, dropped, "fallback_reason") != machinecontract.OverviewFallbackDeltaBaseUnknown {
		t.Fatalf("a partial delta Attestation must drop the base: %s", dropped[:300])
	}

	// Re-establish, then a pending compaction takes the complete reload.
	if proof := attestDelivered(t, session, root, dropped, false); !strings.Contains(proof, "model_attestation: pass") {
		t.Fatalf("re-establishing the base failed: %s", proof[:300])
	}
	current := overviewMetadataValue(t, dropped, "index_sha256")
	callVolumeTool(t, session, "aoci_overview", map[string]any{"check_only": true, "refresh_reasons": []string{"context_compaction"}, "refresh_event_id": "delta-compaction-1"})
	compaction := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": current})
	if overviewMetadataValue(t, compaction, "fallback_reason") != machinecontract.OverviewFallbackDeltaCompactionPending {
		t.Fatalf("a pending compaction must take the complete reload: %s", overviewMetadataValue(t, compaction, "fallback_reason"))
	}
	if proof := attestDelivered(t, session, root, compaction, false); !strings.Contains(proof, "model_attestation: pass") {
		t.Fatalf("the compaction reload did not attest: %s", proof[:300])
	}
	current = overviewMetadataValue(t, compaction, "index_sha256")

	// Past half the objects the complete body is the answer.
	reviseSources(t, session, root, revised, "pkg/mod000/file002.go", "pkg/mod000/file003.go", "pkg/mod000/file004.go",
		"pkg/mod000/file005.go", "pkg/mod000/file006.go", "pkg/mod000/file007.go", "pkg/mod000/file008.go")
	passProbe(t, session, root)
	share := callVolumeTool(t, session, "aoci_overview", map[string]any{"delta_since": current})
	if overviewMetadataValue(t, share, "delivery_mode") != overviewDeliveryFull || overviewMetadataValue(t, share, "fallback_reason") != machinecontract.OverviewFallbackDeltaExceedsShare {
		t.Fatalf("a delta past the share must answer the complete scope: mode=%s reason=%s", overviewMetadataValue(t, share, "delivery_mode"), overviewMetadataValue(t, share, "fallback_reason"))
	}

	// Combination faults are refused before anything is read.
	for name, arguments := range map[string]map[string]any{
		"check_only": {"delta_since": current, "check_only": true},
		"cursor":     {"delta_since": current, "cursor": "v2:deadbeef"},
		"scope":      {"delta_since": current, "scope": "code"},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || !strings.Contains(resText(t, result), "overview_delta_since_invalid_combination") {
			t.Fatalf("%s: the combination was served: %s", name, resText(t, result)[:200])
		}
	}
}
