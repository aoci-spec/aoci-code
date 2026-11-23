package mcptools

import "testing"

// An older chain's attestation, finished after a newer one, never moves the
// session generation back or replaces the newer receipt (audit G3-02).
func TestDeliveryAttemptNeverMovesTheGenerationBack(t *testing.T) {
	session := newCognitionRefreshSession()
	newer := cognitionReceipt{RefreshGeneration: 2, LastRefreshEventID: "event-2"}
	session.markDeliveryAttempt(newer)
	session.markDeliveryAttempt(cognitionReceipt{RefreshGeneration: 1})
	if session.generation != 2 || session.lastReceipt.LastRefreshEventID != "event-2" {
		t.Fatalf("an older attestation moved the session back: generation=%d receipt=%+v", session.generation, session.lastReceipt)
	}
	session.markDeliveryAttempt(cognitionReceipt{RefreshGeneration: 3})
	if session.generation != 3 {
		t.Fatalf("a newer attestation did not advance the session: %d", session.generation)
	}
}
