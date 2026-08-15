package mcptools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func syntheticChallengeTargets(entryCount int) []overviewChallengeTarget {
	targets := make([]overviewChallengeTarget, entryCount)
	for i := range targets {
		targets[i] = overviewChallengeTarget{
			ObjectIdentity: fmt.Sprintf("internal/pkg/file-%03d.go", i+1),
			Tag:            fmt.Sprintf("ID%dS", i%9+1),
			CoreF:          fmt.Sprintf("owns responsibility %03d", i+1),
		}
	}
	return targets
}

func syntheticChallenge(entryCount int) overviewChallenge {
	targets := syntheticChallengeTargets(entryCount)
	return buildOverviewChallenge("scope-identity", targets)
}

func completeAttestation(challenge overviewChallenge, entries, tokens int) *overviewModelAttestation {
	answers := make([]overviewChallengeAnswer, 0, len(challenge.Ordinals))
	for _, ordinal := range challenge.Ordinals {
		target := challenge.Targets[ordinal]
		answers = append(answers, overviewChallengeAnswer{
			Ordinal: ordinal, ObjectIdentity: target.ObjectIdentity,
			Tag: target.Tag, CoreF: target.CoreF,
		})
	}
	return &overviewModelAttestation{
		Version:     modelCognitionAttestationV1,
		IndexSHA256: challenge.IndexSHA256, EntrySequenceSHA256: challenge.EntrySequenceSHA256,
		EntryCount: challenge.EntryCount, ChallengeDigest: challenge.Digest,
		ReportedEntryCount: entries, ReportedEstimatedTokens: tokens,
		CoveragePercent: 100, SystemMasteryPercent: 92, ConfidencePercent: 95,
		UnseenSections: []string{}, UncertaintyReasons: []string{}, ChallengeAnswers: answers,
	}
}

func validLegacyAttestationMap(t *testing.T, root string) map[string]any {
	t.Helper()
	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	sequence, err := legacyOverviewSequence(repository.doc, repository.text)
	if err != nil {
		t.Fatal(err)
	}
	challenge := buildOverviewChallenge(newCognitionReceipt(
		root, "unused", repository.text, cognitionScopeRepositoryFull,
	).IndexSHA256, sequence)
	return attestationMap(t, completeAttestation(challenge, len(sequence), len(repository.text)/3))
}

func legacyAttestationMapWithWrongAnswers(t *testing.T, root string, wrong int) map[string]any {
	t.Helper()
	result := validLegacyAttestationMap(t, root)
	answers, ok := result["challenge_answers"].([]any)
	if !ok || wrong < 0 || wrong > len(answers) {
		t.Fatalf("invalid challenge fixture: answers=%T wrong=%d", result["challenge_answers"], wrong)
	}
	for index := 0; index < wrong; index++ {
		answer, ok := answers[index].(map[string]any)
		if !ok {
			t.Fatalf("challenge answer %d = %T", index, answers[index])
		}
		answer["object_identity"] = "deliberately-wrong-object"
	}
	return result
}

