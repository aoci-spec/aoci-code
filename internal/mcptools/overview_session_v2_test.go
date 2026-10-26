package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func chunkSessionRepo(t *testing.T) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	names := make([]string, 120)
	for i := range names {
		names[i] = fmt.Sprintf("entry-%03d.go", i)
	}
	writeVolumeAttestationEntries(t, root, names)
	return root
}

func sessionOverview(t *testing.T, root string, session *cognitionRefreshSession, owner string, input overviewIn) *mcp.CallToolResult {
	t.Helper()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return failResult(fail)
	}
	return handleVolumeOverview(root, "session-test", input, loaded, session, time.Now(), volumeDeliveryOwner{direct: owner})
}

func chunkMetadata(t *testing.T, result *mcp.CallToolResult) (map[string]any, string) {
	t.Helper()
	if result.IsError {
		t.Fatalf("overview failed: %s", resText(t, result))
	}
	prefix, body, ok := strings.Cut(resText(t, result), "\n"+overviewChunkBodyMarker+"\n")
	if !ok {
		t.Fatalf("missing chunk: %s", prefix)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(prefix), &meta); err != nil {
		t.Fatal(err)
	}
	return meta, body
}

func workCounters(t *testing.T, result *mcp.CallToolResult) overviewWork {
	t.Helper()
	data, err := json.Marshal(result.GetMeta()["aoci_work"])
	if err != nil {
		t.Fatal(err)
	}
	var work overviewWork
	if err := json.Unmarshal(data, &work); err != nil {
		t.Fatal(err)
	}
	return work
}

func TestOverviewV2CanonicalChunksAndWork(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail)
	}
	view, err := loaded.set.Scope("all")
	if err != nil {
		t.Fatal(err)
	}
	body, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	ctx := overviewRenderContext{Root: root, EffectiveScope: view.EffectiveScope, ScopeIdentity: view.ScopeIdentity, Content: body, EntryCount: view.ObjectCount, Sequence: sequence}
	expected := frameOverviewBody(root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	var canonical strings.Builder
	cursor := ""
	for ordinal := 1; ordinal < 20; ordinal++ {
		result := sessionOverview(t, root, session, "host-a", overviewIn{Scope: "all", Cursor: cursor})
		meta, chunk := chunkMetadata(t, result)
		want := overviewWork{}
		if ordinal == 1 {
			want = overviewWork{GovernanceEvals: 1, BodyBuilds: 1}
		}
		if ordinal == 1 && meta["body_sha256"] != expected.Receipt.BodySHA256 {
			t.Fatal("body identity changed")
		}
		if work := workCounters(t, result); work != want {
			t.Fatalf("chunk %d work=%+v want=%+v", ordinal, work, want)
		}
		if int(meta["chunk_index"].(float64)) != ordinal {
			t.Fatal("chunk order changed")
		}
		legacy := cursor
		if cursor != "" {
			parts := strings.Split(cursor, ":")
			legacy = strings.Join(parts[2:6], ":")
		}
		original, err := renderOverviewDelivery(ctx, overviewIn{Cursor: legacy}, loaded.cfg.OverviewDelivery.ChunkTokens)
		if err != nil {
			t.Fatal(err)
		}
		_, expectedChunk := chunkMetadata(t, textResult(original.Output))
		if chunk != expectedChunk {
			t.Fatal("chunk bytes/boundaries changed")
		}
		canonical.WriteString(chunk)
		if meta["completed"].(bool) {
			if canonical.String() != expected.Text {
				t.Fatal("canonical body changed")
			}
			return
		}
		cursor = meta["next_cursor"].(string)
		if !strings.HasPrefix(cursor, "v2:") {
			t.Fatal("not a session cursor")
		}
	}
	t.Fatal("never completed")
}

