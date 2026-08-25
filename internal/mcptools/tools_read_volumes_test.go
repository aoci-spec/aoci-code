package mcptools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func buildVolumeRepo(t *testing.T, includeCode, includeDatabase bool) string {
	t.Helper()
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	declarations := []string{
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-",
	}
	if includeCode {
		declarations = append(declarations, "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta")
	}
	if includeDatabase {
		declarations = append(declarations, "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta")
	}
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: MCP fixture\n" + strings.Join(declarations, "\n") + "\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", rootText)
	write("aoci.meta.txt", metaText)
	if includeCode {
		write("main.go", "package main\n")
		write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\nmain.go[CD9S]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep execution deterministic\n")
	}
	if includeDatabase {
		write("aoci.database.txt", cognition.DatabaseMarker+"\n===Primary tables/database://primary/public/===\nusers[DB9S]: F:store canonical user account state | R:tenants,user_profiles | A:user_id,UserRepository,AuthService | S:Hard deletion is forbidden because retained ownership records require the identity\n")
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func callVolumeTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return resText(t, result)
}

func TestVolumeOverviewScopesAndReceiptV2(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	session := connectMCPClient(t, root)
	for _, test := range []struct {
		scope  string
		want   []string
		forbid []string
		full   bool
	}{
		{"project", nil, []string{"main.go[CD9S]", "users[DB9S]"}, false},
		{"meta", []string{cognition.MetaVolumeMarker, "requested_scope: meta"}, []string{"main.go[CD9S]", "users[DB9S]"}, false},
		{"code", []string{"main.go[CD9S]", "\"requested_scope\":\"code\"", "\"id\":\"code\""}, []string{"users[DB9S]"}, false},
		{"database", []string{"users[DB9S]", "object_count=1", "\"id\":\"database\""}, []string{"main.go[CD9S]"}, false},
		{"all", []string{"main.go[CD9S]", "users[DB9S]", "model_full_cognition_reliable: false", "host_delivery_status: host_delivery_unconfirmed"}, nil, false},
	} {
		t.Run(test.scope, func(t *testing.T) {
			output := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": test.scope})
			if !strings.Contains(output, "layout_mode: volumes-v1") || !strings.Contains(output, "requested_scope: "+test.scope) || !strings.Contains(output, "cognition_receipt_v2:") || !strings.Contains(output, "\"version\":2") {
				t.Fatalf("missing receipt v2 metadata:\n%s", output)
			}
			for _, want := range test.want {
				if !strings.Contains(output, want) {
					t.Fatalf("%s missing %q:\n%s", test.scope, want, output)
				}
			}
			for _, forbidden := range test.forbid {
				if strings.Contains(output, forbidden) {
					t.Fatalf("%s leaked %q", test.scope, forbidden)
				}
			}
			if strings.Contains(output, "model_full_cognition_reliable: true") != test.full {
				t.Fatalf("wrong full reliability for %s", test.scope)
			}
		})
	}
}

func TestVolumeAbsentScopeIsSuccessfulEmptyCapability(t *testing.T) {
	root := buildVolumeRepo(t, false, false)
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_overview", map[string]any{"scope": "database"})
	for _, want := range []string{"scope_available: false", "asset_state: absent", "full_text_included: false", "model_scope_cognition_reliable: false", "\"scope_available\":false", "\"asset_state\":\"absent\"", "\"delivered_volumes\":[]"} {
		if !strings.Contains(output, want) {
			t.Fatalf("absent scope missing %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
		t.Fatal("absent scope created a database Volume")
	}
}

func TestVolumeOverviewNoArgumentMeansAll(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_overview", nil)
	for _, want := range []string{"requested_scope: all", "main.go[CD9S]", "users[DB9S]", "model_full_cognition_reliable: false", "host_delivery_status: host_delivery_unconfirmed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("no-argument Volumes Overview is not all; missing %q:\n%s", want, output)
		}
	}
}

func TestVolumeRepeatedExplicitOverviewAlwaysDeliversScopeBody(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	session := connectMCPClient(t, root)
	digests := make([]string, 0, 2)
	for call := 1; call <= 2; call++ {
		output := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": "database"})
		if !strings.Contains(output, "full_text_included: true") ||
			!strings.Contains(output, "users[DB9S]") ||
			strings.Contains(output, `"refresh_status":"refresh_not_required"`) {
			t.Fatalf("explicit database Overview call %d did not deliver the full scope:\n%s", call, output)
		}
		digests = append(digests, overviewMetadataValue(t, output, "challenge_digest"))
	}
	if digests[0] == "-" || digests[0] != digests[1] {
		t.Fatalf("repeated explicit Overview challenge drifted: %v", digests)
	}
}

func TestVolumeOverviewAcceptsModelCognitionAttestation(t *testing.T) {
	// An absent Database domain is a legal current state. Keeping this fixture
	// Code-only makes the successful Attestation depend on live governance
	// facts instead of an enabled Database Volume without accepted Evidence.
	root := buildVolumeRepo(t, true, false)
	session := connectMCPClient(t, root)
	first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": "all"})
	bodyBytes, err := strconv.Atoi(overviewMetadataValue(t, first, "body_utf8_bytes"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	_, sequence, sequenceErr := buildVolumeOverviewBody(view)
	if sequenceErr != nil {
		t.Fatal(sequenceErr)
	}
	challenge := buildOverviewChallenge(view.ScopeIdentity, sequence)
	output := callVolumeTool(t, session, "aoci_overview", map[string]any{
		"scope": "all",
		"host_delivery_confirmation": map[string]any{
			"version": overviewDeliveryReceiptV1, "body_sha256": overviewMetadataValue(t, first, "body_sha256"),
			"body_bytes": bodyBytes, "end_marker_observed": true,
		},
		"model_cognition_attestation": attestationMap(t, completeAttestation(
			challenge, view.ObjectCount, volumeScopeBytes(view)/3,
		)),
	})
	for _, want := range []string{
		"delivery_integrity: confirmed", "model_attestation: pass",
		"cognition_assimilation: complete", "model_full_cognition_reliable: true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("attested Volume Overview missing %q:\n%s", want, output)
		}
	}
}

func writeVolumeAttestationEntries(t *testing.T, root string, names []string) {
	t.Helper()
	var body strings.Builder
	body.WriteString(cognition.CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n")
	padding := strings.Repeat("x", 500)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "%s[CD9S]: F:own current attestation responsibility for %s | R:- | A:- | S:%s\n", name, name, padding)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
}

func attestCurrentVolumeIndex(t *testing.T, session *mcp.ClientSession) string {
	t.Helper()
	cursor := ""
	var aggregate strings.Builder
	var bodySHA, challengeIndexSHA, sequenceSHA, challengeDigest string
	var bodyBytes, entryCount, estimatedTokens, challengeEntryCount int
	var challengeOrdinals []int
	chunkCount := 0
	for call := 1; call <= 10; call++ {
		arguments := map[string]any{"scope": "all"}
		if cursor != "" {
			arguments["cursor"] = cursor
		}
		output := callVolumeTool(t, session, "aoci_overview", arguments)
		parts := strings.SplitN(output, "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("current Volume read call %d did not return a Chunk", call)
		}
		var receipt struct {
			ChunkIndex        int    `json:"chunk_index"`
			ChunkCount        int    `json:"chunk_count"`
			NextCursor        string `json:"next_cursor"`
			Completed         bool   `json:"completed"`
			BodySHA           string `json:"body_sha256"`
			BodyBytes         int    `json:"body_utf8_bytes"`
			EntryCount        int    `json:"entry_count"`
			EstimatedTokens   int    `json:"estimated_tokens"`
			ChallengeIndexSHA string `json:"challenge_index_sha256"`
			ChallengeSequence string `json:"challenge_entry_sequence_sha256"`
			ChallengeEntries  int    `json:"challenge_entry_count"`
			ChallengeDigest   string `json:"challenge_digest"`
			ChallengeOrdinals []int  `json:"challenge_ordinals"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ChunkIndex != call {
			t.Fatalf("current Volume Chunk order changed: %+v", receipt)
		}
		chunkCount = receipt.ChunkCount
		aggregate.WriteString(parts[1])
		if call == 1 {
			bodySHA, bodyBytes = receipt.BodySHA, receipt.BodyBytes
			entryCount, estimatedTokens = receipt.EntryCount, receipt.EstimatedTokens
		}
		if receipt.Completed {
			challengeIndexSHA = receipt.ChallengeIndexSHA
			sequenceSHA = receipt.ChallengeSequence
			challengeEntryCount = receipt.ChallengeEntries
			challengeDigest = receipt.ChallengeDigest
			challengeOrdinals = receipt.ChallengeOrdinals
			break
		}
		cursor = receipt.NextCursor
	}
	if chunkCount != 2 || len(challengeOrdinals) != 1 || challengeEntryCount != entryCount {
		t.Fatalf("current Volume read did not preserve the 52-Entry two-Chunk Challenge: chunks=%d entries=%d challenge=%d ordinals=%v", chunkCount, entryCount, challengeEntryCount, challengeOrdinals)
	}
	if aggregate.Len() != bodyBytes {
		t.Fatalf("aggregate body bytes=%d want=%d", aggregate.Len(), bodyBytes)
	}
	digest := sha256.Sum256([]byte(aggregate.String()))
	if fmt.Sprintf("%x", digest[:]) != bodySHA {
		t.Fatal("current Volume aggregate body SHA mismatch")
	}

	entries := make([]*index.Entry, 0, entryCount)
	for lineNumber, line := range strings.Split(aggregate.String(), "\n") {
		entry, ok := index.ParseEntryLine(line, lineNumber+1)
		if ok && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			entries = append(entries, entry)
		}
	}
	if len(entries) != entryCount {
		t.Fatalf("delivered current Entry sequence=%d want=%d", len(entries), entryCount)
	}
	answers := make([]overviewChallengeAnswer, 0, len(challengeOrdinals))
	for _, ordinal := range challengeOrdinals {
		entry := entries[ordinal-1]
		answers = append(answers, overviewChallengeAnswer{
			// The delivered Code Entry exposes its exact repository-relative
			// path. The public Attestation contract permits that spelling; the
			// assessor binds it one-to-one to the canonical code: identity.
			Ordinal: ordinal, ObjectIdentity: entry.Filename,
			Tag: entry.TagsRaw, CoreF: entry.F,
		})
	}
	report := &overviewModelAttestation{
		Version: modelCognitionAttestationV1, IndexSHA256: challengeIndexSHA,
		EntrySequenceSHA256: sequenceSHA, EntryCount: challengeEntryCount,
		ChallengeDigest: challengeDigest, ReportedEntryCount: entryCount,
		ReportedEstimatedTokens: estimatedTokens, CoveragePercent: 100,
		SystemMasteryPercent: 92, ConfidencePercent: 95,
		UnseenSections: []string{}, UncertaintyReasons: []string{}, ChallengeAnswers: answers,
	}
	return callVolumeTool(t, session, "aoci_overview", map[string]any{
		"scope": "all",
		"host_delivery_confirmation": map[string]any{
			"version": overviewDeliveryReceiptV1, "body_sha256": bodySHA,
			"body_bytes": bodyBytes, "end_marker_observed": true,
		},
		"model_cognition_attestation": attestationMap(t, report),
	})
}

func TestVolumeAttestationPassesAfterCurrentEntryRemovalAndAddition(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 52)
	for index := range names {
		names[index] = fmt.Sprintf("file-%03d.go", index+1)
	}
	writeVolumeAttestationEntries(t, root, names)
	session := connectMCPClient(t, root)

	assertPass := func(stage, output string) {
		t.Helper()
		for _, want := range []string{
			"model_attestation: pass", "cognition_assimilation: complete", "challenge_passed: 1/1",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("%s missing %q:\n%s", stage, want, output)
			}
		}
	}
	assertPass("initial current Index", attestCurrentVolumeIndex(t, session))

	if err := os.Remove(filepath.Join(root, names[16])); err != nil {
		t.Fatal(err)
	}
	names = append(append([]string{}, names[:16]...), names[17:]...)
	writeVolumeAttestationEntries(t, root, names)
	assertPass("current Index after Entry removal", attestCurrentVolumeIndex(t, session))

	names = append(append([]string{}, names[:20]...), append([]string{"file-added.go"}, names[20:]...)...)
	writeVolumeAttestationEntries(t, root, names)
	assertPass("current Index after Entry addition", attestCurrentVolumeIndex(t, session))
}

func TestVolumeAllScopeChunkAndChallengeShareCanonicalSequence(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	loaded, fail := loadCognitionCtx(root)
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
	if len(sequence) != 2 || sequence[0].ObjectIdentity != "code:main.go" ||
		sequence[1].ObjectIdentity != "database://primary/public/users" {
		t.Fatalf("Volumes canonical sequence drifted: %+v", sequence)
	}
	challenge := buildOverviewChallenge(view.ScopeIdentity, sequence)
	if len(challenge.Ordinals) != 1 {
		t.Fatalf("default Challenge count=%d want=1", len(challenge.Ordinals))
	}
	for _, ordinal := range challenge.Ordinals {
		if challenge.Targets[ordinal].ObjectIdentity != sequence[ordinal-1].ObjectIdentity {
			t.Fatalf("Challenge ordinal %d diverged from canonical sequence", ordinal)
		}
	}
	framed := frameOverviewBody(root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	spans, err := planOverviewChunks(
		framed.Text, 4000, len(framed.Receipt.StartMarker)+1, sequence,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].FirstOrdinal != 1 || spans[0].LastOrdinal != len(sequence) {
		t.Fatalf("Chunk ordinal range diverged from canonical sequence: %+v", spans)
	}
}

func TestVolumeRulesAndAbsentLocalReads(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	session := connectMCPClient(t, root)
	rules := callVolumeTool(t, session, "aoci_rules", nil)
	if !strings.Contains(rules, "cognition_refresh_threshold:") {
		t.Fatalf("Volume Rules did not return the existing runtime contract:\n%s", rules)
	}
	search := resText(t, handleSearch(root, "test-version", searchIn{Keyword: "user", Scope: "database"}, nil))
	if !strings.Contains(search, "asset_state=absent") {
		t.Fatalf("absent database search was not explicit: %s", search)
	}
	entries := resText(t, handleGetEntries(root, "test-version", getEntriesIn{VolumeID: "database", ObjectRefs: []string{"database://primary/public/users"}}, nil))
	if !strings.Contains(entries, "asset_state=absent") {
		t.Fatalf("absent database object lookup was not explicit: %s", entries)
	}
}

func TestVolumeRulesProjectModuleIsStatelessAndWholeIndexCompatible(t *testing.T) {
	root := buildProjectModuleMCPRepo(t)
	session := connectMCPClient(t, root)

	plainBefore := callVolumeTool(t, session, "aoci_rules", nil)
	overviewBefore := callVolumeTool(t, session, "aoci_overview", nil)
	for _, want := range []string{"requested_scope: all", "effective_scope: all", "a.go[CD9S]", "b.go[CD9S]"} {
		if !strings.Contains(overviewBefore, want) {
			t.Fatalf("default Overview missing %q:\n%s", want, overviewBefore)
		}
	}

	firstOutput := callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/alpha"})
	first := decodeProjectModuleRules(t, firstOutput)
	if first.Version != "project-module-cognition/v1" || first.ModulePath != "internal/alpha" ||
		first.RootSHA256 == "" || first.MetaSHA256 == "" || first.CompositeIdentity == "" || first.ProjectionIdentity == "" ||
		len(first.Objects) != 1 || first.Objects[0].ObjectRef != "code:internal/alpha/a.go" || len(first.Relations) != 2 ||
		!first.Derived || first.Authoritative || first.Persisted || first.SourceBound || first.NetworkAccessed || first.BusinessDataRead {
		t.Fatalf("unexpected initial module projection: %#v", first)
	}
	repeatedOutput := callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/alpha"})
	if repeatedOutput != firstOutput || strings.Contains(repeatedOutput, "agent_memory_policy") ||
		strings.Contains(repeatedOutput, `"project_module_cognition"`) {
		t.Fatalf("repeated module projection was stateful or incomplete:\nfirst=%s\nrepeated=%s", firstOutput, repeatedOutput)
	}

	overviewAfter := callVolumeTool(t, session, "aoci_overview", nil)
	if strings.Contains(overviewAfter, "project-module-cognition/v1") ||
		overviewFact(overviewAfter, "body_sha256") != overviewFact(overviewBefore, "body_sha256") ||
		overviewFact(overviewAfter, "scope_identity") != overviewFact(overviewBefore, "scope_identity") {
		t.Fatalf("module state changed the default Whole-Index delivery:\nbefore=%s\nafter=%s", overviewBefore, overviewAfter)
	}
	afterOverview := callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/alpha"})
	if afterOverview != firstOutput {
		t.Fatalf("default full Overview changed stateless module output:\nbefore=%s\nafter=%s", firstOutput, afterOverview)
	}

	plainAfter := callVolumeTool(t, session, "aoci_rules", nil)
	if plainAfter != plainBefore {
		t.Fatalf("no-argument Rules output changed after module use:\nbefore=%s\nafter=%s", plainBefore, plainAfter)
	}
	afterPlain := callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/alpha"})
	if afterPlain != firstOutput {
		t.Fatalf("no-argument Rules changed stateless module output:\nbefore=%s\nafter=%s", firstOutput, afterPlain)
	}

	codePath := filepath.Join(root, "aoci.code.txt")
	codeBytes, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(codeBytes), "F:coordinate alpha behavior", "F:coordinate updated alpha behavior", 1)
	if changed == string(codeBytes) {
		t.Fatal("module fixture entry was not found")
	}
	if err := os.WriteFile(codePath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	current := decodeProjectModuleRules(t, callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/alpha"}))
	if current.ProjectionIdentity == first.ProjectionIdentity ||
		current.Objects[0].CanonicalEntry == first.Objects[0].CanonicalEntry {
		t.Fatalf("module call did not load the current projection: %#v", current)
	}
	other := decodeProjectModuleRules(t, callVolumeTool(t, session, "aoci_rules", map[string]any{"module_path": "internal/beta"}))
	if other.ModulePath != "internal/beta" || len(other.Objects) != 1 ||
		other.Objects[0].ObjectRef != "code:internal/beta/b.go" {
		t.Fatalf("different module did not return its complete projection: %#v", other)
	}

	for _, item := range []struct {
		modulePath string
		want       string
	}{{modulePath: "", want: "minLength"}, {modulePath: "../escape", want: "project_module_cognition_path_invalid"}} {
		invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "aoci_rules", Arguments: map[string]any{"module_path": item.modulePath},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !invalid.IsError || !strings.Contains(resText(t, invalid), item.want) {
			t.Fatalf("invalid module path %q did not fail closed: %s", item.modulePath, resText(t, invalid))
		}
	}
}

func TestRulesProjectModuleRejectsLegacyAndOversizedProjection(t *testing.T) {
	legacy := connectMCPClient(t, buildRepo(t))
	legacyResult, err := legacy.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_rules", Arguments: map[string]any{"module_path": "internal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !legacyResult.IsError || !strings.Contains(resText(t, legacyResult), "project_module_cognition_invalid") {
		t.Fatalf("Legacy module cognition did not fail explicitly: %s", resText(t, legacyResult))
	}

	root := buildVolumeRepo(t, true, false)
	var code strings.Builder
	code.WriteString(cognition.CodeVolumeMarker + "\n===" + filepath.ToSlash(root) + "/bulk/===\n")
	for index := 0; index <= maxDirEntries; index++ {
		fmt.Fprintf(&code, "f%02d.go[CD9S]: F:coordinate bulk module object %d | R:- | A:- | S:-\n", index, index)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(code.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := connectMCPClient(t, root)
	oversizedResult, err := oversized.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_rules", Arguments: map[string]any{"module_path": "bulk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !oversizedResult.IsError || !strings.Contains(resText(t, oversizedResult), "50") {
		t.Fatalf("oversized module projection did not fail closed: %s", resText(t, oversizedResult))
	}
}

func buildProjectModuleMCPRepo(t *testing.T) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	for _, path := range []string{"internal/alpha/a.go", "internal/beta/b.go"} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code := cognition.CodeVolumeMarker + "\n" +
		"===" + filepath.ToSlash(root) + "/===\n" +
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n" +
		"===" + filepath.ToSlash(root) + "/internal/alpha/===\n" +
		"a.go[CD9S]: F:coordinate alpha behavior | R:code:internal/beta/b.go | A:- | S:-\n" +
		"===" + filepath.ToSlash(root) + "/internal/beta/===\n" +
		"b.go[CD9S]: F:coordinate beta behavior | R:code:internal/alpha/a.go | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func decodeProjectModuleRules(t *testing.T, output string) cognition.ProjectModuleCognition {
	t.Helper()
	const marker = "AOCI Project Module Cognition JSON:\n"
	position := strings.LastIndex(output, marker)
	if position < 0 {
		t.Fatalf("module Rules output missing JSON marker:\n%s", output)
	}
	var projection cognition.ProjectModuleCognition
	if err := json.Unmarshal([]byte(strings.TrimSpace(output[position+len(marker):])), &projection); err != nil {
		t.Fatalf("decode module Rules projection: %v\n%s", err, output)
	}
	return projection
}

func overviewFact(output, key string) string {
	prefix := key + ": "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

func TestVolumeHeaderSearchAndGetEntries(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	header, fail := BuildHeaderText(root, "agent")
	if fail != nil || !strings.Contains(header, "AOCI Root") || !strings.Contains(header, "#[Tag dictionary: database]") || strings.Contains(header, "#Project: MCP fixture") {
		t.Fatalf("header did not return Meta authority: fail=%v\n%s", fail, header)
	}
	if meta := volumeFileText(t, root, "aoci.meta.txt"); !strings.HasSuffix(header, meta) {
		t.Fatalf("aoci_header altered the formal Meta contract:\nheader=%s\nmeta=%s", header, meta)
	}
	search := resText(t, handleSearch(root, "test-version", searchIn{Keyword: "canonical user", Scope: "database"}, nil))
	if !strings.Contains(search, "volume_id=database object_ref=database://primary/public/users") || strings.Contains(search, "main.go") {
		t.Fatalf("database search failed:\n%s", search)
	}
	entries := resText(t, handleGetEntries(root, "test-version", getEntriesIn{VolumeID: "database", ObjectRefs: []string{"database://primary/public/users"}}, nil))
	if !strings.Contains(entries, "users[DB9S]") || strings.Contains(entries, "Not indexed") {
		t.Fatalf("database object lookup failed:\n%s", entries)
	}
	invalid := handleGetEntries(root, "test-version", getEntriesIn{VolumeID: "database", ObjectRefs: []string{"users\nforged"}}, nil)
	if !invalid.IsError || !strings.Contains(resText(t, invalid), errBadArgs) {
		t.Fatalf("non-canonical database reference was accepted: %s", resText(t, invalid))
	}
	code := resText(t, handleGetEntries(root, "test-version", getEntriesIn{Paths: []string{"main.go"}}, nil))
	if !strings.Contains(code, "volume_id=code object_ref=code:main.go") {
		t.Fatalf("compatible code path lookup failed:\n%s", code)
	}
}

func TestVolumeFormalWriteEntrypointsFailBeforeMutation(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	rootBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if _, fail := planUpdateEntry(root, "main.go", "main.go[CD9S]: F:x | R:- | A:- | S:-"); fail == nil || fail.Code != errVolumeReadOnly {
		t.Fatalf("update did not fail closed: %#v", fail)
	}
	if _, fail := planRemoveEntry(root, "gone.go", false); fail == nil || fail.Code != errVolumeReadOnly {
		t.Fatalf("remove did not fail closed: %#v", fail)
	}
	report := handleReport(root, reportIn{Path: "main.go", Note: "fixture"})
	if !report.IsError || !strings.Contains(resText(t, report), errVolumeReadOnly) {
		t.Fatalf("report did not fail closed: %s", resText(t, report))
	}
	maintain := resText(t, handleMaintain(root))
	if !strings.Contains(maintain, errVolumeReadOnly) {
		t.Fatalf("maintain did not fail closed: %s", maintain)
	}
	rootAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if string(rootBefore) != string(rootAfter) {
		t.Fatal("Volume write guard modified Root")
	}
}

func TestReceiptV2UsesScopeIdentityForPartialReliability(t *testing.T) {
	left := cognitionReceipt{Version: 2, RuntimeRepositoryRoot: "/repo", MCPServiceVersion: "v", Scope: "database", LayoutMode: cognition.LayoutVolumesV1, RequestedScope: "database", EffectiveScope: "database", ScopeIdentity: "database-scope", CompositeIdentity: "whole-before"}
	right := left
	right.CompositeIdentity = "whole-after-code-change"
	if !receiptIdentityMatches(left, right) {
		t.Fatal("unrelated composite change invalidated a partial scope")
	}
	right.ScopeIdentity = "database-after"
	if receiptIdentityMatches(left, right) {
		t.Fatal("changed scope identity remained reliable")
	}
}
