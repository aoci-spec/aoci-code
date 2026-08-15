package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	modelCognitionAttestationV1 = machinecontract.ModelCognitionAttestationV1
	overviewChallengeVersion    = "overview-cognition-challenge/v1"
	overviewChallengeSize       = 10

	deliveryIntegrityConfirmed   = "confirmed"
	deliveryIntegrityUnconfirmed = "unconfirmed"
	deliveryIntegrityIncomplete  = "incomplete"

	modelAttestationPass        = "pass"
	modelAttestationPartial     = "partial"
	modelAttestationFail        = "fail"
	modelAttestationNotProvided = "not_provided"

	cognitionAssimilationComplete  = "complete"
	cognitionAssimilationPartial   = "partial"
	cognitionAssimilationUncertain = "uncertain"

	attestationCoverageComplete = 95.0
	attestationMasteryComplete  = 80.0

	// The Challenge measures assimilation, not verbatim recall. Delivery
	// integrity is already proven by the host receipt over the exact bytes, so
	// the answers only need to show that the model holds the sequence: at least
	// this share of ordinals fully correct, with object identity — the one
	// discrete fact that has no paraphrase — missed at most once. Core F is
	// judged as a semantic match (see overviewCoreFMatches); Tag stays exact.
	attestationChallengePassPercent = 80
	attestationIdentityMissMax      = 1
	// Jaccard similarity floor over normalized core-F tokens. A paraphrase or a
	// dropped trailing clause of the recalled responsibility stays well above
	// it; a different Entry's F, even a formulaic sibling, stays well below.
	attestationCoreFSimilarityMin = 0.6
)

type overviewChallengeAnswer struct {
	Ordinal        int    `json:"ordinal"`
	ObjectIdentity string `json:"object_identity"`
	Tag            string `json:"tag"`
	CoreF          string `json:"core_f"`
}

type overviewModelAttestation struct {
	Version                 string                    `json:"version"`
	IndexSHA256             string                    `json:"index_sha256"`
	EntrySequenceSHA256     string                    `json:"entry_sequence_sha256"`
	EntryCount              int                       `json:"entry_count"`
	ChallengeDigest         string                    `json:"challenge_digest"`
	ReportedEntryCount      int                       `json:"reported_entry_count"`
	ReportedEstimatedTokens int                       `json:"reported_estimated_tokens"`
	CoveragePercent         float64                   `json:"coverage_percent"`
	SystemMasteryPercent    float64                   `json:"system_mastery_percent"`
	ConfidencePercent       float64                   `json:"confidence_percent"`
	TruncationDetected      bool                      `json:"truncation_detected"`
	UnseenSections          []string                  `json:"unseen_sections"`
	UncertaintyReasons      []string                  `json:"uncertainty_reasons"`
	ChallengeAnswers        []overviewChallengeAnswer `json:"challenge_answers"`
}

type overviewChallengeTarget struct {
	ContentOffset  int
	Ordinal        int
	ObjectIdentity string
	Tag            string
	CoreF          string
}

type overviewChallenge struct {
	Version             string
	IndexSHA256         string
	EntrySequenceSHA256 string
	EntryCount          int
	Digest              string
	Ordinals            []int
	Targets             map[int]overviewChallengeTarget
}

type overviewAttestationResult struct {
	DeliveryIntegrity        string
	ModelAttestation         string
	CognitionAssimilation    string
	ReportProvided           bool
	EnvelopeValid            bool
	ChallengeAnswersComplete bool
	ChallengeAnswersOrdered  bool
	ObjectIdentityMismatch   bool
	ObjectIdentityMissCount  int
	TagMismatch              bool
	CoreFMismatch            bool
	ReportedEntryCountMatch  bool
	ReportedTokensMatch      bool
	StrictFailureReasons     []string
	ChallengePassed          int
	ChallengeTotal           int
	ReportedEntryCount       int
	ReportedEstimatedTokens  int
	CoveragePercent          float64
	SystemMasteryPercent     float64
	ConfidencePercent        float64
	TruncationDetected       bool
	UnseenSections           []string
	UncertaintyReasons       []string
}