func attestationMap(t *testing.T, attestation *overviewModelAttestation) map[string]any {
	t.Helper()
	data, err := json.Marshal(attestation)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestModelCognitionAttestationComplete(t *testing.T) {
	challenge := syntheticChallenge(120)
	if len(challenge.Ordinals) != 10 || challenge.Ordinals[0] > 12 || challenge.Ordinals[4] < 49 ||
		challenge.Ordinals[5] > 72 || challenge.Ordinals[9] < 109 {
		t.Fatalf("challenge is not distributed across the complete sequence: %v", challenge.Ordinals)
	}
	report := completeAttestation(challenge, 120, 30_000)
	result := assessOverviewAttestation(challenge, 120, 30_000, hostDeliveryConfirmed, true, report)
	if result.DeliveryIntegrity != deliveryIntegrityConfirmed || result.ModelAttestation != modelAttestationPass ||
		result.CognitionAssimilation != cognitionAssimilationComplete || result.ChallengePassed != 10 {
		t.Fatalf("complete attestation was not accepted: %+v", result)
	}
}

func currentSyntheticChallenge(targets []overviewChallengeTarget) overviewChallenge {
	digest := sha256.Sum256([]byte("index\x00" + overviewEntrySequenceSHA256(targets)))
	return buildOverviewChallenge(fmt.Sprintf("%x", digest[:]), targets)
}

func assertCompleteChallengeAttestation(t *testing.T, challenge overviewChallenge, tokens int) {
	t.Helper()
	report := completeAttestation(challenge, challenge.EntryCount, tokens)
	result := assessOverviewAttestation(
		challenge, challenge.EntryCount, tokens, hostDeliveryConfirmed, true, report,
	)
	if result.ModelAttestation != modelAttestationPass ||
		result.CognitionAssimilation != cognitionAssimilationComplete ||
		result.ChallengePassed != 10 || result.ChallengeTotal != 10 {
		t.Fatalf("current Challenge did not pass 10/10: %+v", result)
	}
}

func TestCognitionAttestationStabilityAcrossCurrentEntrySetChanges(t *testing.T) {
	baseTargets := syntheticChallengeTargets(52)
	first := currentSyntheticChallenge(baseTargets)
	assertCompleteChallengeAttestation(t, first, 12_000)

	removedTargets := append([]overviewChallengeTarget{}, baseTargets[:16]...)
	removedTargets = append(removedTargets, baseTargets[17:]...)
	afterRemoval := currentSyntheticChallenge(removedTargets)
	assertCompleteChallengeAttestation(t, afterRemoval, 11_800)
	if afterRemoval.IndexSHA256 == first.IndexSHA256 ||
		afterRemoval.EntrySequenceSHA256 == first.EntrySequenceSHA256 ||
		afterRemoval.EntryCount != 51 {
		t.Fatalf("Entry removal did not produce a new current Challenge identity: before=%+v after=%+v", first, afterRemoval)
	}

	added := overviewChallengeTarget{
		ObjectIdentity: "internal/pkg/file-added.go", Tag: "ID9S", CoreF: "owns added responsibility",
	}
	addedTargets := append([]overviewChallengeTarget{}, removedTargets[:20]...)
	addedTargets = append(addedTargets, added)
	addedTargets = append(addedTargets, removedTargets[20:]...)
	afterAddition := currentSyntheticChallenge(addedTargets)
	assertCompleteChallengeAttestation(t, afterAddition, 12_100)
	if afterAddition.IndexSHA256 == afterRemoval.IndexSHA256 ||
		afterAddition.EntrySequenceSHA256 == afterRemoval.EntrySequenceSHA256 ||
		afterAddition.EntryCount != 52 {
		t.Fatalf("Entry addition did not produce a new current Challenge identity: removed=%+v added=%+v", afterRemoval, afterAddition)
	}
}

func TestOldAttestationIsInvalidAfterIndexSHAChanges(t *testing.T) {
	targets := syntheticChallengeTargets(52)
	oldChallenge := buildOverviewChallenge(strings.Repeat("a", 64), targets)
	oldReport := completeAttestation(oldChallenge, len(targets), 12_000)
	currentChallenge := buildOverviewChallenge(strings.Repeat("b", 64), targets)

	result := assessOverviewAttestation(
		currentChallenge, len(targets), 12_000, hostDeliveryConfirmed, true, oldReport,
	)
	if result.ModelAttestation != modelAttestationFail ||
		result.CognitionAssimilation != cognitionAssimilationUncertain {
		t.Fatalf("old Index Attestation was accepted against the current SHA: %+v", result)
	}
}

func TestAttestationRequiresEveryCurrentChallengeIdentity(t *testing.T) {
	challenge := currentSyntheticChallenge(syntheticChallengeTargets(52))
	for name, mutate := range map[string]func(*overviewModelAttestation){
		"index SHA": func(report *overviewModelAttestation) {
			report.IndexSHA256 = strings.Repeat("0", 64)
		},
		"Entry sequence": func(report *overviewModelAttestation) {
			report.EntrySequenceSHA256 = strings.Repeat("0", 64)
		},
		"Entry count": func(report *overviewModelAttestation) {
			report.EntryCount--
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := completeAttestation(challenge, challenge.EntryCount, 12_000)
			mutate(report)
			result := assessOverviewAttestation(
				challenge, challenge.EntryCount, 12_000, hostDeliveryConfirmed, true, report,
			)
			if result.ModelAttestation != modelAttestationFail ||
				result.CognitionAssimilation != cognitionAssimilationUncertain {
				t.Fatalf("mismatched %s was accepted: %+v", name, result)
			}
		})
	}
}

