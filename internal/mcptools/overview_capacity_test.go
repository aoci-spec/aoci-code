package mcptools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func compactionReload(scope, event string) overviewIn {
	return overviewIn{Scope: scope, RefreshReasons: []string{machinecontract.RefreshReasonContextCompaction}, RefreshEventID: event}
}

func capacityStopFacts(t *testing.T, text string) map[string]string {
	t.Helper()
	if strings.Contains(text, "<<<AOCI_OVERVIEW_BODY_BEGIN") || strings.Contains(text, overviewChunkBodyMarker) {
		t.Fatalf("a capacity stop must carry no body:\n%s", text)
	}
	facts := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ": "); ok {
			facts[key] = value
		}
	}
	return facts
}

func readLedgerOps(t *testing.T, root string) []map[string]any {
	t.Helper()
	file, err := os.Open(filepath.Join(root, ".aoci", "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

// The #114 loop: a compaction reload that is itself compacted before any
// attested delivery consumes it proves the Whole-Index does not fit. The next
// reload answers the capacity stop instead of another chain, every later one
// does too, and nothing else the Host can hold is withheld.
func TestCompactionReloadInterruptedByCompactionAnswersTheCapacityStop(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()

	first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
	if first["continuation_required"] != true || int(first["chunk_count"].(float64)) < 2 {
		t.Fatalf("the fixture must chunk: %+v", first)
	}
	failedTokens := int(first["estimated_tokens"].(float64))

	stop := capacityStopFacts(t, resText(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2"))))
	for key, want := range map[string]string{
		"delivery_mode":                    machinecontract.OverviewDeliveryModeCapacityStop,
		"fallback_reason":                  machinecontract.OverviewFallbackHostContextCapacityExceeded,
		"completed":                        "false",
		"continuation_required":            "false",
		"full_text_included":               "false",
		"refresh_status":                   machinecontract.RefreshStatusNotRequired,
		"cognition_state":                  cognitionStateUncertain,
		"model_full_cognition_reliable":    "false",
		"delivery_guidance":                machinecontract.OverviewDeliveryGuidanceSourceBound,
		"capacity_interruptions":           "1",
		"capacity_failed_estimated_tokens": fmt.Sprint(failedTokens),
		"interrupted_chunks_served":        "1",
		"chunk_count":                      fmt.Sprint(int(first["chunk_count"].(float64))),
	} {
		if stop[key] != want {
			t.Fatalf("stop %s = %q, want %q\n%v", key, stop[key], want, stop)
		}
	}
	if !strings.HasPrefix(stop["interrupted_delivery_chain_id"], "chain-") || stop["next_action"] == "" {
		t.Fatalf("stop must name the interrupted chain and the next action: %v", stop)
	}

	// check_only now reports the plan and the verdict before any transfer.
	var checkpoint overviewCheckpointEnvelope
	if err := json.Unmarshal([]byte(resText(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", CheckOnly: true}))), &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.DeliveryPlan == nil || checkpoint.DeliveryPlan.ChunkCount < 2 || checkpoint.DeliveryPlan.EstimatedTokens != failedTokens ||
		checkpoint.HostCapacity == nil || !checkpoint.HostCapacity.Verdict ||
		checkpoint.HostCapacity.CompactionReload != machinecontract.OverviewCompactionReloadCapacityStop {
		t.Fatalf("check_only plan or capacity: plan=%+v capacity=%+v", checkpoint.DeliveryPlan, checkpoint.HostCapacity)
	}

	// Every later compaction reload stops at once; the stop opened no attempt,
	// so the interruption count stays where the evidence put it.
	again := capacityStopFacts(t, resText(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-3"))))
	if again["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop || again["capacity_interruptions"] != "1" {
		t.Fatalf("a later reload must stop without a new interruption: %v", again)
	}

	// An explicit Overview with no compaction pending still delivers in full.
	explicit, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all"}))
	if int(explicit["chunk_index"].(float64)) != 1 {
		t.Fatalf("an explicit request must start the chain: %+v", explicit)
	}

	// A smaller scope still fits, and still delivers; being a scoped read, it
	// opens no reload attempt, so it can never lower the verdict.
	meta := resText(t, sessionOverview(t, root, session, "host", compactionReload("meta", "compact-4")))
	if strings.Contains(meta, "delivery_mode: "+machinecontract.OverviewDeliveryModeCapacityStop) || !strings.Contains(meta, "<<<AOCI_OVERVIEW_BODY_BEGIN/v1 scope=meta>>>") {
		t.Fatalf("a scope below the verdict must deliver:\n%s", meta)
	}
	if session.compactionAttempt != nil {
		t.Fatalf("a scoped delivery opened a reload attempt: %+v", session.compactionAttempt)
	}
	// compact-4 is still pending, so the complete scope stops again, and the
	// interruption count has not moved.
	pending := capacityStopFacts(t, resText(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all"})))
	if pending["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop || pending["capacity_interruptions"] != "1" {
		t.Fatalf("a pending compaction over the verdict must stop: %v", pending)
	}

	var sawChunk, sawInterruption, sawStop bool
	for _, event := range readLedgerOps(t, root) {
		switch event["op"] {
		case "overview":
			if event["delivery_chain_id"] != nil && event["chunk_index"] != nil && event["delivery_completed"] != nil {
				sawChunk = true
			}
		case "overview_delivery_interrupted":
			sawInterruption = event["delivery_chain_id"] == stop["interrupted_delivery_chain_id"] && event["chunk_index"] == float64(1)
		case "overview_capacity_stop":
			sawStop = event["delivery_mode"] == machinecontract.OverviewDeliveryModeCapacityStop
		}
	}
	if !sawChunk || !sawInterruption || !sawStop {
		t.Fatalf("ledger chain facts: chunk=%v interruption=%v stop=%v", sawChunk, sawInterruption, sawStop)
	}
}

// Only a reload that started under a declared compaction can prove the
// verdict: the session's first delivery being compacted proves nothing about
// a fresh context, and a reload that an attested delivery consumed is over.
func TestOnlyAnInterruptedCompactionReloadIsAVerdict(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()

	chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all"}))
	reload, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
	if int(reload["chunk_index"].(float64)) != 1 {
		t.Fatalf("compacting the first delivery must not stop the reload: %+v", reload)
	}

	// The reload's events are consumed (as a confirmed, attested delivery
	// would consume them), so the next compaction starts clean.
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail)
	}
	view, err := loaded.set.Scope("all")
	if err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	session.markDeliveryAttempt(receiptWithState(session.deliveredReceipt(newVolumeCognitionReceipt(root, "session-test", loaded.set, view), ""), cognitionStateValid, true))
	session.mu.Unlock()
	next, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2")))
	if int(next["chunk_index"].(float64)) != 1 {
		t.Fatalf("a consumed reload is not an interruption: %+v", next)
	}

	// The same event declared again is not a new compaction.
	repeat, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2")))
	if int(repeat["chunk_index"].(float64)) != 1 || session.capacity != nil {
		t.Fatalf("a repeated event id must not count: %+v capacity=%+v", repeat, session.capacity)
	}
}

// A proof half for the reload's body closes the attempt: the Host held every
// chunk, so the next compaction is ordinary and its reload delivers.
func TestProofForTheReloadBodyClosesTheAttempt(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
	cursor, _ := first["next_cursor"].(string)
	for cursor != "" {
		next, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", Cursor: cursor}))
		cursor, _ = next["next_cursor"].(string)
	}
	confirmation := &overviewHostConfirmation{
		Version: overviewDeliveryReceiptV1, BodySHA256: first["body_sha256"].(string),
		BodyBytes: int(first["body_utf8_bytes"].(float64)), EndMarkerObserved: true,
	}
	proof := resText(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", HostConfirmation: confirmation}))
	if !strings.Contains(proof, "host_delivery_status: host_delivery_confirmed") {
		t.Fatalf("confirmation not accepted:\n%.600s", proof)
	}
	if session.compactionAttempt != nil {
		t.Fatalf("a proof half left the attempt open: %+v", session.compactionAttempt)
	}
	reload, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2")))
	if int(reload["chunk_index"].(float64)) != 1 || session.capacity != nil {
		t.Fatalf("a proven transport is not an interruption: %+v capacity=%+v", reload, session.capacity)
	}
}

// The stop answers fresh requests only: continuing a chain the Host already
// holds a cursor for, or submitting proof for it, is served as before.
func TestCapacityStopAnswersOnlyFreshRequests(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
	capacityStopFacts(t, resText(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2"))))
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("first chunk has no cursor")
	}
	second, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", Cursor: cursor}))
	if int(second["chunk_index"].(float64)) != 2 {
		t.Fatalf("a held cursor must continue its chain: %+v", second)
	}
}

func TestCompactionInvalidatesEarlierProofAndDeltaBase(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprintf("capacity_stop_%t", stop), func(t *testing.T) {
			root, _ := deltaVolumesFixture(t, 10)
			fixture := transportProofFixtureForRoot(t, root)
			const owner = "compacted-host"
			fixture.prove(owner, overviewIn{})
			proof := fixture.prove(owner, fixture.both)
			if overviewMetadataValue(t, proof, "model_full_cognition_reliable") != "true" {
				t.Fatal("fixture must first establish a reliable delivery")
			}
			if stop {
				fixture.prove(owner, compactionReload("all", "compact-1"))
				capacity := capacityStopFacts(t, fixture.prove(owner, compactionReload("all", "compact-2")))
				if capacity["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop {
					t.Fatalf("fixture must reach the stop: %v", capacity)
				}
				checkpoint := checkpointV2ForTest(t, root, fixture.session, owner, overviewIn{})
				assertCheckpointSourceBound(t, checkpoint.Assessment)
			} else {
				checkpointV2ForTest(t, root, fixture.session, owner, compactionReload("all", "compact-1"))
			}

			generation := fixture.session.generation
			compactionPending := fixture.session.compactionPending()
			old := fixture.both
			old.CognitionStateVersion = machinecontract.CognitionStateV2
			late := fixture.prove(owner, old)
			if overviewMetadataValue(t, late, "host_delivery_status") != hostDeliveryIncomplete ||
				cognitionStateV2MetadataForTest(t, late).CurrentSystemCognitionReliable ||
				fixture.session.generation != generation || fixture.session.compactionPending() != compactionPending {
				t.Errorf("old confirmation changed the host status or consumed compaction: host=%s generation=%d pending=%t",
					overviewMetadataValue(t, late, "host_delivery_status"), fixture.session.generation, fixture.session.compactionPending())
			}
			// A probe at the current identity cannot revive the old anchor.
			fixture.session.noteProbePass(volumeDeliveryOwner{direct: owner}, fixture.identity, fixture.session.generation)
			reload := fixture.prove(owner, overviewIn{DeltaSince: fixture.identity})
			if overviewMetadataValue(t, reload, "delivery_mode") != overviewDeliveryFull ||
				overviewMetadataValue(t, reload, "fallback_reason") != machinecontract.OverviewFallbackDeltaBaseUnknown {
				t.Errorf("the old anchor must fall back to full/unknown: mode=%s reason=%s",
					overviewMetadataValue(t, reload, "delivery_mode"), overviewMetadataValue(t, reload, "fallback_reason"))
				return
			}
			checkpoint := checkpointV2ForTest(t, root, fixture.session, owner, overviewIn{})
			if checkpoint.CognitionStateV2.CurrentSystemCognitionReliable {
				t.Fatal("the fresh body alone cannot restore reliability")
			}
			newProof := fixture.prove(owner, old)
			if overviewMetadataValue(t, newProof, "host_delivery_status") != hostDeliveryConfirmed ||
				!cognitionStateV2MetadataForTest(t, newProof).CurrentSystemCognitionReliable ||
				!checkpointV2ForTest(t, root, fixture.session, owner, overviewIn{}).CognitionStateV2.CurrentSystemCognitionReliable {
				t.Fatal("fresh full delivery and proof must restore reliability")
			}
		})
	}
}

func TestOnlyTheCompactedHostsFullProofCanFinishRefresh(t *testing.T) {
	root, _ := deltaVolumesFixture(t, 10)
	full := transportProofFixtureForRoot(t, root)
	scoped := transportProofFixtureForScope(t, root, "code")
	scoped.session = full.session
	const owner, other, event = "owner", "other-host", "same-event-id"
	for _, host := range []string{owner, other} {
		full.prove(host, overviewIn{})
		full.prove(host, full.both)
	}
	checkpointV2ForTest(t, root, full.session, owner, compactionReload("all", event))
	scoped.prove(owner, overviewIn{Scope: "code"})
	local := scoped.prove(owner, scoped.both)
	if overviewMetadataValue(t, local, "host_delivery_status") != hostDeliveryConfirmed || !full.session.compactionPending() {
		t.Fatal("the scoped proof must succeed for its own body and leave compaction pending")
	}
	full.prove(other, full.both)
	if !full.session.compactionPending() || !checkpointV2ForTest(t, root, full.session, other, overviewIn{}).CognitionStateV2.CurrentSystemCognitionReliable {
		t.Fatal("another Host must keep its own proof without consuming the pending compaction")
	}
	checkpointV2ForTest(t, root, full.session, other, compactionReload("all", event))
	late := full.prove(other, full.both)
	if overviewMetadataValue(t, late, "host_delivery_status") != hostDeliveryIncomplete {
		t.Fatalf("the same event ID from a different Host must invalidate that Host's proof: %s", late)
	}
}

// The Legacy layout serves Overview too, and loops the same way.
func TestLegacyCompactionReloadLoopStops(t *testing.T) {
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.OverviewDelivery.ChunkTokens = machinecontract.OverviewChunkTokensMin
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	var index strings.Builder
	index.WriteString("#====Legacy capacity fixture====\n#Locale: zh-CN\n===" + filepath.ToSlash(root) + "/===\n")
	for ordinal := 1; ordinal <= 400; ordinal++ {
		name := fmt.Sprintf("file-%04d.go", ordinal)
		if err := os.WriteFile(filepath.Join(root, name), []byte("package demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&index, "%s[XUT5T]: F:preserve legacy object %04d across a capacity stop | R:- | A:- | S:-\n", name, ordinal)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(index.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	client := connectMCPClient(t, root)
	first := callVolumeTool(t, client, "aoci_overview", map[string]any{"refresh_reasons": []string{"context_compaction"}, "refresh_event_id": "legacy-1"})
	if !strings.Contains(first, `"continuation_required":true`) {
		t.Fatalf("the Legacy fixture must chunk: %.300s", first)
	}
	stop := capacityStopFacts(t, callVolumeTool(t, client, "aoci_overview", map[string]any{"refresh_reasons": []string{"context_compaction"}, "refresh_event_id": "legacy-2"}))
	if stop["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop || stop["layout_mode"] != "legacy-monolithic" {
		t.Fatalf("Legacy stop: %v", stop)
	}
}

// The verdict describes capacity, and the proof against it is the Host
// confirming a complete delivery at least that large. A reload the Host
// stopped for its own reasons (a user interrupt, a cost cap, an unreachable
// model) is an interruption too, and before #168 the verdict it left outlived
// every later proof, keeping a long-lived Host source-bound for the rest of
// the server session.
func TestConfirmedCompleteDeliveryClearsTheVerdict(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	sessionOverview(t, root, session, "host", compactionReload("all", "compact-1"))
	capacityStopFacts(t, resText(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2"))))
	if session.capacity == nil {
		t.Fatal("the interrupted reload must have left a verdict")
	}
	first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all"}))
	cursor, _ := first["next_cursor"].(string)
	for cursor != "" {
		next, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", Cursor: cursor}))
		cursor, _ = next["next_cursor"].(string)
	}
	if session.capacity == nil {
		t.Fatal("an unconfirmed delivery must not clear the verdict")
	}
	confirmation := &overviewHostConfirmation{
		Version: overviewDeliveryReceiptV1, BodySHA256: first["body_sha256"].(string),
		BodyBytes: int(first["body_utf8_bytes"].(float64)), EndMarkerObserved: true,
	}
	proof := resText(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", HostConfirmation: confirmation}))
	if !strings.Contains(proof, "host_delivery_status: host_delivery_confirmed") {
		t.Fatalf("confirmation not accepted:\n%.600s", proof)
	}
	if session.capacity != nil {
		t.Fatalf("a confirmed complete delivery must clear the verdict: %+v", session.capacity)
	}
	reload, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-3")))
	if int(reload["chunk_index"].(float64)) != 1 {
		t.Fatalf("after the proof a compaction reload must deliver again: %+v", reload)
	}
}
