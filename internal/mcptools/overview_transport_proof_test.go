package mcptools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// A Host confirmation echoes a hash the first Chunk prints, and an unchanged
// index has the hash it had before. After a compaction the reload used to be
// provable without being read: fetch Chunk 1, submit the previous delivery's
// proof again, and the answer was confirmed, complete, reliable. The
// confirmation counts only for a body this process sent to its end since the
// last fresh request, and a later Chunk replayed from the earlier chain does
// not finish the new one.
func TestHostConfirmationNeedsTheCurrentChainSentToItsEnd(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	deliver := func(input overviewIn, stopAfter int) (first map[string]any, cursors []string) {
		t.Helper()
		for sent := 1; ; sent++ {
			meta, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", input))
			if first == nil {
				first = meta
			}
			if meta["completed"].(bool) || sent == stopAfter {
				return first, cursors
			}
			cursors = append(cursors, meta["next_cursor"].(string))
			input = overviewIn{Cursor: meta["next_cursor"].(string)}
		}
	}
	first, cursors := deliver(overviewIn{Scope: cognition.ScopeAll}, 0)
	if len(cursors) < 2 {
		t.Fatalf("the fixture must take at least three Chunks, took %d", len(cursors)+1)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	_, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	report := completeAttestation(buildOverviewChallenge(view.ScopeIdentity, sequence), view.ObjectCount, volumeScopeBytes(view)/3)
	encoded, err := json.Marshal(map[string]any{
		"scope": cognition.ScopeAll,
		"host_delivery_confirmation": map[string]any{
			"version": overviewDeliveryReceiptV1, "body_sha256": first["body_sha256"],
			"body_bytes": first["body_utf8_bytes"], "end_marker_observed": true,
		},
		"model_cognition_attestation": attestationMap(t, report),
	})
	if err != nil {
		t.Fatal(err)
	}
	var proof overviewIn
	if err := json.Unmarshal(encoded, &proof); err != nil {
		t.Fatal(err)
	}
	prove := func() string {
		t.Helper()
		result := sessionOverview(t, root, session, "host", proof)
		if result.IsError {
			t.Fatalf("proof refused: %s", resText(t, result))
		}
		return resText(t, result)
	}
	if out := prove(); !strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) || !strings.Contains(out, "model_attestation: pass") {
		t.Fatalf("a delivery sent to its end must confirm:\n%s", out)
	}

	// The reload: one Chunk, then the same proof again.
	reload := overviewIn{Scope: cognition.ScopeAll, RefreshReasons: []string{"context_compaction"}, RefreshEventID: "compaction-replay"}
	again, _ := deliver(reload, 1)
	if again["body_sha256"] != first["body_sha256"] {
		t.Fatal("the reload must deliver the same body for this test to mean anything")
	}
	if out := prove(); !strings.Contains(out, "host_delivery_status: "+hostDeliveryIncomplete) ||
		strings.Contains(out, "model_full_cognition_reliable: true") || strings.Contains(out, "cognition_assimilation: complete") {
		t.Fatalf("the earlier proof vouched for a reload that sent one Chunk:\n%s", out)
	}
	// The earlier chain's last cursor still replays its bytes; it must not
	// finish the new chain, which is at Chunk 1.
	if result := sessionOverview(t, root, session, "host", overviewIn{Cursor: cursors[len(cursors)-1]}); result.IsError {
		t.Fatalf("an earlier cursor must stay replayable: %s", resText(t, result))
	}
	if out := prove(); !strings.Contains(out, "host_delivery_status: "+hostDeliveryIncomplete) {
		t.Fatalf("a replayed final Chunk of the earlier chain finished the new one:\n%s", out)
	}

	// Read to its end, the reload proves.
	deliver(reload, 0)
	if out := prove(); !strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) || !strings.Contains(out, "model_attestation: pass") {
		t.Fatalf("a reload sent to its end must confirm:\n%s", out)
	}
}
