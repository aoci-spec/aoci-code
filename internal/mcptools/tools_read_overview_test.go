// Overview交付元数据与Ledger事实专项测试。
package mcptools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func renderLegacyOverviewForTest(
	t *testing.T,
	root, serviceVersion, indexText string,
	document *index.Document,
	input overviewIn,
	chunkTokens int,
) overviewRendered {
	t.Helper()
	ctx := legacyRenderContextForTest(t, root, serviceVersion, indexText, document)
	rendered, err := renderOverviewDelivery(ctx, input, chunkTokens)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

func legacyRenderContextForTest(
	t *testing.T,
	root, serviceVersion, indexText string,
	document *index.Document,
) overviewRenderContext {
	t.Helper()
	sectionCount, entryCount := countOverviewDimensions(document)
	identity := newCognitionReceipt(root, serviceVersion, indexText, cognitionScopeRepositoryFull).IndexSHA256
	assessment := cognitionRefreshAssessment{
		RefreshStatus: machinecontract.RefreshStatusReadyForOverview,
		Semantic: semanticChangeFacts{
			Threshold:         machinecontract.CognitionRefreshThresholdDefault,
			GovernanceAligned: true,
		},
	}
	receipt := newCognitionReceipt(root, serviceVersion, indexText, cognitionScopeRepositoryFull)
	sequence, err := legacyOverviewSequence(document, indexText)
	if err != nil {
		t.Fatal(err)
	}
	return overviewRenderContext{
		Root: root, MCPServiceVersion: serviceVersion, LayoutMode: "legacy-monolithic",
		RequestedScope: cognitionScopeRepositoryFull, EffectiveScope: cognitionScopeRepositoryFull,
		ScopeIdentity: identity, Content: indexText, ContentBytes: len(indexText),
		EntryCount: entryCount, SectionCount: sectionCount, EstimatedTokens: len(indexText) / 3,
		Receipt: receipt, Assessment: assessment, Sequence: sequence,
	}
}

// TestOverviewDeliveryFullIsByteExactAndLightweight locks the small-index path
// to marker -> formal bytes -> marker without the former directory wrapper.
func TestOverviewDeliveryFullIsByteExactAndLightweight(t *testing.T) {
	root := buildRepo(t)
	mcpServiceVersion := "v9.8.7-runtime-test"

	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatalf(
			"loadRepoCtx失败: %+v",
			fail,
		)
	}

	rendered := renderLegacyOverviewForTest(t, root, mcpServiceVersion, repository.text, repository.doc, overviewIn{}, machinecontract.OverviewChunkTokensDefault)
	output, facts := rendered.Output, rendered.Facts

	if facts.DeliveryMode != overviewDeliveryFull ||
		!facts.FullTextIncluded ||
		facts.IndexSHA256 == "" {
		t.Fatalf(
			"小索引应完整交付: %+v",
			facts,
		)
	}

	if facts.IndexBytes != len(repository.text) ||
		facts.OutputBytes != len(output) ||
		facts.EstimatedTokens != len(repository.text)/3 {
		t.Fatalf(
			"完整交付计量不一致: %+v output=%d",
			facts,
			len(output),
		)
	}

	if facts.SectionCount != 1 ||
		facts.EntryCount != 1 {
		t.Fatalf(
			"目录段或条目计数不一致: %+v",
			facts,
		)
	}

	if len(facts.IndexSHA256) != 64 {
		t.Fatalf(
			"索引SHA长度应为64: %q",
			facts.IndexSHA256,
		)
	}

	for _, anchor := range []string{
		"AOCI Overview Metadata:",
		"runtime_repository_root: " + root,
		"mcp_service_version: " + mcpServiceVersion,
		"delivery_mode: full",
		"full_text_included: true",
		"cognition_state: uncertain",
		"index_sha256: " + facts.IndexSHA256,
		"section_count: 1",
		"entry_count: 1",
		"cognition_currency: current",
		"request_mode: full",
		"server_delivery_complete: true",
		"host_delivery_status: host_delivery_unconfirmed",
		"delivery_receipt_version: overview-delivery-receipt/v1",
		"<<<AOCI_OVERVIEW_BODY_BEGIN/v1 scope=repository_full>>>",
		"<<<AOCI_OVERVIEW_BODY_END/v1 scope=repository_full>>>",
		"a.go[X.Y.5.T]",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"完整交付缺少锚点%q:\n%s",
				anchor,
				output,
			)
		}
	}
	if !strings.Contains(output, "model_full_cognition_reliable: false") {
		t.Fatal("server delivery alone must not establish host reliability")
	}

	if !strings.Contains(output, repository.text) || facts.BodySHA256 == "" || facts.BodyUTF8Bytes == 0 {
		t.Fatal("framed delivery must contain byte-exact index text and a body receipt")
	}
	start := strings.LastIndex(output, "<<<AOCI_OVERVIEW_BODY_BEGIN/v1")
	end := strings.LastIndex(output, "<<<AOCI_OVERVIEW_BODY_END/v1")
	if start < 0 || end < start || !strings.Contains(output[start:end], repository.text) {
		t.Fatal("formal index was not delivered byte-exactly between markers")
	}
	if start/3 >= 600 || strings.Contains(output[:start], "A Layer distribution") ||
		strings.Contains(output[:start], "AOCI Index Overview") {
		t.Fatalf("Overview wrapper is not lightweight: %d estimated tokens", start/3)
	}

	event := facts.ledgerEvent(17)

	if event.Op != "overview" ||
		event.Source != ledger.SourceAgent ||
		event.DurationMs != 17 ||
		event.DeliveryMode != overviewDeliveryFull ||
		event.FullTextIncluded == nil ||
		!*event.FullTextIncluded ||
		event.IndexSHA256 != facts.IndexSHA256 ||
		event.IndexBytes != facts.IndexBytes ||
		event.OutputBytes != facts.OutputBytes ||
		event.EstimatedTokens != facts.EstimatedTokens ||
		event.SectionCount != 1 ||
		event.EntryCount != 1 || event.AOCIToolCalls != 1 || event.OverviewReads != 1 {
		t.Fatalf(
			"完整交付Ledger事件不一致: %+v",
			event,
		)
	}

}