// legacyOverviewSequence is the single formal Entry order used by both Chunk
// planning and Attestation. Entry.LineNo binds each semantic object to the
// exact delivered bytes without reparsing Header examples as formal Entries.
func legacyOverviewSequence(document *index.Document, content string) ([]overviewChallengeTarget, error) {
	targets := make([]overviewChallengeTarget, 0)
	if document != nil {
		for _, section := range document.Sections {
			if section.AbsPath == "" {
				continue
			}
			for _, entry := range section.Entries {
				offset, err := overviewObjectLineOffset(content, entry.LineNo, entry.FullLine)
				if err != nil {
					return nil, err
				}
				targets = append(targets, overviewChallengeTarget{
					ContentOffset:  offset,
					ObjectIdentity: entry.RelPath,
					Tag:            entry.TagsRaw,
					CoreF:          entry.F,
				})
			}
		}
	}
	return targets, nil
}

func overviewObjectLineOffset(content string, lineNumber int, expectedLine string) (int, error) {
	if lineNumber < 1 {
		return 0, fmt.Errorf("overview_cognition_sequence_line_invalid")
	}
	offset := 0
	for line := 1; line < lineNumber; line++ {
		newline := strings.IndexByte(content[offset:], '\n')
		if newline < 0 {
			return 0, fmt.Errorf("overview_cognition_sequence_line_missing")
		}
		offset += newline + 1
	}
	end := len(content)
	if newline := strings.IndexByte(content[offset:], '\n'); newline >= 0 {
		end = offset + newline
	}
	actual := strings.TrimSuffix(content[offset:end], "\r")
	if actual != expectedLine {
		return 0, fmt.Errorf("overview_cognition_sequence_line_mismatch")
	}
	return offset, nil
}

func buildOverviewChallenge(indexSHA256 string, targets []overviewChallengeTarget) overviewChallenge {
	entrySequenceSHA256 := overviewEntrySequenceSHA256(targets)
	ordinals := stratifiedChallengeOrdinals(targets)
	byOrdinal := make(map[int]overviewChallengeTarget, len(ordinals))
	for _, ordinal := range ordinals {
		target := targets[ordinal-1]
		target.Ordinal = ordinal
		byOrdinal[ordinal] = target
	}
	payload, _ := json.Marshal(struct {
		Version             string `json:"version"`
		IndexSHA256         string `json:"index_sha256"`
		EntrySequenceSHA256 string `json:"entry_sequence_sha256"`
		EntryCount          int    `json:"entry_count"`
		Ordinals            []int  `json:"ordinals"`
	}{overviewChallengeVersion, indexSHA256, entrySequenceSHA256, len(targets), ordinals})
	digest := sha256.Sum256(payload)
	return overviewChallenge{
		Version: overviewChallengeVersion, IndexSHA256: indexSHA256,
		EntrySequenceSHA256: entrySequenceSHA256, EntryCount: len(targets),
		Digest: hex.EncodeToString(digest[:]), Ordinals: ordinals, Targets: byOrdinal,
	}
}