func TestOverviewV2RejectsInputDrift(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"source same size same mtime": func(t *testing.T, root string) {
			path := filepath.Join(root, "entry-000.go")
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			writeVolumeTestFile(t, root, "entry-000.go", "package changed\n")
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
		},
		"membership add": func(t *testing.T, root string) { writeVolumeTestFile(t, root, "fresh.go", "package fresh\n") },
		"membership remove": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "entry-000.go")); err != nil {
				t.Fatal(err)
			}
		},
		"index": func(t *testing.T, root string) {
			p := filepath.Join(root, "aoci.code.txt")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			writeVolumeTestFile(t, root, "aoci.code.txt", string(b)+"# drift\n")
		},
		"config": func(t *testing.T, root string) {
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.CognitionRefreshThreshold++
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
		},
		"baseline": func(t *testing.T, root string) {
			p := filepath.Join(root, ".aoci/baseline.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			writeVolumeTestFile(t, root, ".aoci/baseline.json", string(b)+" \n")
		},
		"recovery": func(t *testing.T, root string) {
			writeVolumeTestFile(t, root, ".aoci/transactions/entries-pending.json", "{}\n")
		},
		"atomic intent": func(t *testing.T, root string) {
			writeVolumeTestFile(t, root, "aoci.code.txt.aoci-cas.intent", "pending")
		},
		"curation": func(t *testing.T, root string) { writeVolumeTestFile(t, root, ".aoci/curation.json", "{}\n") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := chunkSessionRepo(t)
			session := newCognitionRefreshSession()
			meta, _ := chunkMetadata(t, sessionOverview(t, root, session, "owner", overviewIn{}))
			mutate(t, root)
			result := sessionOverview(t, root, session, "owner", overviewIn{Cursor: meta["next_cursor"].(string)})
			if !result.IsError {
				t.Fatal("stale continuation served")
			}
		})
	}
}

func TestOverviewV2GuardBindsInitialSnapshotAndObserveBytes(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail)
	}
	metadata, err := deliveryMetadata(root, loaded)
	if err != nil {
		t.Fatal(err)
	}
	_, _, state, fail := inspectVolumeGovernanceWithSnapshot(root, loaded)
	if fail != nil {
		t.Fatal(fail)
	}
	guard := newOverviewSourceGuard(loaded, metadata, state)
	if guard == nil || guard.validate(root, loaded) != nil {
		t.Fatal("aligned snapshot not reusable")
	}
	writeVolumeTestFile(t, root, "notes.txt", "observe-only changed notes\n")
	if guard.validate(root, loaded) == nil {
		t.Fatal("observe drift between assessment and publish escaped")
	}
}

func TestOverviewV2IsolationIntegrityRestartAndEviction(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	a, _ := chunkMetadata(t, sessionOverview(t, root, session, "a", overviewIn{}))
	b, _ := chunkMetadata(t, sessionOverview(t, root, session, "b", overviewIn{Scope: "code"}))
	cursor := a["next_cursor"].(string)
	for name, input := range map[string]overviewIn{"foreign scope": {Scope: "code", Cursor: cursor}, "modified": {Cursor: cursor[:len(cursor)-1] + "x"}} {
		if !sessionOverview(t, root, session, "a", input).IsError {
			t.Fatal(name + " accepted")
		}
	}
	if !sessionOverview(t, root, session, "b", overviewIn{Cursor: cursor}).IsError {
		t.Fatal("foreign owner accepted")
	}
	if !sessionOverview(t, root, newCognitionRefreshSession(), "a", overviewIn{Cursor: cursor}).IsError {
		t.Fatal("restart accepted ephemeral token")
	}
	var wg sync.WaitGroup
	for _, item := range []struct{ owner, scope, cursor string }{{"a", "all", cursor}, {"b", "code", b["next_cursor"].(string)}} {
		wg.Add(1)
		go func(owner, scope, cursor string) {
			defer wg.Done()
			result := sessionOverview(t, root, session, owner, overviewIn{Scope: scope, Cursor: cursor})
			meta, _ := chunkMetadata(t, result)
			if meta["chunk_index"].(float64) != 2 {
				t.Error("position contaminated")
			}
		}(item.owner, item.scope, item.cursor)
	}
	wg.Wait()
	// No state reconstruction for eviction; a caller must start fresh.
	session.deliveries.maxCount = 1
	chunkMetadata(t, sessionOverview(t, root, session, "c", overviewIn{}))
	if !sessionOverview(t, root, session, "a", overviewIn{Cursor: cursor}).IsError {
		t.Fatal("evicted session accepted")
	}
	// V1 continues to use the documented reconstructable live-validation path.
	parts := strings.Split(cursor, ":")
	legacy := strings.Join(parts[2:6], ":")
	result := sessionOverview(t, root, newCognitionRefreshSession(), "a", overviewIn{Cursor: legacy})
	chunkMetadata(t, result)
	if work := workCounters(t, result); work != (overviewWork{GovernanceEvals: 2, BodyBuilds: 1}) {
		t.Fatalf("legacy path changed: %+v", work)
	}
}

