// Native fuzz targets for the Overview inputs a host or model sends back to
// the server: the continuation cursor (decodeOverviewCursor and the Chunk
// chain it drives) and the host delivery confirmation plus model Attestation
// JSON. Run one with
//
//	go test -run '^$' -fuzz '^FuzzOverviewCursorChain$' -fuzztime=30s ./internal/mcptools
//
// or every target through `make fuzz-smoke`.
package mcptools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// overviewCursorErrors are the only structured refusals a cursor may produce.
var overviewCursorErrors = map[string]bool{
	"overview_cursor_invalid":        true,
	"overview_snapshot_changed":      true,
	"overview_chunk_tokens_changed":  true,
	"overview_cursor_out_of_order":   true,
	"overview_cursor_chain_mismatch": true,
}

// fuzzOverviewIndex writes a synthetic legacy index with the given number of
// Entries, each padded so the body crosses the Chunk budget at small sizes.
func fuzzOverviewIndex(entries, padding int) string {
	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n===Sources/repo/src/===\n")
	pad := strings.Repeat("x", padding)
	for ordinal := 1; ordinal <= entries; ordinal++ {
		fmt.Fprintf(&source, "file-%04d.go[IM7S]: F:preserve object %04d | R:- | A:- | S:%s\n", ordinal, ordinal, pad)
	}
	return source.String()
}

