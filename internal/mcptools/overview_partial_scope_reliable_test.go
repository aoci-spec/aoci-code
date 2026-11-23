package mcptools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// A passing Attestation of a partial scope proves that scope only: the
// top-level model_full_cognition_reliable line says false, as the receipt
// beside it does (audit G3-05: it said true).
func TestOverviewPartialScopeNeverReadsAsFullCognition(t *testing.T) {
	root, _ := deltaVolumesFixture(t, 10)
	session := newCognitionRefreshSession()
	resText(t, sessionOverview(t, root, session, "first", overviewIn{Scope: cognition.ScopeCode}))
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeCode)
	if err != nil {
		t.Fatal(err)
	}
	body, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	framed := frameOverviewBody(root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	report := completeAttestation(buildOverviewChallenge(view.ScopeIdentity, sequence), view.ObjectCount, volumeScopeBytes(view)/3)
	encoded, err := json.Marshal(map[string]any{"scope": cognition.ScopeCode, "model_cognition_attestation": attestationMap(t, report),
		"host_delivery_confirmation": map[string]any{"version": overviewDeliveryReceiptV1, "body_sha256": framed.Receipt.BodySHA256,
			"body_bytes": framed.Receipt.BodyUTF8Bytes, "end_marker_observed": true}})
	if err != nil {
		t.Fatal(err)
	}
	var proof overviewIn
	if err := json.Unmarshal(encoded, &proof); err != nil {
		t.Fatal(err)
	}
	out := resText(t, sessionOverview(t, root, session, "first", proof))
	if !strings.Contains(out, "cognition_level: 4") {
		t.Fatalf("fixture: the scoped Attestation must pass:\n%s", out)
	}
	if !strings.Contains(out, "model_full_cognition_reliable: false") || strings.Contains(out, "model_full_cognition_reliable: true") {
		t.Fatalf("a partial scope must never read as full cognition:\n%s", out)
	}
}
