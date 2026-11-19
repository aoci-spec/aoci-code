package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// transportProofFixture delivers the chunked fixture once to its end and
// returns the proof of that body split into its two halves.
type transportProofFixture struct {
	t       *testing.T
	root    string
	session *cognitionRefreshSession
	both    overviewIn
	host    overviewIn
	model   overviewIn
	body    any
}

func newTransportProofFixture(t *testing.T) *transportProofFixture {
	t.Helper()
	fixture := &transportProofFixture{t: t, root: chunkSessionRepo(t), session: newCognitionRefreshSession()}
	loaded, fail := loadCognitionCtx(fixture.root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	body, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	framed := frameOverviewBody(fixture.root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	report := completeAttestation(buildOverviewChallenge(view.ScopeIdentity, sequence), view.ObjectCount, volumeScopeBytes(view)/3)
	confirmation := map[string]any{
		"version": overviewDeliveryReceiptV1, "body_sha256": framed.Receipt.BodySHA256,
		"body_bytes": framed.Receipt.BodyUTF8Bytes, "end_marker_observed": true,
	}
	decode := func(arguments map[string]any) overviewIn {
		t.Helper()
		arguments["scope"] = cognition.ScopeAll
		encoded, err := json.Marshal(arguments)
		if err != nil {
			t.Fatal(err)
		}
		var input overviewIn
		if err := json.Unmarshal(encoded, &input); err != nil {
			t.Fatal(err)
		}
		return input
	}
	fixture.body = framed.Receipt.BodySHA256
	fixture.host = decode(map[string]any{"host_delivery_confirmation": confirmation})
	fixture.model = decode(map[string]any{"model_cognition_attestation": attestationMap(t, report)})
	fixture.both = decode(map[string]any{"host_delivery_confirmation": confirmation, "model_cognition_attestation": attestationMap(t, report)})
	return fixture
}

// chunk requests one Chunk for an owner and returns its metadata.
func (fixture *transportProofFixture) chunk(owner string, input overviewIn) map[string]any {
	fixture.t.Helper()
	meta, _ := chunkMetadata(fixture.t, sessionOverview(fixture.t, fixture.root, fixture.session, owner, input))
	if meta["body_sha256"] != nil && meta["body_sha256"] != fixture.body {
		fixture.t.Fatalf("the fixture's body changed: %v", meta["body_sha256"])
	}
	return meta
}

// rest follows a chain from the given Chunk metadata to its end.
func (fixture *transportProofFixture) rest(owner string, meta map[string]any) {
	fixture.t.Helper()
	for !meta["completed"].(bool) {
		meta = fixture.chunk(owner, overviewIn{Cursor: meta["next_cursor"].(string)})
	}
}

func (fixture *transportProofFixture) prove(owner string, input overviewIn) string {
	fixture.t.Helper()
	result := sessionOverview(fixture.t, fixture.root, fixture.session, owner, input)
	if result.IsError {
		fixture.t.Fatalf("proof refused: %s", resText(fixture.t, result))
	}
	return resText(fixture.t, result)
}

func expectTransport(t *testing.T, what, out string, confirmed bool) {
	t.Helper()
	if confirmed {
		if !strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) || !strings.Contains(out, "server_delivery_complete: true") {
			t.Fatalf("%s must confirm:\n%s", what, out)
		}
		return
	}
	if strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) || strings.Contains(out, "model_full_cognition_reliable: true") ||
		strings.Contains(out, "cognition_assimilation: complete") || !strings.Contains(out, "server_delivery_complete: false") {
		t.Fatalf("%s must not confirm:\n%s", what, out)
	}
}

func freshAll() overviewIn { return overviewIn{Scope: cognition.ScopeAll} }

func reloadAll(event string) overviewIn {
	return overviewIn{Scope: cognition.ScopeAll, RefreshReasons: []string{"context_compaction"}, RefreshEventID: event}
}