// fuzzLegacyRenderContext is legacyRenderContextForTest without a *testing.T,
// so a fuzz target can build its fixture once before f.Fuzz.
func fuzzLegacyRenderContext(root, serviceVersion, indexText string) (overviewRenderContext, error) {
	document, warnings := index.Parse(indexText)
	if len(warnings) != 0 {
		return overviewRenderContext{}, fmt.Errorf("fixture warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, root)
	sectionCount, entryCount := countOverviewDimensions(document)
	receipt := newCognitionReceipt(root, serviceVersion, indexText, cognitionScopeRepositoryFull)
	sequence, err := legacyOverviewSequence(document, indexText)
	if err != nil {
		return overviewRenderContext{}, err
	}
	return overviewRenderContext{
		Root: root, MCPServiceVersion: serviceVersion, LayoutMode: "legacy-monolithic",
		RequestedScope: cognitionScopeRepositoryFull, EffectiveScope: cognitionScopeRepositoryFull,
		ScopeIdentity: receipt.IndexSHA256, Content: indexText, ContentBytes: len(indexText),
		EntryCount: entryCount, SectionCount: sectionCount, EstimatedTokens: len(indexText) / 3,
		Receipt: receipt,
		Assessment: cognitionRefreshAssessment{
			RefreshStatus: machinecontract.RefreshStatusReadyForOverview,
			Semantic: semanticChangeFacts{
				Threshold:         machinecontract.CognitionRefreshThresholdDefault,
				GovernanceAligned: true,
			},
		},
		Sequence: sequence,
	}, nil
}

// chunkReceiptHead decodes the JSON receipt that precedes a Chunk body.
func chunkReceiptHead(output string) (map[string]any, string, error) {
	parts := strings.SplitN(output, "\n"+overviewChunkBodyMarker+"\n", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("no Chunk body marker")
	}
	var head map[string]any
	if err := json.Unmarshal([]byte(parts[0]), &head); err != nil {
		return nil, "", err
	}
	return head, parts[1], nil
}

// FuzzDecodeOverviewCursor pins the cursor grammar: four colon-separated
// fields bound to the current snapshot identity and Chunk budget, an ordinal
// of at least one, and a 64-character prior-Chunk digest. A refusal is one of
// the structured errors with zero values; an acceptance re-encodes to a cursor
// that decodes to the same ordinal and digest.
func FuzzDecodeOverviewCursor(f *testing.F) {
	const identity = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digest := strings.Repeat("ab", 32)
	f.Add(encodeOverviewCursor(identity, 20000, 7, digest), 20000)
	f.Add(encodeOverviewCursor(identity, 4000, 1, digest), 4000)
	f.Add(encodeOverviewCursor(identity, 12000, 2, digest), 20000)
	f.Add(encodeOverviewCursor("other", 20000, 2, digest), 20000)
	f.Add(identity+":20000:0:"+digest, 20000)
	f.Add(identity+":20000:2:"+digest[:63], 20000)
	f.Add(identity+":20000:2:"+digest+":extra", 20000)
	f.Add(identity+":twenty:2:"+digest, 20000)
	f.Add(identity+":+20000:02:"+digest, 20000)
	f.Add(identity+":20000:-2:"+digest, 20000)
	f.Add("start", 20000)
	f.Add("", 20000)
	f.Add(":::", 20000)
	f.Add(identity+":9223372036854775808:1:"+digest, 20000)
	f.Fuzz(func(t *testing.T, cursor string, tokens int) {
		ordinal, prior, err := decodeOverviewCursor(cursor, identity, tokens)
		if err != nil {
			if !overviewCursorErrors[err.Error()] {
				t.Fatalf("unstructured cursor refusal %q for %q", err.Error(), cursor)
			}
			if ordinal != 0 || prior != "" {
				t.Fatalf("refused cursor %q still yielded %d/%q", cursor, ordinal, prior)
			}
			return
		}
		if ordinal < 1 || len(prior) != 64 {
			t.Fatalf("accepted cursor %q yielded ordinal %d and digest %q", cursor, ordinal, prior)
		}
		if !strings.HasPrefix(cursor, identity+":") {
			t.Fatalf("accepted cursor %q is not bound to the identity", cursor)
		}
		again := encodeOverviewCursor(identity, tokens, ordinal, prior)
		ordinal2, prior2, err := decodeOverviewCursor(again, identity, tokens)
		if err != nil || ordinal2 != ordinal || prior2 != prior {
			t.Fatalf("re-encoded cursor %q decodes to %d/%q (%v), want %d/%q", again, ordinal2, prior2, err, ordinal, prior)
		}
		if _, _, err := decodeOverviewCursor(cursor, identity+"x", tokens); err == nil || err.Error() != "overview_snapshot_changed" {
			t.Fatalf("cursor %q survived an identity change: %v", cursor, err)
		}
		if _, _, err := decodeOverviewCursor(cursor, identity, tokens+1); err == nil || err.Error() != "overview_chunk_tokens_changed" {
			t.Fatalf("cursor %q survived a Chunk budget change: %v", cursor, err)
		}
	})
}

// FuzzOverviewCursorChain drives renderOverviewDelivery with an arbitrary
// cursor against one fixed chunked body. The server-issued cursors (walked
// once from the start) are the only ones that may deliver, and each delivers
// exactly the Chunk it was issued for; everything else is refused with a
// structured error and no output at all. A cursor that merely spells an issued
// cursor differently (Atoi leniency) must deliver that cursor's Chunk.
func FuzzOverviewCursorChain(f *testing.F) {
	const chunkTokens = machinecontract.OverviewChunkTokensMin
	content := fuzzOverviewIndex(160, 200)
	ctx, err := fuzzLegacyRenderContext("/repo", "fuzz", content)
	if err != nil {
		f.Fatal(err)
	}
	issued := map[string]string{}
	var chain []string
	cursor := ""
	for {
		rendered, err := renderOverviewDelivery(ctx, overviewIn{Cursor: cursor}, chunkTokens)
		if err != nil {
			f.Fatalf("walking the Chunk chain from %q: %v", cursor, err)
		}
		if rendered.Facts.DeliveryMode != overviewDeliveryChunked {
			f.Fatalf("fixture did not chunk: %+v", rendered.Facts)
		}
		head, _, err := chunkReceiptHead(rendered.Output)
		if err != nil {
			f.Fatal(err)
		}
		issued[cursor] = rendered.Output
		chain = append(chain, cursor)
		if completed, _ := head["completed"].(bool); completed {
			break
		}
		next, _ := head["next_cursor"].(string)
		if next == "" {
			f.Fatal("Chunk without a next cursor before completion")
		}
		cursor = next
	}
	if len(chain) < 3 {
		f.Fatalf("fixture produced only %d Chunks", len(chain))
	}
	issued["start"] = issued[""]

	for _, cursor := range chain {
		f.Add(cursor)
		if cursor == "" {
			continue
		}
		f.Add(cursor[:len(cursor)-1] + "0")
		f.Add(strings.Replace(cursor, ":"+fmt.Sprint(chunkTokens)+":", ":"+fmt.Sprint(chunkTokens+1)+":", 1))
		f.Add("x" + cursor[1:])
	}
	ordinal, prior, err := decodeOverviewCursor(chain[1], ctx.ScopeIdentity, chunkTokens)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encodeOverviewCursor(ctx.ScopeIdentity, chunkTokens, ordinal+1, prior))
	f.Add(ctx.ScopeIdentity + ":+" + fmt.Sprint(chunkTokens) + ":0" + fmt.Sprint(ordinal) + ":" + prior)
	f.Add("start")
	f.Add("")

	f.Fuzz(func(t *testing.T, cursor string) {
		rendered, err := renderOverviewDelivery(ctx, overviewIn{Cursor: cursor}, chunkTokens)
		want, valid := issued[cursor]
		if !valid && err == nil {
			ordinal, prior, decodeErr := decodeOverviewCursor(cursor, ctx.ScopeIdentity, chunkTokens)
			if decodeErr != nil {
				t.Fatalf("cursor %q delivered although it does not decode: %v", cursor, decodeErr)
			}
			canonical := encodeOverviewCursor(ctx.ScopeIdentity, chunkTokens, ordinal, prior)
			if want, valid = issued[canonical]; !valid {
				t.Fatalf("cursor %q (canonical %q) delivered although it was never issued", cursor, canonical)
			}
		}
		if valid {
			if err != nil {
				t.Fatalf("issued cursor %q refused: %v", cursor, err)
			}
			if rendered.Output != want {
				t.Fatalf("cursor %q delivered a different Chunk than it was issued for", cursor)
			}
			if rendered.Facts.DeliveryMode != overviewDeliveryChunked || rendered.Facts.FullTextIncluded {
				t.Fatalf("issued cursor %q changed delivery facts: %+v", cursor, rendered.Facts)
			}
			return
		}
		if !overviewCursorErrors[err.Error()] {
			t.Fatalf("unstructured refusal %q for cursor %q", err.Error(), cursor)
		}
		if !reflect.DeepEqual(rendered, overviewRendered{}) {
			t.Fatalf("refused cursor %q still produced a partial delivery: %+v", cursor, rendered.Facts)
		}
	})
}

// FuzzOverviewAttestationInput feeds arbitrary bytes through the JSON shape
// the host and model send back (host_delivery_confirmation and
// model_cognition_attestation inside the Overview input) against one fixed
// small body. Any decodable input renders without panic or error, never
// re-transmits the body, reports only the enumerated verdicts, and keeps the
// proof boundaries: complete assimilation needs a confirmed host receipt and a
// passing Attestation, pass needs a valid envelope with complete ordered
// answers, and fail is reserved for an invalid envelope or zero recall.
func FuzzOverviewAttestationInput(f *testing.F) {
	const chunkTokens = machinecontract.OverviewChunkTokensDefault
	content := fuzzOverviewIndex(12, 24)
	ctx, err := fuzzLegacyRenderContext("/repo", "fuzz", content)
	if err != nil {
		f.Fatal(err)
	}
	framed := frameOverviewBody(ctx.Root, ctx.EffectiveScope, ctx.ScopeIdentity, ctx.EntryCount, ctx.Content)
	challenge := buildOverviewChallenge(ctx.ScopeIdentity, ctx.Sequence)
	confirmation := &overviewHostConfirmation{
		Version: overviewDeliveryReceiptV1, BodySHA256: framed.Receipt.BodySHA256,
		BodyBytes: framed.Receipt.BodyUTF8Bytes, EndMarkerObserved: true,
	}
	complete := completeAttestation(challenge, ctx.EntryCount, ctx.EstimatedTokens)

	encode := func(input overviewIn) []byte {
		data, err := json.Marshal(input)
		if err != nil {
			f.Fatal(err)
		}
		return data
	}
	full := encode(overviewIn{HostConfirmation: confirmation, ModelAttestation: complete})
	rendered, err := renderOverviewDelivery(ctx, overviewIn{HostConfirmation: confirmation, ModelAttestation: complete}, chunkTokens)
	if err != nil || rendered.Attestation.CognitionAssimilation != cognitionAssimilationComplete {
		f.Fatalf("the complete proof seed does not assimilate: err=%v attestation=%+v", err, rendered.Attestation)
	}
	f.Add(full)
	f.Add(encode(overviewIn{HostConfirmation: confirmation}))
	f.Add(encode(overviewIn{ModelAttestation: complete}))
	f.Add(encode(overviewIn{}))
	f.Add(encode(overviewIn{Cursor: "start"}))
	f.Add(encode(overviewIn{HostConfirmation: &overviewHostConfirmation{Version: "x"}, ModelAttestation: complete}))
	for _, mutate := range []func(*overviewModelAttestation){
		func(report *overviewModelAttestation) {
			report.TruncationDetected = true
			report.CoveragePercent = 55
			report.UnseenSections = []string{"middle\nline"}
			report.ChallengeAnswers = report.ChallengeAnswers[:2]
		},
		func(report *overviewModelAttestation) { report.ChallengeAnswers[0].ObjectIdentity = "src/file-0001.go" },
		func(report *overviewModelAttestation) { report.ChallengeAnswers[1].Tag = "WRONG" },
		func(report *overviewModelAttestation) {
			report.ChallengeAnswers[2], report.ChallengeAnswers[3] = report.ChallengeAnswers[3], report.ChallengeAnswers[2]
		},
		func(report *overviewModelAttestation) { report.IndexSHA256 = "stale" },
		func(report *overviewModelAttestation) { report.SystemMasteryPercent = 64 },
		func(report *overviewModelAttestation) { report.CoveragePercent = -1 },
		func(report *overviewModelAttestation) {
			report.ReportedEntryCount = -5
			report.ReportedEstimatedTokens = 1 << 40
		},
		func(report *overviewModelAttestation) {
			for i := range report.ChallengeAnswers {
				report.ChallengeAnswers[i].CoreF = "metadata-only guess"
			}
		},
		func(report *overviewModelAttestation) {
			report.ChallengeAnswers = append(report.ChallengeAnswers, report.ChallengeAnswers...)
		},
		func(report *overviewModelAttestation) {
			report.ChallengeAnswers[0].Ordinal = -1
			report.ChallengeAnswers[1].Ordinal = 1 << 30
		},
	} {
		report := completeAttestation(challenge, ctx.EntryCount, ctx.EstimatedTokens)
		mutate(report)
		f.Add(encode(overviewIn{HostConfirmation: confirmation, ModelAttestation: report}))
	}
	f.Add([]byte(`{"model_cognition_attestation":{}}`))
	f.Add([]byte(`{"host_delivery_confirmation":{}}`))
	f.Add([]byte(`{"model_cognition_attestation":{"coverage_percent":1e999}}`))
	f.Add([]byte(`{"model_cognition_attestation":{"challenge_answers":[{"ordinal":1},{"ordinal":1}]}}`))
	f.Add([]byte(`{"model_cognition_attestation":null,"host_delivery_confirmation":null}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))

	entryLine := strings.SplitN(strings.TrimPrefix(content, "#AOCI-CLI Complete Index\n#Locale: en-US\n===Sources/repo/src/===\n"), "\n", 2)[0]
	f.Fuzz(func(t *testing.T, data []byte) {
		var input overviewIn
		if json.Unmarshal(data, &input) != nil {
			return
		}
		rendered, err := renderOverviewDelivery(ctx, input, chunkTokens)
		again, errAgain := renderOverviewDelivery(ctx, input, chunkTokens)
		if (err == nil) != (errAgain == nil) || (err == nil && !reflect.DeepEqual(rendered, again)) {
			t.Fatalf("rendering is not deterministic for %s", data)
		}
		if input.HostConfirmation == nil && input.ModelAttestation == nil {
			if err != nil {
				if !overviewCursorErrors[err.Error()] {
					t.Fatalf("unstructured refusal %q for %s", err.Error(), data)
				}
				if !reflect.DeepEqual(rendered, overviewRendered{}) {
					t.Fatalf("refusal with output for %s", data)
				}
				return
			}
			if rendered.Output == "" {
				t.Fatalf("delivery without output for %s", data)
			}
			if input.Cursor == "" && (rendered.Facts.DeliveryMode != overviewDeliveryFull || !rendered.Facts.FullTextIncluded ||
				!strings.Contains(rendered.Output, framed.Text)) {
				t.Fatalf("small body was not delivered in full: %+v", rendered.Facts)
			}
			return
		}

		if err != nil {
			t.Fatalf("attestation input refused: %v for %s", err, data)
		}
		output, facts, attestation := rendered.Output, rendered.Facts, rendered.Attestation
		if facts.DeliveryMode != overviewDeliveryAttestation || facts.FullTextIncluded {
			t.Fatalf("attestation input changed the delivery mode: %+v", facts)
		}
		if strings.Contains(output, overviewChunkBodyMarker) || strings.Contains(output, framed.Receipt.StartMarker) ||
			strings.Contains(output, entryLine) {
			t.Fatalf("attestation response re-transmitted cognition for %s", data)
		}
		if !utf8.ValidString(output) {
			t.Fatalf("attestation response is not valid UTF-8 for %s", data)
		}
		switch attestation.ModelAttestation {
		case modelAttestationPass, modelAttestationPartial, modelAttestationFail, modelAttestationNotProvided:
		default:
			t.Fatalf("unknown model attestation verdict %q", attestation.ModelAttestation)
		}
		switch attestation.CognitionAssimilation {
		case cognitionAssimilationComplete, cognitionAssimilationPartial, cognitionAssimilationUncertain:
		default:
			t.Fatalf("unknown assimilation verdict %q", attestation.CognitionAssimilation)
		}
		switch attestation.DeliveryIntegrity {
		case deliveryIntegrityConfirmed, deliveryIntegrityUnconfirmed, deliveryIntegrityIncomplete:
		default:
			t.Fatalf("unknown delivery integrity %q", attestation.DeliveryIntegrity)
		}
		if attestation.ChallengeTotal != len(challenge.Ordinals) ||
			attestation.ChallengePassed < 0 || attestation.ChallengePassed > attestation.ChallengeTotal {
			t.Fatalf("challenge tally out of range: %+v", attestation)
		}
		if input.ModelAttestation != nil {
			// One recall per distinct challenged ordinal: a repeated answer is a
			// format fault and must not be scored twice.
			answered := map[int]bool{}
			for _, answer := range input.ModelAttestation.ChallengeAnswers {
				if _, challenged := challenge.Targets[answer.Ordinal]; challenged {
					answered[answer.Ordinal] = true
				}
			}
			if attestation.ChallengePassed > len(answered) {
				t.Fatalf("%d passes over %d distinct challenged ordinals: %+v", attestation.ChallengePassed, len(answered), attestation)
			}
		}
		if (input.ModelAttestation == nil) != (attestation.ModelAttestation == modelAttestationNotProvided) ||
			(input.ModelAttestation == nil) == attestation.ReportProvided {
			t.Fatalf("report presence misreported: %+v", attestation)
		}
		if attestation.ModelAttestation == modelAttestationPass &&
			!(attestation.EnvelopeValid && attestation.ChallengeAnswersComplete && attestation.ChallengeAnswersOrdered &&
				attestation.ObjectIdentityMissCount <= attestationIdentityMissMax && !attestation.TruncationDetected) {
			t.Fatalf("pass without its preconditions: %+v", attestation)
		}
		if attestation.ModelAttestation == modelAttestationFail && attestation.EnvelopeValid && attestation.ChallengePassed != 0 {
			t.Fatalf("fail with a valid envelope and recall: %+v", attestation)
		}
		if attestation.CognitionAssimilation == cognitionAssimilationComplete &&
			(attestation.DeliveryIntegrity != deliveryIntegrityConfirmed || attestation.ModelAttestation != modelAttestationPass) {
			t.Fatalf("complete assimilation without proof: %+v", attestation)
		}
		if attestation.DeliveryIntegrity == deliveryIntegrityConfirmed {
			confirmation := input.HostConfirmation
			if confirmation == nil || confirmation.Version != overviewDeliveryReceiptV1 || !confirmation.EndMarkerObserved ||
				confirmation.BodySHA256 != framed.Receipt.BodySHA256 || confirmation.BodyBytes != framed.Receipt.BodyUTF8Bytes {
				t.Fatalf("delivery confirmed without an exact host receipt: %+v", confirmation)
			}
		}
		for _, line := range []string{
			"delivery_mode: attestation\n",
			"model_attestation: " + attestation.ModelAttestation + "\n",
			"cognition_assimilation: " + attestation.CognitionAssimilation + "\n",
			"delivery_integrity: " + attestation.DeliveryIntegrity + "\n",
			fmt.Sprintf("challenge_passed: %d/%d\n", attestation.ChallengePassed, attestation.ChallengeTotal),
		} {
			if !strings.Contains(output, line) {
				t.Fatalf("attestation metadata lacks %q", strings.TrimSpace(line))
			}
		}
	})
}