func TestOverviewV2StoreBoundsAndExpiry(t *testing.T) {
	store := newVolumeDeliveryStore()
	now := time.Unix(1, 0)
	store.now = func() time.Time { return now }
	store.maxCount = 2
	store.maxBytes = 20
	a := &volumeDelivery{id: "a", owner: volumeDeliveryOwner{direct: "owner"}, bytes: 11}
	b := &volumeDelivery{id: "b", owner: volumeDeliveryOwner{direct: "owner"}, bytes: 11}
	if store.put(a) != nil || store.put(b) != nil {
		t.Fatal("put failed")
	}
	if store.lookup("a", volumeDeliveryOwner{direct: "owner"}) != nil {
		t.Fatal("byte cap not enforced")
	}
	if store.put(&volumeDelivery{id: "large", bytes: 21}) == nil {
		t.Fatal("oversize accepted")
	}
	now = now.Add(time.Duration(machinecontract.OverviewDeliverySessionIdleMillis+1) * time.Millisecond)
	if store.lookup("b", volumeDeliveryOwner{direct: "owner"}) != nil || store.bytes != 0 {
		t.Fatal("expired state retained")
	}
}

func TestOverviewV2MCPHostIsolation(t *testing.T) {
	root := chunkSessionRepo(t)
	server, err := NewServer(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *mcp.ClientSession {
		ct, st := mcp.NewInMemoryTransports()
		ss, err := server.Connect(context.Background(), st, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(context.Background(), ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}
	a, b := connect(), connect()
	result, err := a.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := chunkMetadata(t, result)
	foreign, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{"cursor": meta["next_cursor"]}})
	if err != nil {
		t.Fatal(err)
	}
	if !foreign.IsError {
		t.Fatal("foreign SDK session accepted cursor")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	c := connect()
	foreign, err = c.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{"cursor": meta["next_cursor"]}})
	if err != nil {
		t.Fatal(err)
	}
	if !foreign.IsError {
		t.Fatal("replacement connection inherited closed Host delivery")
	}
}

func TestOverviewV2CompactionConsumesGenerationAfterPartialProof(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	session := newCognitionRefreshSession()
	result := sessionOverview(t, root, session, "host", overviewIn{Scope: "all", RefreshReasons: []string{"context_compaction"}, RefreshEventID: "compaction-test"})
	if result.IsError {
		t.Fatal(resText(t, result))
	}
	args := volumeAttestationArguments(t, root, resText(t, result), false)
	report := args["model_cognition_attestation"].(map[string]any)
	report["system_mastery_percent"] = 60
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var input overviewIn
	if err := json.Unmarshal(encoded, &input); err != nil {
		t.Fatal(err)
	}
	result = sessionOverview(t, root, session, "host", input)
	if result.IsError || !strings.Contains(resText(t, result), "cognition_assimilation: partial") {
		t.Fatalf("partial proof failed: %s", resText(t, result))
	}
	if !session.established || session.generation != 1 || len(session.pendingEvents) != 0 || len(session.pendingReasons) != 0 {
		t.Fatalf("refresh generation not consumed: %+v", session)
	}
	if receiptReliable(session.lastReceipt) {
		t.Fatal("partial proof upgraded reliable cognition")
	}
	if work := workCounters(t, result); work != (overviewWork{}) {
		t.Fatalf("proof reran expensive work: %+v", work)
	}
}

