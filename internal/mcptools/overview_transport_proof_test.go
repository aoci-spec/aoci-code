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

// A body this process has no fresh request for is not sent, however its
// record was lost: a continuation alone never completes it.
func TestUnknownTransportProgressIsNotSent(t *testing.T) {
	fixture := newTransportProofFixture(t)
	first := fixture.chunk("host", freshAll())
	delete(fixture.session.deliveryProgress, deliveryKey{owner: volumeDeliveryOwner{direct: "host"}, body: fixture.body.(string)})
	fixture.rest("host", first)
	expectTransport(t, "a chain whose start this process does not remember", fixture.prove("host", fixture.both), false)
	for index := 0; index <= deliveryProgressMax; index++ {
		fixture.session.noteDeliveryProgress(volumeDeliveryOwner{direct: "host"}, strings.Repeat("0", 60)+strings.Repeat("a", 4-len(string(rune('a'+index%4))))+string(rune('a'+index%4)), true, 1, 3)
	}
	if len(fixture.session.deliveryProgress) > deliveryProgressMax || len(fixture.session.progressOrder) > deliveryProgressMax {
		t.Fatalf("the progress memory is unbounded: %d", len(fixture.session.deliveryProgress))
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
}
