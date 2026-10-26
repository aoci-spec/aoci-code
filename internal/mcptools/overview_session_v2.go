package mcptools

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// The store belongs to one MCP server, not a repository-global cache. Each
// delivery owns immutable bytes/guards and private split-proof evidence. Chunk
// position is encoded and authenticated in a token, never a mutable counter.
type volumeDeliveryStore struct {
	mu                 sync.Mutex
	entries            map[string]*volumeDelivery
	bytes              int
	now                func() time.Time
	maxCount, maxBytes int
}

// Weak object identity prevents address reuse without retaining closed SDK
// sessions or their transport buffers. Equality survives referent reclamation.
// Direct callers use a separate label namespace only for in-process tests.
type volumeDeliveryOwner struct {
	session weak.Pointer[mcp.ServerSession]
	direct  string
}

type volumeDelivery struct {
	id                 string
	owner              volumeDeliveryOwner
	secret             [32]byte
	ctx                overviewRenderContext
	guard              *overviewSourceGuard
	semantic           semanticChangeFacts
	governanceIdentity string
	tokens, bytes      int
	proofBytes         int // protected by store.mu; high-water retained proof accounting
	created, touched   time.Time
	complete           bool // protected by store.mu; no chunk position is retained
}

func newVolumeDeliveryStore() *volumeDeliveryStore {
	return &volumeDeliveryStore{entries: map[string]*volumeDelivery{}, now: time.Now, maxCount: machinecontract.OverviewDeliverySessionMaxCount, maxBytes: machinecontract.OverviewDeliverySessionMaxBytes}
}

func (store *volumeDeliveryStore) prune(now time.Time) {
	for id, entry := range store.entries {
		if now.Sub(entry.touched).Milliseconds() > machinecontract.OverviewDeliverySessionIdleMillis || now.Sub(entry.created).Milliseconds() > machinecontract.OverviewDeliverySessionLifetimeMillis {
			delete(store.entries, id)
			store.bytes -= entry.bytes
		}
	}
}

func (store *volumeDeliveryStore) put(entry *volumeDelivery) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now()
	store.prune(now)
	if entry.bytes > store.maxBytes || store.maxCount < 1 {
		return fmt.Errorf("overview_session_capacity_exceeded")
	}
	for len(store.entries) >= store.maxCount || store.bytes+entry.bytes > store.maxBytes {
		var oldest *volumeDelivery
		for _, candidate := range store.entries {
			if oldest == nil || candidate.touched.Before(oldest.touched) {
				oldest = candidate
			}
		}
		delete(store.entries, oldest.id)
		store.bytes -= oldest.bytes
	}
	entry.created, entry.touched = now, now
	store.entries[entry.id] = entry
	store.bytes += entry.bytes
	return nil
}

func (store *volumeDeliveryStore) lookup(id string, owner volumeDeliveryOwner) *volumeDelivery {
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now()
	store.prune(now)
	entry := store.entries[id]
	if entry == nil || entry.owner != owner {
		return nil
	}
	entry.touched = now
	return entry
}

func (store *volumeDeliveryStore) proof(owner volumeDeliveryOwner, identity, bodyHash string) *volumeDelivery {
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now()
	store.prune(now)
	var latest *volumeDelivery
	for _, entry := range store.entries {
		if entry.owner != owner || entry.ctx.ScopeIdentity != identity || !entry.complete {
			continue
		}
		if bodyHash != "" && entry.ctx.Prepared.body.Receipt.BodySHA256 != bodyHash {
			continue
		}
		if latest == nil || entry.created.After(latest.created) {
			latest = entry
		}
	}
	if latest != nil {
		latest.touched = now
	}
	return latest
}

func (store *volumeDeliveryStore) completed(entry *volumeDelivery) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries[entry.id] == entry {
		entry.complete = true
	}
}