func TestOverviewV2FallbackForDatabaseTimeDependentGovernance(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	result := sessionOverview(t, root, newCognitionRefreshSession(), "host", overviewIn{})
	if result.IsError {
		t.Fatal(resText(t, result))
	}
	if work := workCounters(t, result); work != (overviewWork{GovernanceEvals: 2, BodyBuilds: 1}) {
		t.Fatalf("database bypassed live governance: %+v", work)
	}
}

func TestOverviewV2AbsoluteTTLAndCountBound(t *testing.T) {
	store := newVolumeDeliveryStore()
	now := time.Unix(1, 0)
	store.now = func() time.Time { return now }
	store.maxCount = 2
	store.maxBytes = 1000
	for _, id := range []string{"a", "b", "c"} {
		if err := store.put(&volumeDelivery{id: id, owner: volumeDeliveryOwner{direct: "host"}, bytes: 1}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if len(store.entries) != 2 || store.lookup("a", volumeDeliveryOwner{direct: "host"}) != nil {
		t.Fatal("count cap failed")
	}
	for i := 0; i < 7; i++ {
		now = now.Add(10 * time.Minute)
		entry := store.lookup("c", volumeDeliveryOwner{direct: "host"})
		if i < 5 && entry == nil {
			t.Fatal("premature expiry")
		}
		if i == 6 && entry != nil {
			t.Fatal("active replay defeated lifetime cap")
		}
	}
}

func TestOverviewV2CurrentMainInventoryMutations(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"repository initialization": func(t *testing.T, root string) { runFixtureGit(t, root, "init", "-q") },
		"tracked status":            func(t *testing.T, root string) { runFixtureGit(t, root, "add", "entry-000.go") },
		"ignored status":            func(t *testing.T, root string) { writeVolumeTestFile(t, root, ".git/info/exclude", "entry-000.go\n") },
		"submodule boundary": func(t *testing.T, root string) {
			runFixtureGit(t, root, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,module")
		},
		"unsafe boundary": func(t *testing.T, root string) {
			if err := os.Symlink("entry-000.go", filepath.Join(root, "boundary.go")); err != nil {
				t.Fatal(err)
			}
		},
		"root exclusion": func(t *testing.T, root string) {
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ExcludeRootDirs = append(cfg.ExcludeRootDirs, "new-artifacts")
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := chunkSessionRepo(t)
			if name != "repository initialization" {
				runFixtureGit(t, root, "init", "-q")
			}
			session := newCognitionRefreshSession()
			meta, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{}))
			mutate(t, root)
			if !sessionOverview(t, root, session, "host", overviewIn{Cursor: meta["next_cursor"].(string)}).IsError {
				t.Fatal("stale inventory accepted")
			}
		})
	}
}

func TestOverviewV2DuplicateContinuationIsImmutable(t *testing.T) {
	root := chunkSessionRepo(t)
	session := newCognitionRefreshSession()
	meta, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{}))
	input := overviewIn{Cursor: meta["next_cursor"].(string)}
	const clients = 8
	results := make(chan *mcp.CallToolResult, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- sessionOverview(t, root, session, "host", input) }()
	}
	wg.Wait()
	close(results)
	var body, next string
	for result := range results {
		metadata, chunk := chunkMetadata(t, result)
		if metadata["chunk_index"].(float64) != 2 || workCounters(t, result) != (overviewWork{}) {
			t.Fatal("duplicate consumed mutable position or rebuilt body")
		}
		if body == "" {
			body, next = chunk, metadata["next_cursor"].(string)
		}
		if chunk != body || metadata["next_cursor"] != next {
			t.Fatal("duplicate continuation differs")
		}
	}
}