func TestAttestationCanonicalizesOnlyExactCodeRepositoryPath(t *testing.T) {
	targets := []overviewChallengeTarget{{
		ObjectIdentity: "code:src/task.go", Tag: "CD9S",
		CoreF: "owns task priority behavior",
	}}
	challenge := currentSyntheticChallenge(targets)
	report := completeAttestation(challenge, 1, 1024)
	report.ChallengeAnswers[0].ObjectIdentity = "src/task.go"
	result := assessOverviewAttestation(
		challenge, 1, 1024, hostDeliveryConfirmed, true, report,
	)
	if result.ModelAttestation != modelAttestationPass ||
		result.CognitionAssimilation != cognitionAssimilationComplete ||
		result.ChallengePassed != 1 {
		t.Fatalf("exact repository-relative Code identity was rejected: %+v", result)
	}

	for _, identity := range []string{
		"./src/task.go", "SRC/task.go", "task.go", "code:/src/task.go",
		"database://primary/public/task.go",
	} {
		t.Run(identity, func(t *testing.T) {
			invalid := completeAttestation(challenge, 1, 1024)
			invalid.ChallengeAnswers[0].ObjectIdentity = identity
			got := assessOverviewAttestation(
				challenge, 1, 1024, hostDeliveryConfirmed, true, invalid,
			)
			if got.ModelAttestation != modelAttestationFail || got.ChallengePassed != 0 {
				t.Fatalf("non-exact Code identity %q was accepted: %+v", identity, got)
			}
		})
	}
}

func TestAttestationKeepsDatabaseCanonicalIdentityExact(t *testing.T) {
	challenge := currentSyntheticChallenge([]overviewChallengeTarget{{
		ObjectIdentity: "database://primary/public/tasks", Tag: "DB9S",
		CoreF: "stores task priority",
	}})
	report := completeAttestation(challenge, 1, 1024)
	report.ChallengeAnswers[0].ObjectIdentity = "tasks"
	result := assessOverviewAttestation(
		challenge, 1, 1024, hostDeliveryConfirmed, true, report,
	)
	if result.ModelAttestation != modelAttestationFail || result.ChallengePassed != 0 {
		t.Fatalf("non-canonical Database identity was accepted: %+v", result)
	}
}

func TestOverviewCognitionLevelsPreserveProofBoundaries(t *testing.T) {
	tests := []struct {
		name              string
		indexLoaded       bool
		deliveryIntegrity string
		modelAttestation  string
		governanceAligned bool
		wantLevel         int
		wantState         string
	}{
		{"no cognition", false, deliveryIntegrityIncomplete, modelAttestationNotProvided, false, machinecontract.CognitionLevelNoCognition, machinecontract.CognitionLevelStateNoCognition},
		{"index loaded", true, deliveryIntegrityUnconfirmed, modelAttestationNotProvided, true, machinecontract.CognitionLevelIndexLoaded, machinecontract.CognitionLevelStateIndexLoaded},
		{"delivery verified", true, deliveryIntegrityConfirmed, modelAttestationFail, true, machinecontract.CognitionLevelDeliveryVerified, machinecontract.CognitionLevelStateDeliveryVerified},
		{"cognition verified", true, deliveryIntegrityConfirmed, modelAttestationPass, false, machinecontract.CognitionLevelVerified, machinecontract.CognitionLevelStateVerified},
		{"cognition governed", true, deliveryIntegrityConfirmed, modelAttestationPass, true, machinecontract.CognitionLevelGoverned, machinecontract.CognitionLevelStateGoverned},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := assessOverviewCognitionLevel(
				test.indexLoaded, test.deliveryIntegrity,
				test.modelAttestation, test.governanceAligned,
			)
			if got.Level != test.wantLevel || got.State != test.wantState ||
				got.Message == "" || strings.Contains(got.Message, "text_asset_error") {
				t.Fatalf("unexpected cognition level: %+v", got)
			}
		})
	}
}

func TestOverviewCognitionLevelDisplaySupportsBothLocales(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	seen := map[string]string{}
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		level := assessOverviewCognitionLevel(
			true, deliveryIntegrityConfirmed, modelAttestationFail, true,
		)
		if level.Level != machinecontract.CognitionLevelDeliveryVerified || level.Message == "" {
			t.Fatalf("%s delivery status is incomplete: %+v", locale, level)
		}
		seen[locale] = level.Message
	}
	if seen[textassets.DefaultLocale] == seen[textassets.LegacyLocale] ||
		!strings.Contains(seen[textassets.LegacyLocale], "已加载项目认知") {
		t.Fatalf("localized cognition status was not preserved: %+v", seen)
	}
}