func overviewMetadataValue(t *testing.T, output, key string) string {
	t.Helper()
	prefix := key + ": "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("overview metadata missing %s:\n%s", key, output)
	return ""
}

func hostConfirmationFromOverview(t *testing.T, output string) map[string]any {
	t.Helper()
	bodyBytes, err := strconv.Atoi(overviewMetadataValue(t, output, "body_utf8_bytes"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"version":             overviewDeliveryReceiptV1,
		"body_sha256":         overviewMetadataValue(t, output, "body_sha256"),
		"body_bytes":          bodyBytes,
		"end_marker_observed": true,
	}
}

func TestOverviewHostConfirmationIsRequiredForReliability(t *testing.T) {
	root := buildRepo(t)
	session := connectMCPClient(t, root)
	first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	firstText := resText(t, first)
	if !strings.Contains(firstText, "host_delivery_status: host_delivery_unconfirmed") ||
		!strings.Contains(firstText, "model_full_cognition_reliable: false") ||
		!strings.Contains(firstText, "cognition_level: 1") ||
		!strings.Contains(firstText, "cognition_level_state: index_loaded") {
		t.Fatalf("server-only delivery produced a reliability false positive:\n%s", firstText)
	}
	bodyBytes, err := strconv.Atoi(overviewMetadataValue(t, firstText, "body_utf8_bytes"))
	if err != nil {
		t.Fatal(err)
	}
	confirmation := map[string]any{
		"version": overviewDeliveryReceiptV1, "body_sha256": overviewMetadataValue(t, firstText, "body_sha256"),
		"body_bytes": bodyBytes, "end_marker_observed": true,
	}
	confirmed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{"host_delivery_confirmation": confirmation}})
	if err != nil {
		t.Fatal(err)
	}
	confirmedText := resText(t, confirmed)
	if !strings.Contains(confirmedText, "host_delivery_status: host_delivery_confirmed") ||
		!strings.Contains(confirmedText, "delivery_integrity: confirmed") ||
		!strings.Contains(confirmedText, "model_attestation: not_provided") ||
		!strings.Contains(confirmedText, "cognition_level: 2") ||
		!strings.Contains(confirmedText, "cognition_level_state: delivery_verified") ||
		!strings.Contains(confirmedText, "model_full_cognition_reliable: false") {
		t.Fatalf("valid host confirmation was not accepted:\n%s", confirmedText)
	}
	attested, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{
		"host_delivery_confirmation":  confirmation,
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	}})
	if err != nil {
		t.Fatal(err)
	}
	attestedText := resText(t, attested)
	if !strings.Contains(attestedText, "model_attestation: pass") ||
		!strings.Contains(attestedText, "cognition_assimilation: complete") ||
		!strings.Contains(attestedText, "cognition_level: 4") ||
		!strings.Contains(attestedText, "cognition_level_state: cognition_governed") ||
		!strings.Contains(attestedText, "model_full_cognition_reliable: true") {
		t.Fatalf("valid model attestation was not accepted:\n%s", attestedText)
	}
	confirmation["body_sha256"] = strings.Repeat("0", 64)
	incomplete, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{"host_delivery_confirmation": confirmation}})
	if err != nil {
		t.Fatal(err)
	}
	if incompleteText := resText(t, incomplete); !strings.Contains(incompleteText, "host_delivery_status: host_delivery_incomplete") ||
		!strings.Contains(incompleteText, "cognition_level: 1") {
		t.Fatal("mismatched host receipt was not rejected")
	}
}

func TestOverviewAttestationLedgerDoesNotCountAnotherBodyRead(t *testing.T) {
	facts := overviewDeliveryFacts{DeliveryMode: overviewDeliveryAttestation}
	event := facts.ledgerEvent(1)
	if event.OverviewReads != 0 || event.FullTextIncluded == nil || *event.FullTextIncluded {
		t.Fatalf("Attestation was counted as another Overview body read: %+v", event)
	}
}

func TestOverviewCompactionZeroChallengeContinuesSourceBoundWithoutRefreshLoop(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	initial, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	initialText := resText(t, initial)
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{
			"host_delivery_confirmation":  hostConfirmationFromOverview(t, initialText),
			"model_cognition_attestation": validLegacyAttestationMap(t, root),
		},
	}); err != nil {
		t.Fatal(err)
	}

	event := map[string]any{
		"refresh_reasons":  []string{machinecontract.RefreshReasonContextCompaction},
		"refresh_event_id": "zero-challenge-compaction",
	}
	refresh, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: event})
	if err != nil {
		t.Fatal(err)
	}
	refreshText := resText(t, refresh)
	event["host_delivery_confirmation"] = hostConfirmationFromOverview(t, refreshText)
	event["model_cognition_attestation"] = legacyAttestationMapWithWrongAnswers(t, root, 10)
	failed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: event})
	if err != nil {
		t.Fatal(err)
	}
	failedText := resText(t, failed)
	for _, want := range []string{
		"challenge_passed: 0/10",
		"model_full_cognition_reliable: false",
		"refresh_status: " + machinecontract.RefreshStatusNotRequired,
		"delivery_guidance: " + machinecontract.OverviewDeliveryGuidanceSourceBound,
	} {
		if !strings.Contains(failedText, want) {
			t.Fatalf("zero-challenge refresh missing %q:\n%s", want, failedText)
		}
	}

	check, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{
			"check_only":       true,
			"refresh_reasons":  []string{machinecontract.RefreshReasonContextCompaction},
			"refresh_event_id": "zero-challenge-compaction",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkText := resText(t, check); !strings.Contains(checkText, `"refresh_status":"refresh_not_required"`) {
		t.Fatalf("zero-challenge refresh looped: %s", checkText)
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_get_entries", Arguments: map[string]any{"paths": []string{"src/file-000.txt"}},
	}); err != nil {
		t.Fatalf("source-bound retrieval was prohibited after 0/10: %v", err)
	}
}