// Proof halves are retained too. Reserve their conservative high-water cost
// before merge; replacement never reduces the budget while requests may hold
// the prior report. In-flight request arguments are transient, not store state.
func (store *volumeDeliveryStore) reserveProof(entry *volumeDelivery, report *overviewModelAttestation) error {
	if report == nil {
		return nil
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	cost := 8*len(encoded) + 4096
	store.mu.Lock()
	defer store.mu.Unlock()
	store.prune(store.now())
	if store.entries[entry.id] != entry {
		return fmt.Errorf("overview_session_unavailable_restart_required")
	}
	if cost <= entry.proofBytes {
		return nil
	}
	delta := cost - entry.proofBytes
	if entry.bytes+delta > store.maxBytes {
		return fmt.Errorf("overview_session_capacity_exceeded")
	}
	for store.bytes+delta > store.maxBytes {
		var oldest *volumeDelivery
		for _, candidate := range store.entries {
			if candidate != entry && (oldest == nil || candidate.touched.Before(oldest.touched)) {
				oldest = candidate
			}
		}
		delete(store.entries, oldest.id)
		store.bytes -= oldest.bytes
	}
	entry.proofBytes = cost
	entry.bytes += delta
	store.bytes += delta
	return nil
}

func (store *volumeDeliveryStore) remove(entry *volumeDelivery) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries[entry.id] == entry {
		delete(store.entries, entry.id)
		store.bytes -= entry.bytes
	}
}

func newVolumeDelivery(owner volumeDeliveryOwner, ctx overviewRenderContext, guard *overviewSourceGuard, semantic semanticChangeFacts, identity string, tokens int) (*volumeDelivery, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	entry := &volumeDelivery{id: hex.EncodeToString(nonce), owner: owner, ctx: ctx, guard: guard, semantic: semantic, governanceIdentity: identity, tokens: tokens}
	if _, err := rand.Read(entry.secret[:]); err != nil {
		return nil, err
	}
	// Conservative retained-memory accounting includes backing strings and maps;
	// no whole inventory, source body, or managed-state evaluation is retained.
	entry.bytes = 8*len(ctx.Content) + 8*len(ctx.Prepared.body.Text) + 2048*ctx.EntryCount
	for path := range guard.sources {
		entry.bytes += 512 + 4*len(path)
	}
	for path := range guard.metadata {
		entry.bytes += 256 + 4*len(path)
	}
	entry.ctx.Session = newCognitionRefreshSession()
	return entry, nil
}

func (entry *volumeDelivery) cursor(legacy string) string {
	prefix := machinecontract.OverviewDeliverySessionPrefix + ":" + entry.id + ":" + legacy
	mac := hmac.New(sha256.New, entry.secret[:])
	_, _ = mac.Write([]byte(prefix))
	return prefix + ":" + hex.EncodeToString(mac.Sum(nil))
}

func (entry *volumeDelivery) decode(cursor string) (string, error) {
	if len(cursor) > machinecontract.OverviewDeliveryCursorMaxBytes {
		return "", fmt.Errorf("overview_cursor_invalid")
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 7 || parts[0] != machinecontract.OverviewDeliverySessionPrefix || parts[1] != entry.id {
		return "", fmt.Errorf("overview_cursor_invalid")
	}
	signature, err := hex.DecodeString(parts[6])
	if err != nil {
		return "", fmt.Errorf("overview_cursor_invalid")
	}
	mac := hmac.New(sha256.New, entry.secret[:])
	_, _ = mac.Write([]byte(strings.Join(parts[:6], ":")))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", fmt.Errorf("overview_cursor_chain_mismatch")
	}
	legacy := strings.Join(parts[2:6], ":")
	if _, _, err := decodeOverviewCursor(legacy, entry.ctx.ScopeIdentity, entry.tokens); err != nil {
		return "", err
	}
	return legacy, nil
}