func overviewEntrySequenceSHA256(targets []overviewChallengeTarget) string {
	payload := make([]struct {
		Ordinal        int    `json:"ordinal"`
		ObjectIdentity string `json:"object_identity"`
		Tag            string `json:"tag"`
		CoreF          string `json:"core_f"`
	}, len(targets))
	for index, target := range targets {
		payload[index].Ordinal = index + 1
		payload[index].ObjectIdentity = target.ObjectIdentity
		payload[index].Tag = target.Tag
		payload[index].CoreF = target.CoreF
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// stratifiedChallengeOrdinals selects one object from every current-sequence
// stratum. The per-object key keeps unaffected targets stable across nearby
// Entry additions or removals, while the returned ordinal is always its
// position in the newly delivered sequence.
func stratifiedChallengeOrdinals(targets []overviewChallengeTarget) []int {
	entryCount := len(targets)
	if entryCount <= 0 {
		return nil
	}
	count := overviewChallengeSize
	if entryCount < count {
		count = entryCount
	}
	ordinals := make([]int, 0, count)
	for bucket := 0; bucket < count; bucket++ {
		start := bucket*entryCount/count + 1
		end := (bucket + 1) * entryCount / count
		if end < start {
			end = start
		}
		selected := start
		selectedKey := overviewChallengeSelectionKey(targets[start-1])
		for ordinal := start + 1; ordinal <= end; ordinal++ {
			candidateKey := overviewChallengeSelectionKey(targets[ordinal-1])
			if candidateKey < selectedKey {
				selected = ordinal
				selectedKey = candidateKey
			}
		}
		ordinals = append(ordinals, selected)
	}
	return ordinals
}

func overviewChallengeSelectionKey(target overviewChallengeTarget) string {
	digest := sha256.Sum256([]byte(overviewChallengeVersion + "\x00" + target.ObjectIdentity))
	return hex.EncodeToString(digest[:])
}

func assessOverviewAttestation(
	challenge overviewChallenge,
	expectedEntryCount, expectedTokens int,
	hostStatus string,
	governanceAligned bool,
	report *overviewModelAttestation,
) overviewAttestationResult {
	result := overviewAttestationResult{
		DeliveryIntegrity:     deliveryIntegrityFromHostStatus(hostStatus),
		ModelAttestation:      modelAttestationNotProvided,
		CognitionAssimilation: cognitionAssimilationUncertain,
		ChallengeTotal:        len(challenge.Ordinals),
	}
	if report == nil {
		result.StrictFailureReasons = []string{machinecontract.StrictAttestationReasonNotProvided}
		return result
	}
	result.ReportProvided = true
	result.ReportedEntryCount = report.ReportedEntryCount
	result.ReportedEstimatedTokens = report.ReportedEstimatedTokens
	result.CoveragePercent = report.CoveragePercent
	result.SystemMasteryPercent = report.SystemMasteryPercent
	result.ConfidencePercent = report.ConfidencePercent
	result.TruncationDetected = report.TruncationDetected
	result.UnseenSections = append([]string{}, report.UnseenSections...)
	result.UncertaintyReasons = append([]string{}, report.UncertaintyReasons...)

	validEnvelope := report.Version == modelCognitionAttestationV1 &&
		report.IndexSHA256 == challenge.IndexSHA256 &&
		report.EntrySequenceSHA256 == challenge.EntrySequenceSHA256 &&
		report.EntryCount == challenge.EntryCount &&
		report.ChallengeDigest == challenge.Digest &&
		validPercent(report.CoveragePercent) && validPercent(report.SystemMasteryPercent) &&
		validPercent(report.ConfidencePercent) && report.ReportedEntryCount >= 0 &&
		report.ReportedEstimatedTokens >= 0
	result.EnvelopeValid = validEnvelope
	result.ChallengeAnswersComplete = len(report.ChallengeAnswers) == len(challenge.Ordinals)
	ordered := result.ChallengeAnswersComplete
	seen := make(map[int]struct{}, len(report.ChallengeAnswers))
	for answerIndex, answer := range report.ChallengeAnswers {
		if _, duplicate := seen[answer.Ordinal]; duplicate {
			ordered = false
		}
		seen[answer.Ordinal] = struct{}{}
		if answerIndex >= len(challenge.Ordinals) || answer.Ordinal != challenge.Ordinals[answerIndex] {
			ordered = false
		}
		target, ok := challenge.Targets[answer.Ordinal]
		if !ok {
			continue
		}
		identityMatches := overviewAnswerIdentityMatches(answer.ObjectIdentity, target.ObjectIdentity)
		tagMatches := answer.Tag == target.Tag
		coreFMatches := overviewCoreFMatches(answer.CoreF, target.CoreF)
		if !identityMatches {
			result.ObjectIdentityMissCount++
		}
		result.ObjectIdentityMismatch = result.ObjectIdentityMismatch || !identityMatches
		result.TagMismatch = result.TagMismatch || !tagMatches
		result.CoreFMismatch = result.CoreFMismatch || !coreFMatches
		if identityMatches && tagMatches && coreFMatches {
			result.ChallengePassed++
		}
	}
	result.ChallengeAnswersOrdered = ordered
	countMatches := reportedEntryCountMatch(report.ReportedEntryCount, expectedEntryCount)
	tokensMatch := estimatedTokensMatch(report.ReportedEstimatedTokens, expectedTokens)
	result.ReportedEntryCountMatch = countMatches
	result.ReportedTokensMatch = tokensMatch
	challengeMet := ordered &&
		challengePassRatioMet(result.ChallengePassed, result.ChallengeTotal) &&
		result.ObjectIdentityMissCount <= attestationIdentityMissMax
	claimsComplete := report.CoveragePercent >= attestationCoverageComplete &&
		!report.TruncationDetected && countMatches
	// fail is reserved for an Attestation that proves nothing: a foreign or
	// malformed envelope, or not one ordinal recalled. Every other shortfall is
	// partial — including an honest complete-coverage claim that misses the
	// pass ratio, which must never grade below the same answers submitted with
	// a hedged coverage claim.
	if validEnvelope && challengeMet && countMatches && tokensMatch && claimsComplete {
		result.ModelAttestation = modelAttestationPass
	} else if !validEnvelope || result.ChallengePassed == 0 {
		result.ModelAttestation = modelAttestationFail
	} else {
		result.ModelAttestation = modelAttestationPartial
	}
	if result.ModelAttestation != modelAttestationPass {
		result.StrictFailureReasons = strictAttestationFailureReasons(result, report)
	}

	if result.DeliveryIntegrity != deliveryIntegrityConfirmed || !governanceAligned ||
		result.ModelAttestation == modelAttestationFail || result.ModelAttestation == modelAttestationNotProvided {
		result.CognitionAssimilation = cognitionAssimilationUncertain
	} else if result.ModelAttestation == modelAttestationPass &&
		report.SystemMasteryPercent >= attestationMasteryComplete {
		result.CognitionAssimilation = cognitionAssimilationComplete
	} else {
		result.CognitionAssimilation = cognitionAssimilationPartial
	}
	return result
}

func strictAttestationFailureReasons(
	result overviewAttestationResult,
	report *overviewModelAttestation,
) []string {
	if report == nil {
		return []string{machinecontract.StrictAttestationReasonNotProvided}
	}
	reasons := make([]string, 0, 10)
	if !result.EnvelopeValid {
		reasons = append(reasons, machinecontract.StrictAttestationReasonEnvelopeIdentityMismatch)
	}
	if !result.ChallengeAnswersComplete {
		reasons = append(reasons, machinecontract.StrictAttestationReasonAnswerCountMismatch)
	}
	if !result.ChallengeAnswersOrdered {
		reasons = append(reasons, machinecontract.StrictAttestationReasonOrdinalOrderMismatch)
	}
	if result.ObjectIdentityMismatch {
		reasons = append(reasons, machinecontract.StrictAttestationReasonObjectIdentityMismatch)
	}
	if result.TagMismatch {
		reasons = append(reasons, machinecontract.StrictAttestationReasonTagMismatch)
	}
	if result.CoreFMismatch {
		reasons = append(reasons, machinecontract.StrictAttestationReasonCoreFMismatch)
	}
	if !result.ReportedEntryCountMatch {
		reasons = append(reasons, machinecontract.StrictAttestationReasonReportedEntryMismatch)
	}
	if !result.ReportedTokensMatch {
		reasons = append(reasons, machinecontract.StrictAttestationReasonReportedTokenMismatch)
	}
	if report.CoveragePercent < attestationCoverageComplete {
		reasons = append(reasons, machinecontract.StrictAttestationReasonCoverageBelowThreshold)
	}
	if report.TruncationDetected {
		reasons = append(reasons, machinecontract.StrictAttestationReasonTruncationDetected)
	}
	return reasons
}

// overviewAnswerIdentityMatches preserves the canonical identity as the
// machine authority while accepting the exact repository-relative spelling
// that the public Attestation contract has always allowed for a Code object.
// The mapping is one-to-one: no cleaning, case folding, basename lookup, or
// fuzzy resolution is performed. Database and Legacy identities remain exact.
func overviewAnswerIdentityMatches(answer, canonical string) bool {
	if answer == canonical {
		return true
	}
	if strings.HasPrefix(canonical, "code:") {
		rel := strings.TrimPrefix(canonical, "code:")
		return answer == rel && cognition.ExpectedOwner(canonical) == cognition.OwnerCode
	}
	return false
}

func deliveryIntegrityFromHostStatus(status string) string {
	switch status {
	case hostDeliveryConfirmed:
		return deliveryIntegrityConfirmed
	case hostDeliveryIncomplete:
		return deliveryIntegrityIncomplete
	default:
		return deliveryIntegrityUnconfirmed
	}
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

// challengePassRatioMet applies the pass share to whatever Challenge size the
// sequence allowed: 8 of 10 ordinarily, every ordinal for a tiny Index.
func challengePassRatioMet(passed, total int) bool {
	return total > 0 && passed*100 >= total*attestationChallengePassPercent
}

// reportedEntryCountMatch treats the model's own count as the self-report it
// is: one percent, never less than one Entry, of slack. The exact machine count
// is still bound through the echoed envelope entry_count.
func reportedEntryCountMatch(reported, expected int) bool {
	tolerance := expected / 100
	if tolerance < 1 {
		tolerance = 1
	}
	return reported >= expected-tolerance && reported <= expected+tolerance
}

func estimatedTokensMatch(reported, expected int) bool {
	tolerance := expected / 20
	if tolerance < 1024 {
		tolerance = 1024
	}
	return reported >= expected-tolerance && reported <= expected+tolerance
}

// overviewCoreFMatches accepts the exact core F first, then a normalized
// token-set similarity, so a paraphrase or a dropped clause of the recalled
// responsibility still counts as recall while a different Entry's F does not.
// Object identity and Tag are never judged this way: they are discrete facts
// with no paraphrase, and the identity miss cap guards the sequence position.
func overviewCoreFMatches(answer, target string) bool {
	answer, target = strings.TrimSpace(answer), strings.TrimSpace(target)
	if answer == target {
		return true
	}
	answerTokens, targetTokens := coreFTokenSet(answer), coreFTokenSet(target)
	if len(answerTokens) == 0 || len(targetTokens) == 0 {
		return false
	}
	shared := 0
	for token := range answerTokens {
		if _, ok := targetTokens[token]; ok {
			shared++
		}
	}
	union := len(answerTokens) + len(targetTokens) - shared
	return float64(shared) >= attestationCoreFSimilarityMin*float64(union)
}

// coreFTokenSet lowercases and splits on anything that is not a letter or
// digit. Space-delimited scripts contribute whole words (single letters and a
// few English function words dropped as noise); Han, Kana, and Hangul runs,
// which carry no word boundaries, contribute character bigrams instead so the
// same similarity floor works for zh-CN, ja, and ko Entries.
func coreFTokenSet(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var word, ideographs []rune
	flushWord := func() {
		if len(word) >= 2 {
			lowered := strings.ToLower(string(word))
			if _, noise := coreFNoiseWords[lowered]; !noise {
				tokens[lowered] = struct{}{}
			}
		}
		word = word[:0]
	}
	flushIdeographs := func() {
		switch len(ideographs) {
		case 0:
		case 1:
			tokens[string(ideographs)] = struct{}{}
		default:
			for i := 0; i+1 < len(ideographs); i++ {
				tokens[string(ideographs[i:i+2])] = struct{}{}
			}
		}
		ideographs = ideographs[:0]
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			flushWord()
			ideographs = append(ideographs, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushIdeographs()
			word = append(word, r)
		default:
			flushWord()
			flushIdeographs()
		}
	}
	flushWord()
	flushIdeographs()
	return tokens
}

// coreFNoiseWords are English function words that carry no responsibility
// meaning; dropping them keeps two unrelated F sentences from looking alike.
var coreFNoiseWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "of": {}, "to": {}, "for": {},
	"in": {}, "into": {}, "with": {}, "by": {}, "on": {}, "at": {}, "from": {}, "as": {},
	"is": {}, "are": {}, "its": {}, "that": {}, "this": {}, "over": {}, "through": {},
}

func formatChallengeOrdinals(ordinals []int) string {
	parts := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		parts[i] = fmt.Sprintf("%d", ordinal)
	}
	return strings.Join(parts, ",")
}

func formatAttestationList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	clean := make([]string, len(values))
	for i, value := range values {
		clean[i] = overviewMetadataLineValue(value)
	}
	return strings.Join(clean, ";")
}

