package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// identity is the scope identity the fixture's body was delivered under.
	identity string
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
	fixture.body, fixture.identity = framed.Receipt.BodySHA256, view.ScopeIdentity
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
	if session.deliveryServed(host, cognition.ScopeAll, body, "") {
		t.Fatal("the superseded delivery's last Chunk finished its successor")
	}
	session.advanceDelivery(host, cognition.ScopeAll, other, "new", 2, 2)
	session.advanceDelivery(host, cognition.ScopeCode, body, "new", 2, 2)
	session.advanceDelivery(volumeDeliveryOwner{direct: "guest"}, cognition.ScopeAll, body, "new", 2, 2)
	if session.deliveryServed(host, cognition.ScopeAll, body, "") {
		t.Fatal("another body, scope, or Host session finished this delivery")
	}
	session.advanceDelivery(host, cognition.ScopeAll, body, "new", 2, 2)
	// A proof names the retained delivery it arrived through: the replaced
	// one proves nothing about its successor, though the bytes are the same.
	if session.deliveryServed(host, cognition.ScopeAll, body, "old") || !session.deliveryServed(host, cognition.ScopeAll, body, "new") {
		t.Fatal("only the current delivery's own proof may count it as sent")
	}
	if !session.deliveryServed(host, cognition.ScopeAll, body, "") || session.deliveryServed(host, cognition.ScopeCode, body, "") ||
		session.deliveryServed(host, cognition.ScopeAll, other, "") {
		t.Fatal("the current delivery, sent to its end, must be the one thing served")
	}
	// A Chunk out of order advances nothing: the last Chunk of three, asked
	// for after the first, leaves the delivery at its first.
	session.beginDelivery(host, cognition.ScopeCode, body, "ordered", 1, 3)
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 3, 3)
	if session.deliveryServed(host, cognition.ScopeCode, body, "") {
		t.Fatal("the last Chunk, asked for out of order, finished the delivery")
	}
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 2, 3)
	session.advanceDelivery(host, cognition.ScopeCode, body, "ordered", 3, 3)
	if !session.deliveryServed(host, cognition.ScopeCode, body, "") {
		t.Fatal("every Chunk in order must finish the delivery")
	}
	// A second body for the same scope replaces the first, proof memory included.
	session.mergeDeliveryEvidence(host, body, hostDeliveryConfirmed, nil)
	session.beginDelivery(host, cognition.ScopeAll, other, "", 1, 1)
	if session.deliveryServed(host, cognition.ScopeAll, body, "") || !session.deliveryServed(host, cognition.ScopeAll, other, "") {
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
		rendered, err := renderOverviewSent(ctx, input, loaded.cfg.OverviewDelivery.ChunkTokens)
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
	// A fresh request reads no proof memory: the confirmation remembered for
	// the body it is about to send again says nothing about this transmission.
	if again, err := renderOverviewDelivery(ctx, overviewIn{}, loaded.cfg.OverviewDelivery.ChunkTokens); err != nil ||
		again.Facts.HostDeliveryStatus == hostDeliveryConfirmed || again.Attestation.DeliveryIntegrity == deliveryIntegrityConfirmed {
		t.Fatalf("a fresh request answered with the earlier confirmation: %v %+v", err, again.Facts)
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

// refuseOnce makes the next rendered response meet a changed Code Volume, as
// a foreign write between rendering and the snapshot check does, and puts the
// bytes back when the test asks.
func refuseOnce(t *testing.T, root string) (restore func()) {
	t.Helper()
	path := filepath.Join(root, "aoci.code.txt")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	overviewRenderedHook = func() {
		overviewRenderedHook = nil
		if err := os.WriteFile(path, append(append([]byte(nil), original...), []byte("#foreign\n")...), 0o644); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { overviewRenderedHook = nil })
	return func() {
		t.Helper()
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
}

// A request that is refused sends nothing, so it changes nothing: a fresh
// request refused for its Chunk budget leaves the held cursor in use, one
// refused after rendering leaves the delivered body and its proof current,
// and a final Chunk refused after rendering is not counted as sent.
func TestARefusedRequestChangesNoDelivery(t *testing.T) {
	t.Run("refused before a body is prepared", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		first := fixture.chunk("host", freshAll())
		meta := filepath.Join(fixture.root, "aoci.meta.txt")
		original, err := os.ReadFile(meta)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(meta)
		if err != nil {
			t.Fatal(err)
		}
		oversize := append(append([]byte(nil), original...), []byte("#"+strings.Repeat("x", 3*machinecontract.OverviewChunkTokensMax+64)+"\n")...)
		if err := os.WriteFile(meta, oversize, 0o644); err != nil {
			t.Fatal(err)
		}
		if result := sessionOverview(t, fixture.root, fixture.session, "host", freshAll()); !result.IsError ||
			!strings.Contains(resText(t, result), "overview_chunk_budget_exceeded") {
			t.Fatalf("the oversize line must refuse the fresh request: %.300s", resText(t, result))
		}
		if err := os.WriteFile(meta, original, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(meta, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		fixture.rest("host", first)
		expectTransport(t, "the held chain after a refused fresh request", fixture.prove("host", fixture.both), true)
	})
	t.Run("a fresh request refused after rendering", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		expectTransport(t, "the first delivery's Host half", fixture.prove("host", fixture.host), true)
		restore := refuseOnce(t, fixture.root)
		if result := sessionOverview(t, fixture.root, fixture.session, "host", freshAll()); !result.IsError {
			t.Fatalf("the changed snapshot must refuse the fresh request: %.300s", resText(t, result))
		}
		restore()
		out := fixture.prove("host", fixture.model)
		expectTransport(t, "the delivered body after a refused fresh request", out, true)
		if !strings.Contains(out, "model_attestation: pass") {
			t.Fatalf("the remembered Host half must still meet the model half:\n%s", out)
		}
	})
	t.Run("a final Chunk refused after rendering", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		meta := fixture.chunk("host", freshAll())
		for int(meta["chunk_index"].(float64)) < int(meta["chunk_count"].(float64))-1 {
			meta = fixture.chunk("host", overviewIn{Cursor: meta["next_cursor"].(string)})
		}
		restore := refuseOnce(t, fixture.root)
		if result := sessionOverview(t, fixture.root, fixture.session, "host", overviewIn{Cursor: meta["next_cursor"].(string)}); !result.IsError ||
			!strings.Contains(resText(t, result), "overview_snapshot_changed") {
			t.Fatalf("the changed snapshot must refuse the final Chunk: %.300s", resText(t, result))
		}
		restore()
		expectTransport(t, "a final Chunk that was never sent", fixture.prove("host", fixture.both), false)
	})
}

// A request for a delivery that was replaced while it waited finds the
// delivery gone: its remembered Host half cannot prove a successor of the
// same bytes, and its late proof cannot close the reload that replaced it.
func TestAProofOnItsWayCannotSpeakForTheNextDelivery(t *testing.T) {
	held := func(t *testing.T, fixture *transportProofFixture) *volumeDelivery {
		t.Helper()
		entry := fixture.session.deliveries.proof(volumeDeliveryOwner{direct: "host"}, fixture.identity, "")
		if entry == nil {
			t.Fatal("the completed delivery must be retained")
		}
		return entry
	}
	// late submits a proof through a delivery the request had already picked
	// when it was replaced: it asks for a restart and answers nothing from
	// the replaced delivery's memory.
	late := func(t *testing.T, fixture *transportProofFixture, entry *volumeDelivery, input overviewIn) {
		t.Helper()
		loaded, fail := loadCognitionCtx(fixture.root)
		if fail != nil {
			t.Fatal(fail.Msg)
		}
		result := serveVolumeDelivery(fixture.root, input, loaded, fixture.session, entry, time.Now())
		if result == nil || !result.IsError || !strings.Contains(resText(t, result), "overview_session_unavailable_restart_required") {
			t.Fatalf("a proof for a replaced delivery must ask for a restart: %v", result)
		}
	}
	t.Run("a remembered Host half and a successor of the same bytes", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		expectTransport(t, "the first delivery's Host half", fixture.prove("host", fixture.host), true)
		old := held(t, fixture)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		late(t, fixture, old, fixture.model)
		if out := fixture.prove("host", fixture.model); strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) ||
			strings.Contains(out, "model_full_cognition_reliable: true") {
			t.Fatalf("the successor was never confirmed by its Host:\n%s", out)
		}
	})
	t.Run("a late proof and the reload that replaced its delivery", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		expectTransport(t, "the first delivery", fixture.prove("host", fixture.both), true)
		old := held(t, fixture)
		fixture.chunk("host", reloadAll("compaction-1"))
		late(t, fixture, old, fixture.both)
		stop := capacityStopFacts(t, resText(t, sessionOverview(t, fixture.root, fixture.session, "host", reloadAll("compaction-2"))))
		if stop["delivery_mode"] != machinecontract.OverviewDeliveryModeCapacityStop {
			t.Fatalf("the late proof closed the open reload: %v", stop)
		}
	})
}