// A Host confirmation echoes a hash the first Chunk prints, and an unchanged
// index has the hash it had before. After a compaction the reload used to be
// provable without being read. The confirmation counts only for a body this
// process sent to its end for that Host session since its last fresh request.
func TestHostConfirmationNeedsTheCurrentChainSentToItsEnd(t *testing.T) {
	fixture := newTransportProofFixture(t)
	first := fixture.chunk("host", freshAll())
	if int(first["chunk_count"].(float64)) < 3 {
		t.Fatalf("the fixture must take at least three Chunks, takes %v", first["chunk_count"])
	}
	expectTransport(t, "a proof before the end of the first chain", fixture.prove("host", fixture.both), false)
	fixture.rest("host", first)
	out := fixture.prove("host", fixture.both)
	expectTransport(t, "a delivery sent to its end", out, true)
	if !strings.Contains(out, "model_attestation: pass") {
		t.Fatalf("the complete proof must pass:\n%s", out)
	}

	// The reload sends one Chunk; the earlier proof, whole or in halves, must
	// not vouch for it, and the earlier chain's cursors are gone with it.
	reload := fixture.chunk("host", reloadAll("compaction-1"))
	expectTransport(t, "the whole earlier proof after one Chunk of the reload", fixture.prove("host", fixture.both), false)
	expectTransport(t, "the model half alone after one Chunk of the reload", fixture.prove("host", fixture.model), false)
	expectTransport(t, "the Host half alone after one Chunk of the reload", fixture.prove("host", fixture.host), false)
	if result := sessionOverview(t, fixture.root, fixture.session, "host", overviewIn{Cursor: first["next_cursor"].(string)}); !result.IsError ||
		!strings.Contains(resText(t, result), "overview_session_unavailable_restart_required") {
		t.Fatalf("a superseded chain's cursor must ask for a restart: %s", resText(t, result))
	}
	expectTransport(t, "the proof after a superseded cursor was tried", fixture.prove("host", fixture.both), false)

	// The same reload chain, followed to its end by its own cursors, proves.
	fixture.rest("host", reload)
	out = fixture.prove("host", fixture.both)
	expectTransport(t, "the reload followed to its end", out, true)
	if !strings.Contains(out, "model_attestation: pass") {
		t.Fatalf("the reload's proof must pass:\n%s", out)
	}
}

// Halves that arrive separately latch only for a body sent to its end: a
// confirmation remembered from before the end counts for nothing later.
func TestSplitProofHalvesDoNotLatchBeforeTheEnd(t *testing.T) {
	fixture := newTransportProofFixture(t)
	first := fixture.chunk("host", freshAll())
	expectTransport(t, "an early Host half", fixture.prove("host", fixture.host), false)
	fixture.rest("host", first)
	// The body is at its end now, and no confirmation is remembered for it.
	if out := fixture.prove("host", fixture.model); strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) ||
		strings.Contains(out, "model_full_cognition_reliable: true") || !strings.Contains(out, "server_delivery_complete: true") {
		t.Fatalf("a Host half given before the end latched:\n%s", out)
	}
	expectTransport(t, "the Host half given again at the end", fixture.prove("host", fixture.host), true)
	out := fixture.prove("host", fixture.model)
	expectTransport(t, "the model half after the Host half at the end", out, true)
	if !strings.Contains(out, "model_attestation: pass") {
		t.Fatalf("halves given at the end must latch and pass:\n%s", out)
	}
}

// Two Host sessions of one server share its refresh state. Neither rides the
// other's transport: a session that was sent nothing confirms nothing.
func TestAnotherHostSessionCannotProveThisOnesDelivery(t *testing.T) {
	fixture := newTransportProofFixture(t)
	fixture.rest("first", fixture.chunk("first", freshAll()))
	expectTransport(t, "the session that received the body", fixture.prove("first", fixture.both), true)
	expectTransport(t, "a session that requested nothing", fixture.prove("second", fixture.both), false)
	fixture.chunk("second", freshAll())
	expectTransport(t, "a session that was sent one Chunk", fixture.prove("second", fixture.both), false)
	expectTransport(t, "the first session, after the second one started", fixture.prove("first", fixture.both), true)
}