// renderOverviewAttestationMetadata keeps Legacy and Volume delivery receipts
// on one fact path. Layout-specific renderers still own their surrounding
// metadata and body, so their formal identities remain independent.
func renderOverviewAttestationMetadata(
	serverDeliveryComplete bool,
	hostStatus string,
	body overviewBodyReceipt,
	challenge overviewChallenge,
	expectedEntryCount, expectedSectionCount, expectedTokens int,
	governanceAligned bool,
	result overviewAttestationResult,
) string {
	level := assessOverviewCognitionLevel(
		serverDeliveryComplete, result.DeliveryIntegrity,
		result.ModelAttestation, governanceAligned,
	)
	return fmt.Sprintf(
		"server_delivery_complete: %t\n"+
			"host_delivery_status: %s\n"+
			"delivery_receipt_version: %s\n"+
			"body_utf8_bytes: %d\n"+
			"body_sha256: %s\n"+
			"body_start_marker: %s\n"+
			"body_end_marker: %s\n"+
			"attestation_contract_version: %s\n"+
			"challenge_version: %s\n"+
			"challenge_index_sha256: %s\n"+
			"challenge_entry_sequence_sha256: %s\n"+
			"challenge_entry_count: %d\n"+
			"challenge_digest: %s\n"+
			"challenge_ordinals: %s\n"+
			"challenge_count: %d\n"+
			"cognition_level: %d\n"+
			"cognition_level_state: %s\n"+
			"cognition_level_message: %s\n"+
			"attestation_prompt: %s\n"+
			"expected_entry_count: %d\n"+
			"expected_section_count: %d\n"+
			"expected_estimated_tokens: %d\n"+
			"delivery_integrity: %s\n"+
			"model_attestation: %s\n"+
			"cognition_assimilation: %s\n"+
			"reported_entry_count: %d\n"+
			"reported_estimated_tokens: %d\n"+
			"coverage_percent: %.2f\n"+
			"system_mastery_percent: %.2f\n"+
			"confidence_percent: %.2f\n"+
			"truncation_detected: %t\n"+
			"unseen_sections: %s\n"+
			"uncertainty_reasons: %s\n"+
			"challenge_passed: %d/%d\n",
		serverDeliveryComplete, hostStatus, overviewDeliveryReceiptV1,
		body.BodyUTF8Bytes, overviewFallbackDisplayValue(body.BodySHA256),
		overviewFallbackDisplayValue(body.StartMarker), overviewFallbackDisplayValue(body.EndMarker),
		modelCognitionAttestationV1, overviewFallbackDisplayValue(challenge.Version),
		overviewFallbackDisplayValue(challenge.IndexSHA256),
		overviewFallbackDisplayValue(challenge.EntrySequenceSHA256), challenge.EntryCount,
		overviewFallbackDisplayValue(challenge.Digest),
		overviewFallbackDisplayValue(formatChallengeOrdinals(challenge.Ordinals)), result.ChallengeTotal,
		level.Level, level.State, overviewMetadataLineValue(level.Message),
		attestationPrompt(), expectedEntryCount, expectedSectionCount, expectedTokens,
		result.DeliveryIntegrity, result.ModelAttestation, result.CognitionAssimilation,
		result.ReportedEntryCount, result.ReportedEstimatedTokens, result.CoveragePercent,
		result.SystemMasteryPercent, result.ConfidencePercent, result.TruncationDetected,
		formatAttestationList(result.UnseenSections), formatAttestationList(result.UncertaintyReasons),
		result.ChallengePassed, result.ChallengeTotal,
	)
}

func attestationPrompt() string {
	return overviewMetadataLineValue(strings.TrimSpace(
		mcpContract(textassets.ContractMCPOverviewAttestationPrompt),
	))
}

func attestationPromptForCognitionState(includeCognitionStateV2 bool) string {
	prompt := attestationPrompt()
	if !includeCognitionStateV2 {
		return prompt
	}
	return overviewMetadataLineValue(
		prompt + " " + mcpMessage("overview.cognition_state.attestation_interpretation_v2"),
	)
}
