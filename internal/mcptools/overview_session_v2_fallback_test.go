package mcptools

import (
	"encoding/json"
	"strings"
	"testing"
)

// A delivery the store cannot retain is not an error for the Host: the first
// request is served the original live way with a reconstructable v1 cursor,
// and continuation re-runs governance as it did before the reuse existed.
func TestOverviewV2CapacityFallsBackToLiveDelivery(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	session.deliveries.maxBytes = 1
	first := sessionOverview(t, root, session, "host", overviewIn{})
	meta, _ := chunkMetadata(t, first)
	if work := workCounters(t, first); work != (overviewWork{GovernanceEvals: 2, BodyBuilds: 1}) {
		t.Fatalf("capacity fallback did not take the live path: %+v", work)
	}
	cursor, _ := meta["next_cursor"].(string)
	if cursor == "" || strings.HasPrefix(cursor, "v2:") {
		t.Fatalf("capacity fallback must hand out a v1 cursor: %q", cursor)
	}
	if len(session.deliveries.entries) != 0 {
		t.Fatal("an unretained delivery stayed in the store")
	}
	second := sessionOverview(t, root, session, "host", overviewIn{Cursor: cursor})
	meta, _ = chunkMetadata(t, second)
	if meta["chunk_index"].(float64) != 2 {
		t.Fatalf("v1 continuation did not proceed: %v", meta["chunk_index"])
	}
	if work := workCounters(t, second); work != (overviewWork{GovernanceEvals: 2, BodyBuilds: 1}) {
		t.Fatalf("v1 continuation changed its work: %+v", work)
	}
}

// A proof the store cannot retain drops the delivery and is scored on the
// live path, with the same verdict a retained proof would get.
func TestOverviewV2UnretainableProofIsScoredLive(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	session := newCognitionRefreshSession()
	first := sessionOverview(t, root, session, "host", overviewIn{Scope: "all"})
	if first.IsError {
		t.Fatal(resText(t, first))
	}
	if len(session.deliveries.entries) != 1 {
		t.Fatalf("expected one retained delivery, have %d", len(session.deliveries.entries))
	}
	for _, entry := range session.deliveries.entries {
		session.deliveries.maxBytes = entry.bytes
	}
	args := volumeAttestationArguments(t, root, resText(t, first), false)
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var input overviewIn
	if err := json.Unmarshal(encoded, &input); err != nil {
		t.Fatal(err)
	}
	result := sessionOverview(t, root, session, "host", input)
	if result.IsError || !strings.Contains(resText(t, result), "model_attestation: pass") {
		t.Fatalf("live scoring of an unretainable proof failed: %s", resText(t, result))
	}
	if work := workCounters(t, result); work != (overviewWork{GovernanceEvals: 2, BodyBuilds: 1}) {
		t.Fatalf("unretainable proof was not scored on the live path: %+v", work)
	}
	if len(session.deliveries.entries) != 0 {
		t.Fatal("the delivery whose proof could not be retained stayed in the store")
	}
}