func TestOverviewPartialAttestationDisablesFullClaimButConsumesRefreshAttempt(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 12)
	session := connectMCPClient(t, root)
	first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	firstText := resText(t, first)
	bodyBytes, err := strconv.Atoi(overviewMetadataValue(t, firstText, "body_utf8_bytes"))
	if err != nil {
		t.Fatal(err)
	}
	confirmation := map[string]any{
		"version":             overviewDeliveryReceiptV1,
		"body_sha256":         overviewMetadataValue(t, firstText, "body_sha256"),
		"body_bytes":          bodyBytes,
		"end_marker_observed": true,
	}
	attested, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview",
		Arguments: map[string]any{
			"host_delivery_confirmation":  confirmation,
			"model_cognition_attestation": legacyAttestationMapWithWrongAnswers(t, root, 3),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	attestedText := resText(t, attested)
	for _, want := range []string{
		"delivery_integrity: confirmed",
		"challenge_passed: 7/10",
		"model_attestation: fail",
		"cognition_assimilation: uncertain",
		"cognition_level: 2",
		"cognition_level_state: delivery_verified",
		"cognition_level_message: ",
		"model_full_cognition_reliable: false",
		"delivery_guidance: " + machinecontract.OverviewDeliveryGuidanceSourceBound,
		"refresh_status: " + machinecontract.RefreshStatusNotRequired,
	} {
		if !strings.Contains(attestedText, want) {
			t.Fatalf("partial Attestation missing %q:\n%s", want, attestedText)
		}
	}

	check, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{"check_only": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint overviewCheckpointEnvelope
	checkText := resText(t, check)
	if err := json.Unmarshal([]byte(checkText), &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Assessment.RefreshStatus != machinecontract.RefreshStatusNotRequired ||
		checkpoint.Assessment.State != cognitionStateUncertain ||
		checkpoint.Assessment.Receipt.ModelFullReliable {
		t.Fatalf("partial Attestation retriggered or granted reliability: %s", checkText)
	}
}

func TestOverviewChunkCursorDetectsLossAndReassemblesBody(t *testing.T) {
	root := "/repo"
	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n===Sources/repo/src/===\n")
	for ordinal := 1; ordinal <= 900; ordinal++ {
		fmt.Fprintf(&source, "file-%04d.go[IM7S]: F:preserve complete object %04d with deterministic transport evidence | R:- | A:- | S:-\n", ordinal, ordinal)
	}
	document, warnings := index.Parse(source.String())
	if len(warnings) != 0 {
		t.Fatalf("synthetic index warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, root)
	cursor := ""
	var aggregate strings.Builder
	var aggregateSHA string
	chunkCount := 0
	identity := ""
	for calls := 0; calls < 20; calls++ {
		input := overviewIn{Cursor: cursor}
		rendered := renderLegacyOverviewForTest(t, root, "test", source.String(), document, input, 4000)
		parts := strings.SplitN(rendered.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("chunk body marker missing: %s", rendered.Output)
		}
		var metadata struct {
			DeliveryMode    string `json:"delivery_mode"`
			IndexSHA        string `json:"index_sha256"`
			BodySHA         string `json:"body_sha256"`
			NextCursor      string `json:"next_cursor"`
			ChunkIndex      int    `json:"chunk_index"`
			ChunkCount      int    `json:"chunk_count"`
			ChunkTokens     int    `json:"chunk_estimated_tokens"`
			Completed       bool   `json:"completed"`
			CompletedMarker bool   `json:"completed_marker"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.DeliveryMode != overviewDeliveryChunked {
			t.Fatalf("wrong chunk mode: %#v", metadata)
		}
		aggregate.WriteString(parts[1])
		if metadata.ChunkTokens > 4000 || metadata.ChunkIndex != calls+1 {
			t.Fatalf("invalid chunk receipt: %+v", metadata)
		}
		chunkCount = metadata.ChunkCount
		identity = metadata.IndexSHA
		if metadata.BodySHA != "" {
			aggregateSHA = metadata.BodySHA
		}
		if metadata.Completed {
			if !metadata.CompletedMarker || metadata.NextCursor != "" {
				t.Fatalf("final chunk receipt incomplete: %#v", metadata)
			}
			break
		}
		cursor = metadata.NextCursor
	}
	if chunkCount < 2 {
		t.Fatalf("expected multiple chunks, got %d", chunkCount)
	}
	digest := sha256.Sum256([]byte(aggregate.String()))
	if hex.EncodeToString(digest[:]) != aggregateSHA || !strings.Contains(aggregate.String(), "<<<AOCI_OVERVIEW_BODY_END/v1") {
		t.Fatal("chunked body did not reassemble to the aggregate receipt")
	}
	badCursor := identity + ":4000:1:" + strings.Repeat("0", 64)
	_, err := renderOverviewDelivery(
		legacyRenderContextForTest(t, root, "test", source.String(), document),
		overviewIn{Cursor: badCursor}, 4000,
	)
	if err == nil || !strings.Contains(err.Error(), "overview_cursor_out_of_order") {
		t.Fatalf("out-of-order cursor was accepted: %v", err)
	}
}

func largeOverviewFixture(t *testing.T) (string, *index.Document) {
	t.Helper()
	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n===Sources/repo/src/===\n")
	padding := strings.Repeat("x", 210)
	for ordinal := 1; ordinal <= 900; ordinal++ {
		fmt.Fprintf(&source, "file-%04d.go[IM7S]: F:preserve object %04d | R:- | A:- | S:%s\n", ordinal, ordinal, padding)
	}
	document, warnings := index.Parse(source.String())
	if len(warnings) != 0 {
		t.Fatalf("synthetic index warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, "/repo")
	return source.String(), document
}

func collectOverviewChunks(
	t *testing.T,
	content string,
	document *index.Document,
	chunkTokens int,
) (int, string) {
	t.Helper()
	_, expectedEntries := countOverviewDimensions(document)
	cursor := ""
	var aggregate strings.Builder
	chunkCount := 0
	lastOrdinal := 0
	for calls := 0; calls < 100; calls++ {
		rendered := renderLegacyOverviewForTest(
			t, "/repo", "test", content, document, overviewIn{Cursor: cursor}, chunkTokens,
		)
		if len(rendered.Output)/3 >= 25000 {
			t.Fatalf("visible response exceeds 25K estimate: tokens=%d budget=%d", len(rendered.Output)/3, chunkTokens)
		}
		parts := strings.SplitN(rendered.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("chunk body marker missing: %s", rendered.Output)
		}
		var receipt struct {
			ReceiptVersion    string `json:"receipt_version"`
			ChunkIndex        int    `json:"chunk_index"`
			ChunkCount        int    `json:"chunk_count"`
			FirstOrdinal      int    `json:"first_entry_ordinal"`
			LastOrdinal       int    `json:"last_entry_ordinal"`
			EstimatedTokens   int    `json:"chunk_estimated_tokens"`
			NextCursor        string `json:"next_cursor"`
			Completed         bool   `json:"completed"`
			CompletedMarker   bool   `json:"completed_marker"`
			Continuation      bool   `json:"continuation_required"`
			AttestationDigest string `json:"challenge_digest"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ReceiptVersion != overviewChunkReceiptV1 || receipt.ChunkIndex != calls+1 ||
			receipt.FirstOrdinal != lastOrdinal+1 || receipt.LastOrdinal < receipt.FirstOrdinal ||
			receipt.EstimatedTokens > chunkTokens {
			t.Fatalf("invalid continuous Chunk Receipt: %+v", receipt)
		}
		lastOrdinal = receipt.LastOrdinal
		chunkCount = receipt.ChunkCount
		aggregate.WriteString(parts[1])
		if receipt.Completed {
			if receipt.Continuation || receipt.NextCursor != "" || !receipt.CompletedMarker || receipt.AttestationDigest == "" {
				t.Fatalf("invalid final Chunk Receipt: %+v", receipt)
			}
			break
		}
		if !receipt.Continuation || receipt.NextCursor == "" || receipt.AttestationDigest != "" {
			t.Fatalf("invalid intermediate Chunk Receipt: %+v", receipt)
		}
		cursor = receipt.NextCursor
	}
	if lastOrdinal != expectedEntries {
		t.Fatalf("Entry chain ended at %d, want %d", lastOrdinal, expectedEntries)
	}
	return chunkCount, aggregate.String()
}

func TestOverviewEightKWholeIndexScaleUsesFormalEntryOrdinals(t *testing.T) {
	const entryCount = 638
	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n")
	for example := 1; example <= 3; example++ {
		fmt.Fprintf(&source, "#example-%d.go[EX9T]: F:Header calibration only | R:- | A:- | S:-\n", example)
	}
	source.WriteString("# Header comments, blank lines, and Section markers are not formal Entries.\n\n===Sources/repo/src/===\n")
	padding := strings.Repeat("x", 326)
	for ordinal := 1; ordinal <= entryCount; ordinal++ {
		fmt.Fprintf(&source, "file-%04d.go[IM7S]: F:preserve formal object %04d | R:- | A:- | S:%s\n", ordinal, ordinal, padding)
	}
	content := source.String()
	document, warnings := index.Parse(content)
	if len(warnings) != 0 {
		t.Fatalf("synthetic index warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, "/repo")
	sequence, err := legacyOverviewSequence(document, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(sequence) != entryCount || sequence[0].ObjectIdentity != "src/file-0001.go" ||
		sequence[entryCount/2-1].ObjectIdentity != "src/file-0319.go" ||
		sequence[entryCount-1].ObjectIdentity != "src/file-0638.go" {
		t.Fatalf("formal sequence is not exactly 1..%d: first=%+v last=%+v len=%d", entryCount, sequence[0], sequence[len(sequence)-1], len(sequence))
	}
	challenge := buildOverviewChallenge("formal-sequence", sequence)
	for _, ordinal := range challenge.Ordinals {
		want := fmt.Sprintf("src/file-%04d.go", ordinal)
		if challenge.Targets[ordinal].ObjectIdentity != want {
			t.Fatalf("Challenge ordinal %d resolved to %q, want %q", ordinal, challenge.Targets[ordinal].ObjectIdentity, want)
		}
	}
	estimatedTokens := len(content) / 3
	if estimatedTokens < 82_000 || estimatedTokens > 85_000 {
		t.Fatalf("fixture token scale = %d, want about 83.3k", estimatedTokens)
	}
	chunkCount, aggregate := collectOverviewChunks(t, content, document, 8000)
	if chunkCount != 11 {
		t.Fatalf("8K fixture chunk count = %d, want 11", chunkCount)
	}
	if strings.Count(aggregate, "#AOCI-CLI Complete Index") != 1 {
		t.Fatal("Header was not delivered exactly once")
	}
	for example := 1; example <= 3; example++ {
		if strings.Count(aggregate, fmt.Sprintf("#example-%d.go[", example)) != 1 {
			t.Fatalf("Header example %d was split or duplicated", example)
		}
	}
	for ordinal := 1; ordinal <= entryCount; ordinal++ {
		name := fmt.Sprintf("file-%04d.go[", ordinal)
		if strings.Count(aggregate, name) != 1 {
			t.Fatalf("formal Entry %d appeared %d times", ordinal, strings.Count(aggregate, name))
		}
	}
}

func TestOverviewBlackBoxFirstAttestationPassesFromDeliveredFormalSequence(t *testing.T) {
	const entryCount = 120
	root := buildRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OverviewDelivery.ChunkTokens = 4000
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n")
	for example := 1; example <= 3; example++ {
		fmt.Fprintf(&source, "#calibration-%d.go[EX9T]: F:Header example only | R:- | A:- | S:-\n", example)
	}
	source.WriteString("# comment\n\n===Sources" + filepath.ToSlash(root) + "/src/===\n")
	for ordinal := 1; ordinal <= entryCount; ordinal++ {
		name := fmt.Sprintf("file-%03d.go", ordinal)
		if ordinal == 1 {
			name = "a.go"
		}
		if err := os.WriteFile(filepath.Join(root, "src", name), []byte("package fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&source, "%s[IM7S]: F:own black-box responsibility %03d with complete delivery evidence | R:- | A:- | S:-\n", name, ordinal)
	}
	indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
	if err := os.WriteFile(indexPath, []byte(source.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}

	session := connectMCPClient(t, root)
	cursor := ""
	var aggregate strings.Builder
	var bodySHA string
	var bodyBytes, reportedEntries, reportedTokens int
	var challengeIndexSHA, challengeSequenceSHA, challengeDigest string
	var challengeEntryCount int
	var challengeOrdinals []int
	lastOrdinal := 0
	chunkCount := 0
	for call := 1; call <= 20; call++ {
		arguments := map[string]any{}
		if cursor != "" {
			arguments["cursor"] = cursor
		}
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(resText(t, result), "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("black-box call %d did not return a Chunk", call)
		}
		var receipt struct {
			ChunkIndex        int    `json:"chunk_index"`
			ChunkCount        int    `json:"chunk_count"`
			FirstOrdinal      int    `json:"first_entry_ordinal"`
			LastOrdinal       int    `json:"last_entry_ordinal"`
			NextCursor        string `json:"next_cursor"`
			Completed         bool   `json:"completed"`
			CompletedMarker   bool   `json:"completed_marker"`
			BodySHA           string `json:"body_sha256"`
			BodyBytes         int    `json:"body_utf8_bytes"`
			EntryCount        int    `json:"entry_count"`
			EstimatedTokens   int    `json:"estimated_tokens"`
			ChallengeDigest   string `json:"challenge_digest"`
			ChallengeIndexSHA string `json:"challenge_index_sha256"`
			ChallengeSequence string `json:"challenge_entry_sequence_sha256"`
			ChallengeEntries  int    `json:"challenge_entry_count"`
			ChallengeOrdinals []int  `json:"challenge_ordinals"`
			AttestationPrompt string `json:"attestation_prompt"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ChunkIndex != call || receipt.FirstOrdinal != lastOrdinal+1 || receipt.LastOrdinal < receipt.FirstOrdinal {
			t.Fatalf("non-canonical receipt ordinal at call %d: %+v", call, receipt)
		}
		lastOrdinal = receipt.LastOrdinal
		chunkCount = receipt.ChunkCount
		aggregate.WriteString(parts[1])
		if call == 1 {
			bodySHA, bodyBytes = receipt.BodySHA, receipt.BodyBytes
			reportedEntries, reportedTokens = receipt.EntryCount, receipt.EstimatedTokens
		}
		if receipt.Completed {
			if !receipt.CompletedMarker || receipt.NextCursor != "" {
				t.Fatalf("black-box final receipt incomplete: %+v", receipt)
			}
			challengeDigest = receipt.ChallengeDigest
			challengeIndexSHA = receipt.ChallengeIndexSHA
			challengeSequenceSHA = receipt.ChallengeSequence
			challengeEntryCount = receipt.ChallengeEntries
			challengeOrdinals = receipt.ChallengeOrdinals
			if !strings.Contains(receipt.AttestationPrompt, "1-based") || !strings.Contains(receipt.AttestationPrompt, "Header") {
				t.Fatalf("final receipt did not disclose the formal ordinal rule: %q", receipt.AttestationPrompt)
			}
			if receipt.AttestationPrompt != attestationPrompt() {
				t.Fatalf("Legacy final Chunk changed the Attestation Prompt bytes: %q", receipt.AttestationPrompt)
			}
			break
		}
		cursor = receipt.NextCursor
	}
	if chunkCount < 2 || lastOrdinal != entryCount || reportedEntries != entryCount || len(challengeOrdinals) != 10 {
		t.Fatalf("black-box chain facts mismatch: chunks=%d last=%d entries=%d challenge=%v", chunkCount, lastOrdinal, reportedEntries, challengeOrdinals)
	}
	if aggregate.Len() != bodyBytes {
		t.Fatalf("black-box body bytes = %d, want %d", aggregate.Len(), bodyBytes)
	}
	digest := sha256.Sum256([]byte(aggregate.String()))
	if hex.EncodeToString(digest[:]) != bodySHA || !strings.Contains(aggregate.String(), "<<<AOCI_OVERVIEW_BODY_END/v1") {
		t.Fatal("black-box body confirmation failed")
	}

	delivered, warnings := index.Parse(aggregate.String())
	if len(warnings) != 0 {
		t.Fatalf("delivered body warnings: %+v", warnings)
	}
	index.ResolveRelPaths(delivered, root)
	formalEntries := make([]*index.Entry, 0, entryCount)
	for _, section := range delivered.Sections {
		if section.AbsPath != "" {
			formalEntries = append(formalEntries, section.Entries...)
		}
	}
	if len(formalEntries) != entryCount {
		t.Fatalf("delivered formal Entry count = %d, want %d", len(formalEntries), entryCount)
	}
	answers := make([]overviewChallengeAnswer, 0, len(challengeOrdinals))
	for _, ordinal := range challengeOrdinals {
		entry := formalEntries[ordinal-1]
		answers = append(answers, overviewChallengeAnswer{
			Ordinal: ordinal, ObjectIdentity: entry.RelPath, Tag: entry.TagsRaw, CoreF: entry.F,
		})
	}
	report := &overviewModelAttestation{
		Version: modelCognitionAttestationV1, IndexSHA256: challengeIndexSHA,
		EntrySequenceSHA256: challengeSequenceSHA, EntryCount: challengeEntryCount,
		ChallengeDigest:    challengeDigest,
		ReportedEntryCount: reportedEntries, ReportedEstimatedTokens: reportedTokens,
		CoveragePercent: 100, SystemMasteryPercent: 94, ConfidencePercent: 94,
		UnseenSections: []string{}, UncertaintyReasons: []string{}, ChallengeAnswers: answers,
	}
	attested, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{
		"host_delivery_confirmation": map[string]any{
			"version": overviewDeliveryReceiptV1, "body_sha256": bodySHA,
			"body_bytes": bodyBytes, "end_marker_observed": true,
		},
		"model_cognition_attestation": attestationMap(t, report),
	}})
	if err != nil {
		t.Fatal(err)
	}
	attestedText := resText(t, attested)
	for _, want := range []string{
		"model_attestation: pass", "cognition_assimilation: complete",
		"challenge_passed: 10/10", "cognition_level: 4",
		"cognition_level_state: cognition_governed",
	} {
		if !strings.Contains(attestedText, want) {
			t.Fatalf("first black-box Attestation missing %q:\n%s", want, attestedText)
		}
	}
}

func TestOverviewChunkBudgetsPreserveWholeIndex(t *testing.T) {
	content, document := largeOverviewFixture(t)
	counts := map[int]int{}
	for _, chunkTokens := range []int{12000, 20000, 24000} {
		count, aggregate := collectOverviewChunks(t, content, document, chunkTokens)
		counts[chunkTokens] = count
		if strings.Count(aggregate, "<<<AOCI_OVERVIEW_BODY_BEGIN/v1") != 1 ||
			strings.Count(aggregate, "<<<AOCI_OVERVIEW_BODY_END/v1") != 1 {
			t.Fatalf("budget %d did not preserve unique body markers", chunkTokens)
		}
		for ordinal := 1; ordinal <= 900; ordinal++ {
			name := fmt.Sprintf("file-%04d.go[", ordinal)
			if strings.Count(aggregate, name) != 1 {
				t.Fatalf("budget %d delivered %s %d times", chunkTokens, name, strings.Count(aggregate, name))
			}
		}
	}
	if counts[20000] != 5 || counts[12000] <= counts[20000] || counts[24000] >= counts[20000] {
		t.Fatalf("unexpected Chunk counts: %+v", counts)
	}
}

func TestOverviewChunkCursorBindsSnapshotAndConfiguration(t *testing.T) {
	content, document := largeOverviewFixture(t)
	first := renderLegacyOverviewForTest(t, "/repo", "test", content, document, overviewIn{}, 20000)
	parts := strings.SplitN(first.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
	if len(parts) != 2 {
		t.Fatal("automatic first Chunk was not returned")
	}
	var receipt struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(parts[0]), &receipt); err != nil || receipt.NextCursor == "" {
		t.Fatalf("first Chunk cursor missing: err=%v receipt=%+v", err, receipt)
	}
	_, err := renderOverviewDelivery(
		legacyRenderContextForTest(t, "/repo", "test", content, document),
		overviewIn{Cursor: receipt.NextCursor}, 12000,
	)
	if err == nil || !strings.Contains(err.Error(), "overview_chunk_tokens_changed") {
		t.Fatalf("configuration change was accepted: %v", err)
	}

	changed := content + "# changed snapshot\n"
	changedDocument, _ := index.Parse(changed)
	index.ResolveRelPaths(changedDocument, "/repo")
	_, err = renderOverviewDelivery(
		legacyRenderContextForTest(t, "/repo", "test", changed, changedDocument),
		overviewIn{Cursor: receipt.NextCursor}, 20000,
	)
	if err == nil || !strings.Contains(err.Error(), "overview_snapshot_changed") {
		t.Fatalf("snapshot change was accepted: %v", err)
	}
}

func TestOverviewChunkOrderChangeFails(t *testing.T) {
	content, document := largeOverviewFixture(t)
	ctx := legacyRenderContextForTest(t, "/repo", "test", content, document)
	framed := frameOverviewBody(ctx.Root, ctx.EffectiveScope, ctx.ScopeIdentity, ctx.EntryCount, ctx.Content)
	spans, err := planOverviewChunks(
		framed.Text, 20_000, len(framed.Receipt.StartMarker)+1, ctx.Sequence,
	)
	if err != nil || len(spans) < 3 {
		t.Fatalf("fixture did not produce three ordered Chunks: spans=%d err=%v", len(spans), err)
	}

	first := renderLegacyOverviewForTest(t, "/repo", "test", content, document, overviewIn{}, 20_000)
	parts := strings.SplitN(first.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
	var receipt struct {
		NextCursor string `json:"next_cursor"`
	}
	if len(parts) != 2 || json.Unmarshal([]byte(parts[0]), &receipt) != nil {
		t.Fatal("first Chunk receipt is unavailable")
	}
	_, firstChunkSHA, err := decodeOverviewCursor(receipt.NextCursor, ctx.ScopeIdentity, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	reorderedCursor := encodeOverviewCursor(
		ctx.ScopeIdentity, 20_000, spans[2].FirstOrdinal, firstChunkSHA,
	)
	_, err = renderOverviewDelivery(ctx, overviewIn{Cursor: reorderedCursor}, 20_000)
	if err == nil || !strings.Contains(err.Error(), "overview_cursor_chain_mismatch") {
		t.Fatalf("reordered Chunk chain was accepted: %v", err)
	}
}

func TestOverviewEntryExceedsChunkBudget(t *testing.T) {
	content := "#AOCI-CLI Complete Index\n===Sources/repo/===\nlarge.go[IM7S]: F:large | R:- | A:- | S:" + strings.Repeat("x", 13000) + "\n"
	document, _ := index.Parse(content)
	index.ResolveRelPaths(document, "/repo")
	_, err := renderOverviewDelivery(
		legacyRenderContextForTest(t, "/repo", "test", content, document),
		overviewIn{}, 4000,
	)
	if err == nil || !strings.Contains(err.Error(), overviewEntryExceedsChunkBudget) {
		t.Fatalf("oversized Entry was not rejected: %v", err)
	}
}

func TestOverviewExplicitCallsAlwaysDeliverCompleteBody(t *testing.T) {
	root := buildRepo(t)
	server, err := newMCPServer(root, "refresh-suppression-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "refresh-suppression-test", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	first, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	firstText := resText(t, first)
	if !strings.Contains(firstText, "delivery_mode: full") ||
		!strings.Contains(firstText, "a.go[X.Y.5.T]") ||
		!strings.Contains(firstText, "refresh_generation: 1") {
		t.Fatalf("initial call did not establish full cognition:\n%s", firstText)
	}

	second, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	secondText := resText(t, second)
	if !strings.Contains(secondText, "delivery_mode: full") ||
		!strings.Contains(secondText, "full_text_included: true") ||
		!strings.Contains(secondText, "a.go[X.Y.5.T]") ||
		strings.Contains(secondText, `"refresh_status":"refresh_not_required"`) {
		t.Fatalf("repeated explicit Overview must still deliver the full body: %s", secondText)
	}
}

func TestOverviewDirtyFormalCognitionIsDeliveredButUnreliable(t *testing.T) {
	root := buildRepo(t)
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := newMCPServer(root, "dirty-overview-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "dirty-overview-test", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	for _, want := range []string{
		"full_text_included: true",
		"a.go[X.Y.5.T]",
		"cognition_currency: dirty_or_stale",
		"governance_aligned: false",
		"cognition_state: invalid",
		"model_full_cognition_reliable: false",
		"delivery_guidance: verify_current_source_and_complete_governance",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dirty Overview missing %q:\n%s", want, text)
		}
	}
}

func TestOverviewPendingRecoveryFailsClosedButCheckOnlyRemainsAvailable(t *testing.T) {
	root := buildRepo(t)
	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "entries-pending.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if result.IsError || !strings.Contains(text, `"delivery_mode":"blocked"`) ||
		!strings.Contains(text, `"fallback_reason":"recovery_pending"`) ||
		!strings.Contains(text, `"cognition_level":0`) ||
		!strings.Contains(text, `"cognition_level_state":"no_cognition"`) ||
		strings.Contains(text, "a.go[X.Y.5.T]") {
		t.Fatalf("pending recovery did not stop full delivery: %s", text)
	}
	check, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{
			"check_only":                  true,
			"model_cognition_attestation": validLegacyAttestationMap(t, root),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkText := resText(t, check)
	var checkpoint overviewCheckpointEnvelope
	if check.IsError || json.Unmarshal([]byte(checkText), &checkpoint) != nil || checkpoint.Assessment.OverviewReads != 0 ||
		checkpoint.RequestMode != overviewRequestCheckOnly || checkpoint.DeliveryMode != overviewDeliveryCheckpoint || checkpoint.FullTextIncluded {
		t.Fatalf("check_only must remain a compact checkpoint during recovery: %s", checkText)
	}
	if strings.Contains(checkText, "challenge_digest") || strings.Contains(checkText, "model_attestation") {
		t.Fatalf("check_only must not request or assess attestation: %s", checkText)
	}
}

func TestOverviewSnapshotConfirmationRejectsConcurrentChange(t *testing.T) {
	root := buildRepo(t)
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	if err := os.WriteFile(indexPath, append(append([]byte{}, loaded.set.Root.Raw...), []byte("# concurrent\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if fail := confirmCognitionSnapshot(root, loaded.set); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
		t.Fatalf("concurrent formal-asset change was not rejected: %+v", fail)
	}
}

func TestAssessCognitionStates(t *testing.T) {
	current := newCognitionReceipt("/repo", "v1", "index", "repository_full")
	covered := true
	valid := assessCognition(current, &current, "valid", &covered)
	if valid.State != "valid" || valid.Recall != "none" || valid.OverviewReads != 0 ||
		!valid.Receipt.ModelFullReliable || valid.Receipt.State != "valid" {
		t.Fatalf("匹配凭据与可靠宿主认知应零召回: %+v", valid)
	}
	uncertain := assessCognition(current, &current, "uncertain", &covered)
	if uncertain.State != "uncertain" || uncertain.Recall != "host_choice_none_local_or_full" ||
		uncertain.Receipt.ModelFullReliable || uncertain.Receipt.State != "uncertain" {
		t.Fatalf("局部不确定必须保留宿主选择权: %+v", uncertain)
	}
	notCovered := false
	scope := assessCognition(current, &current, "valid", &notCovered)
	if scope.State != "uncertain" || scope.Recall != "host_choice_local_or_full" {
		t.Fatalf("认知范围不确定不得伪装valid: %+v", scope)
	}
	changed := current
	changed.IndexSHA256 = strings.Repeat("f", 64)
	invalid := assessCognition(current, &changed, "valid", &covered)
	if invalid.State != "invalid" || invalid.Reason != "index_sha256_changed" || invalid.Recall != "full" {
		t.Fatalf("索引身份变化必须确定性失效: %+v", invalid)
	}
	for name, mutate := range map[string]func(*cognitionReceipt){
		"receipt_version": func(value *cognitionReceipt) { value.Version-- },
		"service_version": func(value *cognitionReceipt) { value.MCPServiceVersion = "old" },
		"scope":           func(value *cognitionReceipt) { value.Scope = "module_only" },
	} {
		t.Run(name, func(t *testing.T) {
			stale := current
			mutate(&stale)
			assessment := assessCognition(current, &stale, "valid", &covered)
			if assessment.State != "invalid" || assessment.Recall != "full" ||
				assessment.Receipt.ModelFullReliable {
				t.Fatalf("过期认知合同不得零召回: %+v", assessment)
			}
		})
	}
}

func TestOverviewCheckOnlyReusesOrRefreshes(t *testing.T) {
	root := buildRepo(t)
	const version = "cognition-check-test"
	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	receipt := newCognitionReceipt(root, version, repository.text, "repository_full")
	server, err := newMCPServer(root, version)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "cognition-test", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	validResult, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview",
		Arguments: map[string]any{
			"cognition_receipt":     receipt,
			"model_cognition_state": "valid",
			"scope_covered":         true,
			"check_only":            true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	validText := resText(t, validResult)
	var valid overviewCheckpointEnvelope
	if err := json.Unmarshal([]byte(validText), &valid); err != nil ||
		valid.Assessment.State != "valid" || valid.Assessment.OverviewReads != 0 || valid.Reason != "explicit_check_only" ||
		strings.Contains(validText, "a.go[X.Y.5.T]") {
		t.Fatalf("有效凭据检查不得重复注入Overview: err=%v result=%s", err, validText)
	}

	stale := receipt
	stale.IndexSHA256 = strings.Repeat("0", 64)
	invalidResult, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview",
		Arguments: map[string]any{
			"cognition_receipt":     stale,
			"model_cognition_state": "valid",
			"scope_covered":         true,
			"check_only":            true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalidText := resText(t, invalidResult)
	var invalid overviewCheckpointEnvelope
	if err := json.Unmarshal([]byte(invalidText), &invalid); err != nil ||
		invalid.Assessment.RefreshStatus != machinecontract.RefreshStatusNotRequired ||
		invalid.Assessment.OverviewReads != 0 ||
		strings.Contains(invalidText, "a.go[X.Y.5.T]") {
		t.Fatalf("index identity alone must not become a refresh trigger: err=%v result=%s", err, invalidText)
	}
}

// TestOverviewUsesServerRuntimeVersion验证Overview与MCP握手共享RunStdio装配参数，
// 不允许工具层从仓库状态推测服务版本。
func TestOverviewUsesServerRuntimeVersion(
	t *testing.T,
) {
	root := buildRepo(t)
	const runtimeVersion = "v7.6.5-from-run-stdio"

	server, err := newMCPServer(
		root,
		runtimeVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport :=
		mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(
		context.Background(),
		serverTransport,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "overview-version-test",
			Version: "test",
		},
		nil,
	)
	clientSession, err := client.Connect(
		context.Background(),
		clientTransport,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	initialize := clientSession.InitializeResult()
	if initialize == nil ||
		initialize.ServerInfo == nil ||
		initialize.ServerInfo.Name != machinecontract.MCPServerName ||
		initialize.ServerInfo.Version != runtimeVersion {
		t.Fatalf(
			"MCP握手未使用启动版本: %+v",
			initialize,
		)
	}

	result, err := clientSession.CallTool(
		context.Background(),
		&mcp.CallToolParams{
			Name: "aoci_overview",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	output := resText(t, result)
	if !strings.Contains(
		output,
		"mcp_service_version: "+runtimeVersion+"\n",
	) {
		t.Fatalf(
			"Overview未透传启动版本:\n%s",
			output,
		)
	}
}

func TestRulesExposeConfiguredOverviewChunkTokens(t *testing.T) {
	root := buildRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OverviewDelivery.ChunkTokens = 12000
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result, err := connectMCPClient(t, root).CallTool(
		context.Background(), &mcp.CallToolParams{Name: "aoci_rules"},
	)
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	for _, want := range []string{
		"overview_delivery.chunk_tokens: 12000",
		"overview_delivery.chunk_tokens_min: 4000",
		"overview_delivery.chunk_tokens_max: 24000",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Rules missing %q:\n%s", want, text)
		}
	}
}

// TestOverviewDeliveryNilDocument验证损坏前置状态不会令计量辅助崩溃。
func TestOverviewDeliveryNilDocument(
	t *testing.T,
) {
	facts := renderLegacyOverviewForTest(
		t, "/repo", "test", "index", nil, overviewIn{},
		machinecontract.OverviewChunkTokensDefault,
	).Facts

	if facts.SectionCount != 0 ||
		facts.EntryCount != 0 ||
		facts.IndexBytes != 5 ||
		facts.IndexSHA256 == "" {
		t.Fatalf(
			"nil文档计量异常: %+v",
			facts,
		)
	}
}