func TestModelCognitionAttestationRejectsDisplayOrdinalOffsets(t *testing.T) {
	challenge := syntheticChallenge(20)
	for _, offset := range []int{-3, 3} {
		report := completeAttestation(challenge, 20, 30_000)
		for index := range report.ChallengeAnswers {
			report.ChallengeAnswers[index].Ordinal += offset
		}
		result := assessOverviewAttestation(challenge, 20, 30_000, hostDeliveryConfirmed, true, report)
		if result.ModelAttestation != modelAttestationFail || result.CognitionAssimilation != cognitionAssimilationUncertain {
			t.Fatalf("ordinal offset %+d was accepted: %+v", offset, result)
		}
	}
}

func TestChallengeMetadataDoesNotExposeAnswers(t *testing.T) {
	challenge := syntheticChallenge(120)
	metadata := renderOverviewAttestationMetadata(
		true, hostDeliveryUnconfirmed, overviewBodyReceipt{}, challenge,
		120, 12, 30_000, true, assessOverviewAttestation(
			challenge, 120, 30_000, hostDeliveryUnconfirmed, true, nil,
		),
	)
	for _, target := range challenge.Targets {
		for _, secret := range []string{target.ObjectIdentity, target.Tag, target.CoreF} {
			if strings.Contains(metadata, secret) {
				t.Fatalf("Challenge metadata exposed answer content %q: %s", secret, metadata)
			}
		}
	}
}