// A retained delivery keeps its own proof memory. Scored through it, a proof
// counts only while that delivery is the current one, whatever its memory
// holds: here the memory has a confirmation and the scope's current delivery
// is a successor of the same bytes, sent to its end.
func TestARetainedDeliverysMemoryProvesOnlyItself(t *testing.T) {
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
	host := volumeDeliveryOwner{direct: "host"}
	memory, transport := newCognitionRefreshSession(), newCognitionRefreshSession()
	ctx := overviewRenderContext{Root: fixture.root, RequestedScope: cognition.ScopeAll, EffectiveScope: view.EffectiveScope, ScopeIdentity: view.ScopeIdentity,
		Content: body, EntryCount: view.ObjectCount, Sequence: sequence, EstimatedTokens: volumeScopeBytes(view) / 3,
		Session: memory, Transport: transport, Owner: host, DeliveryID: "old"}
	ctx.Assessment.Semantic.GovernanceAligned = true
	memory.mergeDeliveryEvidence(host, fixture.body.(string), hostDeliveryConfirmed, nil)
	transport.beginDelivery(host, cognition.ScopeAll, fixture.body.(string), "new", 3, 3)
	rendered, err := renderOverviewDelivery(ctx, fixture.model, loaded.cfg.OverviewDelivery.ChunkTokens)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Facts.HostDeliveryStatus == hostDeliveryConfirmed || rendered.Facts.ServerSent ||
		rendered.Attestation.CognitionAssimilation == cognitionAssimilationComplete {
		t.Fatalf("the replaced delivery's memory vouched for its successor: %+v", rendered.Facts)
	}
	ctx.DeliveryID = "new"
	memory.mergeDeliveryEvidence(host, fixture.body.(string), hostDeliveryConfirmed, nil)
	if rendered, err = renderOverviewDelivery(ctx, fixture.model, loaded.cfg.OverviewDelivery.ChunkTokens); err != nil ||
		rendered.Facts.HostDeliveryStatus != hostDeliveryConfirmed || !rendered.Facts.ServerSent {
		t.Fatalf("the current delivery's own memory must count: %v %+v", err, rendered.Facts)
	}
}