func rewriteVolumeCursor(delivery *overviewRendered, entry *volumeDelivery) error {
	if delivery.Facts.DeliveryMode != overviewDeliveryChunked {
		return nil
	}
	prefix, body, ok := strings.Cut(delivery.Output, "\n"+overviewChunkBodyMarker+"\n")
	if !ok {
		return fmt.Errorf("overview_session_frame_invalid")
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(prefix), &metadata); err != nil {
		return err
	}
	if next, ok := metadata["next_cursor"].(string); ok && next != "" {
		metadata["next_cursor"] = entry.cursor(next)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	delivery.Output = string(encoded) + "\n" + overviewChunkBodyMarker + "\n" + body
	delivery.Facts.OutputBytes = len(delivery.Output)
	return nil
}

type overviewWork struct {
	GovernanceEvals int `json:"governance_evals"`
	BodyBuilds      int `json:"body_builds"`
}

func overviewWorkResult(delivery overviewRendered, work overviewWork) *mcp.CallToolResult {
	result := textResult(delivery.Output)
	result.SetMeta(map[string]any{"aoci_work": work})
	return result
}

func volumeOwner(request *mcp.CallToolRequest) volumeDeliveryOwner {
	if request == nil || request.Session == nil {
		return volumeDeliveryOwner{direct: "direct"}
	}
	return volumeDeliveryOwner{session: weak.Make(request.Session)}
}

func deliveryCompleted(delivery overviewRendered, entry *volumeDelivery) bool {
	if delivery.Facts.DeliveryMode == overviewDeliveryFull {
		return true
	}
	if delivery.Facts.DeliveryMode != overviewDeliveryChunked {
		return false
	}
	return strings.HasSuffix(delivery.Output, entry.ctx.Prepared.body.Receipt.EndMarker+"\n")
}

// The token is bound to both the immutable snapshot and its Host session.
// Postflight repeats the cheap input guard, never the full policy evaluation.
// A nil result means the store could not retain the proof this call carries:
// the delivery is dropped and the caller serves the request the live way.
func serveVolumeDelivery(root string, input overviewIn, loaded *cognitionRepoCtx, refresh *cognitionRefreshSession, entry *volumeDelivery, start time.Time) *mcp.CallToolResult {
	if err := entry.guard.validate(root, loaded); err != nil {
		refresh.deliveries.remove(entry)
		return overviewSnapshotFailure()
	}
	if err := refresh.deliveries.reserveProof(entry, input.ModelAttestation); err != nil {
		refresh.deliveries.remove(entry)
		return nil
	}
	hostReasons, eventID, fail := normalizeRefreshInput(input)
	if fail != nil {
		return failResult(fail)
	}
	refresh.mu.Lock()
	ctx := entry.ctx
	ctx.Assessment = refresh.evaluate(input, ctx.Receipt, entry.semantic, hostReasons, eventID)
	delivery, err := renderOverviewDelivery(ctx, input, entry.tokens)
	refresh.mu.Unlock()
	if err != nil {
		return errResult(errBadArgs, err.Error(), "")
	}
	if err := entry.guard.validate(root, loaded); err != nil {
		refresh.deliveries.remove(entry)
		return overviewSnapshotFailure()
	}
	if err := rewriteVolumeCursor(&delivery, entry); err != nil {
		return errResult(errInternal, err.Error(), "")
	}
	if deliveryCompleted(delivery, entry) {
		refresh.deliveries.completed(entry)
	}
	refresh.mu.Lock()
	refresh.noteDeliveryChain(&delivery, ctx.Prepared.body.Receipt.BodySHA256, false, false, ctx.Assessment.RefreshReasons, ctx.EstimatedTokens)
	refresh.recordAttestedDelivery(ctx.Receipt, delivery.Attestation, entry.semantic.GovernanceAligned)
	refresh.mu.Unlock()
	ledger.Append(root, loaded.cfg.LedgerEnabled, delivery.Facts.ledgerEvent(time.Since(start).Milliseconds()))
	recordOverviewProofEvents(root, loaded.cfg.LedgerEnabled, input, delivery, ctx.Prepared.body)
	recordCapacityEvents(root, loaded.cfg.LedgerEnabled, ctx.ScopeIdentity, ctx.Assessment.Interruption, nil, overviewDeliveryPlan{}, nil)
	return overviewWorkResult(delivery, overviewWork{})
}