// Nothing is sent that is not current: a chain whose record this process
// lost is not completed by its continuations, the memory is bounded, and the
// oldest scope is the one that goes.
func TestOnlyTheCurrentDeliveryCountsAsSent(t *testing.T) {
	fixture := newTransportProofFixture(t)
	host := volumeDeliveryOwner{direct: "host"}
	scope := deliveryScope{owner: host, scope: cognition.ScopeAll}
	first := fixture.chunk("host", freshAll())
	delete(fixture.session.currentDeliveries, scope)
	fixture.rest("host", first)
	expectTransport(t, "a chain whose start this process does not remember", fixture.prove("host", fixture.both), false)

	fixture.rest("host", fixture.chunk("host", freshAll()))
	expectTransport(t, "the same body requested afresh and sent to its end", fixture.prove("host", fixture.both), true)
	for index := 0; index < currentDeliveryMax; index++ {
		fixture.session.beginDelivery(volumeDeliveryOwner{direct: fmt.Sprintf("other-%d", index)}, cognition.ScopeAll, strings.Repeat("b", 64), "", 1, 3)
	}
	if len(fixture.session.currentDeliveries) != currentDeliveryMax || len(fixture.session.currentOrder) != currentDeliveryMax {
		t.Fatalf("the memory must hold exactly its bound: %d", len(fixture.session.currentDeliveries))
	}
	if fixture.session.currentDeliveries[scope] != nil {
		t.Fatal("the oldest scope must be the one evicted")
	}
	expectTransport(t, "a delivery evicted from the memory", fixture.prove("host", fixture.both), false)
}

// A delivery that is not the current one of its scope advances nothing: a
// request of a superseded delivery that was already in flight when its
// successor began, a Chunk out of order, and another body.
func TestOnlyTheCurrentDeliveryAdvances(t *testing.T) {
	session := newCognitionRefreshSession()
	host := volumeDeliveryOwner{direct: "host"}
	body, other := strings.Repeat("a", 64), strings.Repeat("c", 64)
	session.beginDelivery(host, cognition.ScopeAll, body, "old", 1, 2)
	session.beginDelivery(host, cognition.ScopeAll, body, "new", 1, 2)
	session.advanceDelivery(host, cognition.ScopeAll, body, "old", 2, 2)
	if session.deliveryServed(host, cognition.ScopeAll, body) {
		t.Fatal("the superseded delivery's last Chunk finished its successor")
	}
	session.advanceDelivery(host, cognition.ScopeAll, other, "new", 2, 2)
	session.advanceDelivery(host, cognition.ScopeCode, body, "new", 2, 2)
	session.advanceDelivery(volumeDeliveryOwner{direct: "guest"}, cognition.ScopeAll, body, "new", 2, 2)
	if session.deliveryServed(host, cognition.ScopeAll, body) {
		t.Fatal("another body, scope, or Host session finished this delivery")
	}
	session.advanceDelivery(host, cognition.ScopeAll, body, "new", 2, 2)
	if !session.deliveryServed(host, cognition.ScopeAll, body) || session.deliveryServed(host, cognition.ScopeCode, body) ||
		session.deliveryServed(host, cognition.ScopeAll, other) {
		t.Fatal("the current delivery, sent to its end, must be the one thing served")
	}
	// A Chunk out of order advances nothing: the last Chunk of three, asked
	// for after the first, leaves the delivery at its first.
	session.beginDelivery(host, cognition.ScopeCode, body, "ordered", 1, 3)
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 3, 3)
	if session.deliveryServed(host, cognition.ScopeCode, body) {
		t.Fatal("the last Chunk, asked for out of order, finished the delivery")
	}
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 2, 3)
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 3, 3)
	if !session.deliveryServed(host, cognition.ScopeCode, body) {
		t.Fatal("every Chunk in order must finish the delivery")
	}
	// A second body for the same scope replaces the first, proof memory included.
	session.mergeDeliveryEvidence(host, body, hostDeliveryConfirmed, nil)
	session.beginDelivery(host, cognition.ScopeAll, other, "", 1, 1)
	if session.deliveryServed(host, cognition.ScopeAll, body) || !session.deliveryServed(host, cognition.ScopeAll, other) {
		t.Fatal("a new body for the scope must replace the earlier one")
	}
	if status, _ := session.mergeDeliveryEvidence(host, body, hostDeliveryUnconfirmed, nil); status == hostDeliveryConfirmed {
		t.Fatal("the replaced body's confirmation stayed remembered")
	}
}