func TestOverviewV2RetainedProofBudget(t *testing.T) {
	store := newVolumeDeliveryStore()
	store.maxBytes = 10000
	a := &volumeDelivery{id: "a", bytes: 3000}
	b := &volumeDelivery{id: "b", bytes: 3000}
	if store.put(a) != nil || store.put(b) != nil {
		t.Fatal("setup failed")
	}
	report := &overviewModelAttestation{Version: "proof"}
	if err := store.reserveProof(a, report); err != nil {
		t.Fatal(err)
	}
	if len(store.entries) != 1 || store.entries[a.id] != a || a.proofBytes == 0 || store.bytes != a.bytes || store.bytes > store.maxBytes {
		t.Fatal("proof growth bypassed budget or evicted its own delivery")
	}
	before := store.bytes
	if err := store.reserveProof(a, report); err != nil || store.bytes != before {
		t.Fatal("replacement did not preserve high-water accounting")
	}
	report.UncertaintyReasons = []string{strings.Repeat("x", 10000)}
	if store.reserveProof(a, report) == nil || store.bytes != before {
		t.Fatal("oversize proof accepted or corrupted accounting")
	}
	store.remove(a)
	if store.bytes != 0 {
		t.Fatal("proof bytes leaked after eviction")
	}
	if store.reserveProof(a, &overviewModelAttestation{}) == nil {
		t.Fatal("evicted delivery accepted retained proof")
	}
}

func TestOverviewV2ProofActivityHonorsIdleAndAbsoluteTTL(t *testing.T) {
	store := newVolumeDeliveryStore()
	now := time.Unix(1, 0)
	store.now = func() time.Time { return now }
	owner := volumeDeliveryOwner{direct: "host"}
	entry := &volumeDelivery{id: "active-proof", owner: owner, bytes: 1, ctx: overviewRenderContext{ScopeIdentity: "scope"}}
	if err := store.put(entry); err != nil {
		t.Fatal(err)
	}
	store.completed(entry)
	for i := 0; i < 7; i++ {
		now = now.Add(10 * time.Minute)
		got := store.proof(owner, "scope", "")
		if i < 6 && got != entry {
			t.Fatal("active proof lookup failed idle TTL")
		}
		if i == 6 && got != nil {
			t.Fatal("proof lookup defeated absolute TTL")
		}
	}
}

func TestOverviewV2ManagedExistingUnsafeBoundaries(t *testing.T) {
	for _, kind := range []string{"symlink", "submodule"} {
		t.Run(kind, func(t *testing.T) {
			root := buildObservedVolumeRepo(t)
			runFixtureGit(t, root, "init", "-q")
			if kind == "symlink" {
				if err := os.Symlink("main.go", filepath.Join(root, "boundary.go")); err != nil {
					t.Fatal(err)
				}
				runFixtureGit(t, root, "add", "boundary.go")
			} else {
				runFixtureGit(t, root, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,module")
			}
			loaded, fail := loadCognitionCtx(root)
			if fail != nil {
				t.Fatal(fail)
			}
			metadata, err := deliveryMetadata(root, loaded)
			if err != nil {
				t.Fatal(err)
			}
			_, _, state, fail := inspectVolumeGovernanceWithSnapshot(root, loaded)
			if fail != nil {
				t.Fatal(fail)
			}
			if state.Legacy {
				t.Fatal("test must cover Managed Scope")
			}
			guard := newOverviewSourceGuard(loaded, metadata, state)
			if guard == nil {
				t.Fatal("supported snapshot rejected")
			}
			if err := guard.validate(root, loaded); err != nil {
				t.Fatalf("unchanged existing %s boundary rejected: %v", kind, err)
			}
			if err := os.Symlink("main.go", filepath.Join(root, "another-boundary.go")); err != nil {
				t.Fatal(err)
			}
			if guard.validate(root, loaded) == nil {
				t.Fatal("boundary drift accepted")
			}
		})
	}
}