// A refused proof is forgotten whole. On the live path its model half must
// not wait in memory for a later Host half; through a retained delivery the
// room for it must not be made, at another delivery's expense, before the
// response stands.
func TestARefusedProofLeavesNoTrace(t *testing.T) {
	t.Run("the live path remembers no refused half", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		fixture.session.deliveries.maxBytes = 1
		first := fixture.chunk("host", freshAll())
		if strings.HasPrefix(first["next_cursor"].(string), machinecontract.OverviewDeliverySessionPrefix+":") {
			t.Fatal("the fixture must be served the live way")
		}
		fixture.rest("host", first)
		restore := refuseOnce(t, fixture.root)
		if result := sessionOverview(t, fixture.root, fixture.session, "host", fixture.model); !result.IsError {
			t.Fatalf("the changed snapshot must refuse the model half: %.300s", resText(t, result))
		}
		restore()
		out := fixture.prove("host", fixture.host)
		expectTransport(t, "the Host half after a refused model half", out, true)
		if strings.Contains(out, "model_attestation: pass") || strings.Contains(out, "model_full_cognition_reliable: true") {
			t.Fatalf("the refused model half was remembered:\n%s", out)
		}
		// An accepted half is remembered: the model half given again meets it.
		if out = fixture.prove("host", fixture.model); !strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) ||
			!strings.Contains(out, "model_attestation: pass") {
			t.Fatalf("halves accepted on the live path must latch:\n%s", out)
		}
	})
	t.Run("a refused proof evicts no other delivery", func(t *testing.T) {
		fixture := newTransportProofFixture(t)
		fixture.rest("host", fixture.chunk("host", freshAll()))
		code, _ := chunkMetadata(t, sessionOverview(t, fixture.root, fixture.session, "host", overviewIn{Scope: cognition.ScopeCode}))
		store := fixture.session.deliveries
		store.maxBytes = store.bytes
		restore := refuseOnce(t, fixture.root)
		if result := sessionOverview(t, fixture.root, fixture.session, "host", fixture.both); !result.IsError {
			t.Fatalf("the changed snapshot must refuse the proof: %.300s", resText(t, result))
		}
		restore()
		if result := sessionOverview(t, fixture.root, fixture.session, "host", overviewIn{Scope: cognition.ScopeCode, Cursor: code["next_cursor"].(string)}); result.IsError {
			t.Fatalf("the refused proof evicted another scope's delivery: %s", resText(t, result))
		}
	})
}