// An earlier delivery's Attestation, submitted after one Chunk of a reload,
// proves no transport and must not close the reload attempt: the next
// compaction still finds it open and answers the capacity stop.
func TestAnEarlierProofDoesNotCloseAnOpenReload(t *testing.T) {
	for _, half := range []string{"model", "host", "both"} {
		fixture := newTransportProofFixture(t)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		expectTransport(t, "the first delivery", fixture.prove("host", fixture.both), true)
		fixture.chunk("host", reloadAll("compaction-1"))
		expectTransport(t, "the earlier "+half+" half after one Chunk of the reload",
			fixture.prove("host", map[string]overviewIn{"model": fixture.model, "host": fixture.host, "both": fixture.both}[half]), false)
		stop := capacityStopFacts(t, resText(t, sessionOverview(t, fixture.root, fixture.session, "host", reloadAll("compaction-2"))))
		if stop["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop {
			t.Fatalf("the earlier %s half closed the open reload: %v", half, stop)
		}
	}
}

// The Legacy layout keeps no retained delivery: its proof halves live in the
// Host session's own memory. A confirmation given before the end must not
// stay there to latch once the end is sent, and a session that was sent
// nothing confirms nothing.
func TestLegacyConfirmationNeedsTheChainSentToItsEnd(t *testing.T) {
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.OverviewDelivery.ChunkTokens = machinecontract.OverviewChunkTokensMin
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	text.WriteString("#====Legacy transport fixture====\n#Locale: zh-CN\n===" + filepath.ToSlash(root) + "/===\n")
	for ordinal := 1; ordinal <= 400; ordinal++ {
		name := fmt.Sprintf("file-%04d.go", ordinal)
		if err := os.WriteFile(filepath.Join(root, name), []byte("package demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&text, "%s[XUT5T]: F:preserve legacy object %04d across a reload | R:- | A:- | S:-\n", name, ordinal)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	chunk := func(client *mcp.ClientSession, arguments map[string]any) map[string]any {
		t.Helper()
		prefix, _, ok := strings.Cut(callVolumeTool(t, client, "aoci_overview", arguments), "\n"+overviewChunkBodyMarker+"\n")
		if !ok {
			t.Fatalf("the Legacy fixture must chunk: %.200s", prefix)
		}
		var meta map[string]any
		if err := json.Unmarshal([]byte(prefix), &meta); err != nil {
			t.Fatal(err)
		}
		return meta
	}
	client := connectMCPClient(t, root)
	first := chunk(client, map[string]any{})
	if int(first["chunk_count"].(float64)) < 3 {
		t.Fatalf("the Legacy fixture must take at least three Chunks, takes %v", first["chunk_count"])
	}
	confirmation := map[string]any{"host_delivery_confirmation": map[string]any{
		"version": overviewDeliveryReceiptV1, "body_sha256": first["body_sha256"],
		"body_bytes": first["body_utf8_bytes"], "end_marker_observed": true,
	}}
	confirm := func(client *mcp.ClientSession) string {
		return callVolumeTool(t, client, "aoci_overview", confirmation)
	}
	expectTransport(t, "a Legacy confirmation after one Chunk", confirm(client), false)
	meta := first
	for !meta["completed"].(bool) {
		meta = chunk(client, map[string]any{"cursor": meta["next_cursor"]})
	}
	expectTransport(t, "a Legacy confirmation at the end", confirm(client), true)

	// A reload restarts the body: one Chunk does not carry the earlier proof.
	reload := chunk(client, map[string]any{"refresh_reasons": []string{"context_compaction"}, "refresh_event_id": "legacy-reload"})
	expectTransport(t, "the earlier Legacy proof after one Chunk of the reload", confirm(client), false)
	for meta = reload; !meta["completed"].(bool); {
		meta = chunk(client, map[string]any{"cursor": meta["next_cursor"]})
	}
	expectTransport(t, "the Legacy reload followed to its end", confirm(client), true)

	// A second MCP session of the same server, and a new server, were sent nothing.
	expectTransport(t, "a Legacy session that requested nothing", confirm(connectMCPClient(t, root)), false)
}

// On the live path (no retained delivery) the two halves meet in the Host
// session's own memory. A Host half given before the end is graded
// incomplete and must not stay remembered: once the end is sent, the model
// half alone finds no confirmation waiting for it.
func TestEarlyConfirmationIsNotRememberedOnTheLivePath(t *testing.T) {
	fixture := newTransportProofFixture(t)
	loaded, fail := loadCognitionCtx(fixture.root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	body, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	session := newCognitionRefreshSession()
	ctx := overviewRenderContext{Root: fixture.root, EffectiveScope: view.EffectiveScope, ScopeIdentity: view.ScopeIdentity,
		Content: body, EntryCount: view.ObjectCount, Sequence: sequence, EstimatedTokens: volumeScopeBytes(view) / 3,
		Session: session, Owner: volumeDeliveryOwner{direct: "host"}}
	ctx.Assessment.Semantic.GovernanceAligned = true
	render := func(input overviewIn) overviewRendered {
		t.Helper()
		rendered, err := renderOverviewDelivery(ctx, input, loaded.cfg.OverviewDelivery.ChunkTokens)
		if err != nil {
			t.Fatal(err)
		}
		return rendered
	}
	first := render(overviewIn{})
	if first.Facts.ChunkCount < 3 || first.Facts.Completed {
		t.Fatalf("the fixture must take at least three Chunks: %+v", first.Facts)
	}
	if early := render(fixture.host); early.Facts.HostDeliveryStatus != hostDeliveryIncomplete || early.Facts.ServerSent {
		t.Fatalf("a Host half before the end must be incomplete: %+v", early.Facts)
	}
	for chunk := first; !chunk.Facts.Completed; {
		meta, _ := chunkMetadata(t, textResult(chunk.Output))
		chunk = render(overviewIn{Cursor: meta["next_cursor"].(string)})
	}
	if late := render(fixture.model); late.Facts.HostDeliveryStatus == hostDeliveryConfirmed || !late.Facts.ServerSent ||
		late.Attestation.CognitionAssimilation == cognitionAssimilationComplete {
		t.Fatalf("the early Host half latched once the end was sent: %+v", late.Facts)
	}
	if done := render(fixture.host); done.Facts.HostDeliveryStatus != hostDeliveryConfirmed ||
		done.Attestation.ModelAttestation != modelAttestationPass || done.Attestation.CognitionAssimilation != cognitionAssimilationComplete {
		t.Fatalf("both halves given at the end must latch and pass: %+v %+v", done.Facts, done.Attestation)
	}
	// The confirmation is remembered now. Once the delivery is no longer
	// current (here: evicted), the remembered half must not confirm what a
	// later call carries no confirmation for: the check follows the merge.
	for index := 0; index < currentDeliveryMax; index++ {
		session.beginDelivery(volumeDeliveryOwner{direct: fmt.Sprintf("other-%d", index)}, ctx.RequestedScope, strings.Repeat("b", 64), "", 1, 3)
	}
	if lost := render(fixture.model); lost.Facts.HostDeliveryStatus == hostDeliveryConfirmed || lost.Facts.ServerSent ||
		lost.Attestation.CognitionAssimilation == cognitionAssimilationComplete {
		t.Fatalf("a remembered confirmation vouched for a delivery that is not current: %+v", lost.Facts)
	}
}