func TestModelCognitionAttestationDetectsTruncationAndFalseClaims(t *testing.T) {
	challenge := syntheticChallenge(120)
	tests := []struct {
		name       string
		mutate     func(*overviewModelAttestation)
		hostStatus string
		want       string
		assim      string
	}{
		{
			name: "middle body truncated",
			mutate: func(report *overviewModelAttestation) {
				report.TruncationDetected = true
				report.CoveragePercent = 55
				report.UnseenSections = []string{"middle"}
				report.ChallengeAnswers = append(report.ChallengeAnswers[:2], report.ChallengeAnswers[8:]...)
			},
			hostStatus: hostDeliveryIncomplete, want: modelAttestationPartial, assim: cognitionAssimilationUncertain,
		},
		{
			name: "head and tail retained but middle lost",
			mutate: func(report *overviewModelAttestation) {
				report.CoveragePercent = 40
				report.TruncationDetected = true
				report.ChallengeAnswers = []overviewChallengeAnswer{report.ChallengeAnswers[0], report.ChallengeAnswers[9]}
			},
			hostStatus: hostDeliveryIncomplete, want: modelAttestationPartial, assim: cognitionAssimilationUncertain,
		},
		{
			// 认证测的是消化而非逐字复述: 十题里一处身份失手仍在通过比例内, 且
			// 身份失手数未超上限。
			name: "one identity slip inside the pass ratio",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[5].ObjectIdentity = "internal/pkg/not-seen.go"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPass, assim: cognitionAssimilationComplete,
		},
		{
			// 身份是唯一没有"复述误差"的离散事实: 两处身份失手即便还在 8/10 之
			// 内, 也说明序列位置认知有洞, 不得通过。
			name: "two identity misses exceed the identity cap",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[2].ObjectIdentity = "internal/pkg/not-seen-a.go"
				report.ChallengeAnswers[7].ObjectIdentity = "internal/pkg/not-seen-b.go"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPartial, assim: cognitionAssimilationPartial,
		},
		{
			// 倒挂修正: 诚实声称看全但三题失手, 是 partial 而不是 fail —— 同一组
			// 答案配上保守的覆盖率声明也只会是 partial, 完整声明不能判得更重。
			name: "complete claim with three misses is partial not fail",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[1].Tag = "WRONG"
				report.ChallengeAnswers[4].Tag = "WRONG"
				report.ChallengeAnswers[8].CoreF = "an unrelated responsibility altogether"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPartial, assim: cognitionAssimilationPartial,
		},
		{
			// F 按语义等价判: 释义与截去尾句都算召回, 掉换成别的职责不算。
			name: "paraphrased and truncated core F still match",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[0].CoreF = "Owns the " + strings.TrimPrefix(report.ChallengeAnswers[0].CoreF, "owns ")
				report.ChallengeAnswers[9].CoreF = "owns responsibility"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPass, assim: cognitionAssimilationComplete,
		},
		{
			// 语义等价不是放水: 只差一个编号的兄弟 F 相似度落在门槛之下, 三处
			// 这样的错答把通过率拉到 7/10。
			name: "three sibling core F texts are not recall",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[1].CoreF = "owns responsibility 999"
				report.ChallengeAnswers[4].CoreF = "owns responsibility 998"
				report.ChallengeAnswers[8].CoreF = "owns responsibility 997"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPartial, assim: cognitionAssimilationPartial,
		},
		{
			name: "entry count correct but challenge wrong",
			mutate: func(report *overviewModelAttestation) {
				for i := range report.ChallengeAnswers {
					report.ChallengeAnswers[i].CoreF = "metadata-only guess"
				}
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationFail, assim: cognitionAssimilationUncertain,
		},
		{
			name: "challenge answer missing",
			mutate: func(report *overviewModelAttestation) {
				report.CoveragePercent = 80
				report.ChallengeAnswers = report.ChallengeAnswers[:9]
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPartial, assim: cognitionAssimilationPartial,
		},
		{
			// 顺序是可修正的格式问题, 不是认知缺失: partial, 修正格式后重提。
			name: "challenge answers reordered",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[3], report.ChallengeAnswers[4] = report.ChallengeAnswers[4], report.ChallengeAnswers[3]
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPartial, assim: cognitionAssimilationPartial,
		},
		{
			// Tag 保持精确匹配, 但一处 Tag 失手只计入通过比例。
			name: "object exists but one tag is wrong",
			mutate: func(report *overviewModelAttestation) {
				report.ChallengeAnswers[4].Tag = "WRONG"
			},
			hostStatus: hostDeliveryConfirmed, want: modelAttestationPass, assim: cognitionAssimilationComplete,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeAttestation(challenge, 120, 30_000)
			test.mutate(report)
			result := assessOverviewAttestation(challenge, 120, 30_000, test.hostStatus, true, report)
			if result.ModelAttestation != test.want || result.CognitionAssimilation != test.assim {
				t.Fatalf("unexpected assessment: %+v", result)
			}
		})
	}
}

func TestModelCognitionAttestationLowMasteryIsPartialAssimilation(t *testing.T) {
	challenge := syntheticChallenge(120)
	report := completeAttestation(challenge, 120, 30_000)
	report.SystemMasteryPercent = 64
	result := assessOverviewAttestation(challenge, 120, 30_000, hostDeliveryConfirmed, true, report)
	if result.ModelAttestation != modelAttestationPass || result.CognitionAssimilation != cognitionAssimilationPartial {
		t.Fatalf("low mastery must not erase delivery or become complete: %+v", result)
	}
}

func TestHostTruncationOverridesACompleteModelClaim(t *testing.T) {
	challenge := syntheticChallenge(120)
	result := assessOverviewAttestation(
		challenge, 120, 30_000, hostDeliveryIncomplete, true,
		completeAttestation(challenge, 120, 30_000),
	)
	if result.DeliveryIntegrity != deliveryIntegrityIncomplete || result.ModelAttestation != modelAttestationPass ||
		result.CognitionAssimilation != cognitionAssimilationUncertain {
		t.Fatalf("Host truncation did not prevent complete assimilation: %+v", result)
	}
}

func TestModelCognitionAttestationDirtyCognitionCannotBecomeComplete(t *testing.T) {
	challenge := syntheticChallenge(120)
	result := assessOverviewAttestation(
		challenge, 120, 30_000, hostDeliveryConfirmed, false,
		completeAttestation(challenge, 120, 30_000),
	)
	if result.ModelAttestation != modelAttestationPass || result.CognitionAssimilation != cognitionAssimilationUncertain {
		t.Fatalf("dirty cognition became reliable: %+v", result)
	}
}

func TestModelCognitionAttestationPromptSupportsBothLocales(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	seen := map[string]string{}
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		prompt := attestationPrompt()
		if !strings.Contains(prompt, "ordinal") || len(prompt) < 80 {
			t.Fatalf("%s Attestation prompt is incomplete: %q", locale, prompt)
		}
		seen[locale] = prompt
	}
	if seen[textassets.DefaultLocale] == seen[textassets.LegacyLocale] {
		t.Fatal("official Locales unexpectedly share one Attestation prompt")
	}
}

func TestLegacyAttestationPromptMatchesByteCompatibilityGolden(t *testing.T) {
	goldenBytes, err := os.ReadFile(filepath.Join(
		"..", "..", "testdata", "golden", "overview_legacy_attestation_prompts.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatal(err)
	}

	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(attestationPrompt()))
		actual := fmt.Sprintf("%x", digest[:])
		if actual != golden[locale] {
			t.Fatalf("%s Legacy Attestation Prompt bytes changed: actual=%s expected=%s", locale, actual, golden[locale])
		}
	}
}