// A proof the store cannot retain is scored the live way, and still only for
// the delivery the request picked: replaced on the way by a successor of the
// same bytes, it proves nothing about that successor.
func TestAProofThatFallsBackKeepsItsDelivery(t *testing.T) {
	fixture := newTransportProofFixture(t)
	fixture.rest("host", fixture.chunk("host", freshAll()))
	store := fixture.session.deliveries
	store.maxBytes = store.bytes
	overviewLiveFallbackHook = func() {
		overviewLiveFallbackHook = nil
		fixture.rest("host", fixture.chunk("host", freshAll()))
	}
	t.Cleanup(func() { overviewLiveFallbackHook = nil })
	expectTransport(t, "a proof whose delivery was replaced during its fallback", fixture.prove("host", fixture.both), false)
	if overviewLiveFallbackHook != nil {
		t.Fatal("the proof must have fallen back to the live path")
	}
	// Nor does it leave its model half for the successor's Host half to meet.
	out := fixture.prove("host", fixture.host)
	expectTransport(t, "the successor's own Host half", out, true)
	if !strings.Contains(out, "model_attestation: "+modelAttestationNotProvided) {
		t.Fatalf("the replaced proof left its model half to the successor:\n%s", out)
	}

	// With no replacement on the way, the same fallback proves its delivery.
	fixture = newTransportProofFixture(t)
	fixture.rest("host", fixture.chunk("host", freshAll()))
	fixture.session.deliveries.maxBytes = fixture.session.deliveries.bytes
	expectTransport(t, "a proof scored the live way for its own delivery", fixture.prove("host", fixture.both), true)
}

// A fresh request refused after rendering leaves the governance binding of
// the delivery it would have replaced, so that delivery's proof is still
// judged against the snapshot it was delivered under.
func TestARefusedFreshRequestRebindsNoGovernance(t *testing.T) {
	fixture := newTransportProofFixture(t)
	fixture.session.deliveries.maxBytes = 1
	fixture.rest("host", fixture.chunk("host", freshAll()))
	bound := map[string]string{}
	for key, identity := range fixture.session.governanceSnapshots {
		bound[key] = identity
	}
	if len(bound) != 1 {
		t.Fatalf("the delivery must have bound one governance snapshot: %v", bound)
	}
	baselinePath := filepath.Join(fixture.root, ".aoci", "baseline.json")
	original, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, append(append([]byte(nil), original...), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := refuseOnce(t, fixture.root)
	if result := sessionOverview(t, fixture.root, fixture.session, "host", freshAll()); !result.IsError {
		t.Fatalf("the changed snapshot must refuse the fresh request: %.300s", resText(t, result))
	}
	restore()
	for key, identity := range bound {
		if fixture.session.governanceSnapshots[key] != identity {
			t.Fatal("the refused request rebound the governance snapshot of the delivery it did not replace")
		}
	}
	// The same request, answered, does rebind.
	fixture.chunk("host", freshAll())
	for key, identity := range bound {
		if fixture.session.governanceSnapshots[key] == identity {
			t.Fatal("an answered fresh request must bind the snapshot it was delivered under")
		}
	}
}

// A Host half given after the first Chunk of a retained delivery is scored
// the live way and must not be remembered there: the retained Chunks that
// follow keep their own memory and would not clear it, and a model half that
// later falls back to the live path would find it waiting.
func TestAnEarlyConfirmationDoesNotWaitOnTheLivePath(t *testing.T) {
	fixture := newTransportProofFixture(t)
	first := fixture.chunk("host", freshAll())
	expectTransport(t, "an early Host half on a retained delivery", fixture.prove("host", fixture.host), false)
	fixture.rest("host", first)
	fixture.session.deliveries.maxBytes = fixture.session.deliveries.bytes
	out := fixture.prove("host", fixture.model)
	if strings.Contains(out, "host_delivery_status: "+hostDeliveryConfirmed) || strings.Contains(out, "cognition_assimilation: complete") {
		t.Fatalf("the early Host half waited on the live path:\n%s", out)
	}
}
