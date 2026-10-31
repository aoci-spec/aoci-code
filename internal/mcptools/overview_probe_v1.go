// The micro cognition probe: it turns "does the model claim it still remembers the system" into a machine verdict.
//
// A complete attestation binds one complete delivery, ten questions, and consumes a generation: heavy. The probe reuses the same formal targets
// (ordinal → identity/Tag/core F) but asks only two questions, runs only inside check_only, and never changes any
// session state: no generation advance, no refresh reason registered, no receipt produced. It is pure measurement.
//
// Preventing excessive recall is its first purpose: a correct answer proves "cognition is still there, no need to resend the whole index"; a wrong answer is
// the machine evidence worth one recall, and the host then declares context_compaction under the existing contract: the machine
// measures, the host declares, the vocabulary is unchanged. Answers must come from memory: calling
// Search/Get Entries to look up answers between receiving the questions and submitting the answers defeats the measurement, which the contract forbids exactly as for the complete attestation.
package mcptools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	cognitionProbeV1     = "cognition-probe/v1"
	cognitionProbeCount  = 2
	probeResultPass      = "pass"
	probeResultFail      = "fail"
	probeResultStale     = "stale"
	probeGuidancePass    = "cognition retained; keep reusing it and do not re-request the Whole-Index"
	probeGuidanceFail    = "cognition loss measured; declare context_compaction: call aoci_overview with refresh_reasons=[\"context_compaction\"] and a refresh_event_id, then follow next_cursor until completed=true"
	probeGuidanceStale   = "the index changed since this probe was issued; request a fresh probe with check_only=true and probe=true"
	probeAnswerFromMemo  = "answer each ordinal from memory with its exact repository-relative path or canonical object identity, tag, and core F; consulting Search or Get Entries between probe and answer defeats the measurement"
	probeOrdinalDomainV1 = "cognition-probe-ordinals/v1"
)

type cognitionProbe struct {
	Version            string `json:"version"`
	IndexSHA256        string `json:"index_sha256"`
	EntryCount         int    `json:"entry_count"`
	Generation         int    `json:"generation"`
	Ordinals           []int  `json:"ordinals"`
	Digest             string `json:"digest"`
	AnswerInstructions string `json:"answer_instructions"`
}

type overviewProbeAnswers struct {
	Version string                    `json:"version"`
	Digest  string                    `json:"digest"`
	Answers []overviewChallengeAnswer `json:"answers"`
}

type cognitionProbeResult struct {
	Version            string `json:"version"`
	Result             string `json:"result"`
	Passed             int    `json:"passed"`
	Total              int    `json:"total"`
	MismatchedOrdinals []int  `json:"mismatched_ordinals,omitempty"`
	Guidance           string `json:"guidance"`
}

// buildCognitionProbe draws questions deterministically from the current formal sequence. Within one (index, generation) the
// questions are stable; as soon as the index or the generation changes the questions change, and a stale answer is refused by the digest.
func buildCognitionProbe(indexSHA256 string, generation int, targets []overviewChallengeTarget) *cognitionProbe {
	if len(targets) == 0 {
		return nil
	}
	ordinals := probeOrdinals(indexSHA256, generation, len(targets))
	probe := &cognitionProbe{
		Version: cognitionProbeV1, IndexSHA256: indexSHA256, EntryCount: len(targets),
		Generation: generation, Ordinals: ordinals,
		AnswerInstructions: probeAnswerFromMemo,
	}
	probe.Digest = probeDigest(probe)
	return probe
}

func probeOrdinals(indexSHA256 string, generation, entryCount int) []int {
	seed := sha256.Sum256([]byte(probeOrdinalDomainV1 + "\x00" + indexSHA256 + "\x00" + fmt.Sprint(generation)))
	count := cognitionProbeCount
	if entryCount < count {
		count = entryCount
	}
	ordinals := make([]int, 0, count)
	used := map[int]bool{}
	for slot := 0; len(ordinals) < count; slot++ {
		offset := (slot * 8) % (len(seed) - 8)
		ordinal := 1 + int(binary.BigEndian.Uint64(seed[offset:offset+8])%uint64(entryCount))
		for used[ordinal] {
			ordinal = 1 + ordinal%entryCount
		}
		used[ordinal] = true
		ordinals = append(ordinals, ordinal)
	}
	return ordinals
}

func probeDigest(probe *cognitionProbe) string {
	parts := []string{cognitionProbeV1, probe.IndexSHA256, fmt.Sprint(probe.Generation)}
	for _, ordinal := range probe.Ordinals {
		parts = append(parts, fmt.Sprint(ordinal))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// gradeCognitionProbe scores the from-memory answers with exactly the complete attestation's criteria (identity by equivalent spelling / Tag exact / core F
// exact without whitespace). When the index has advanced or the questions are of another version it reports stale rather than fail:
// staleness is not evidence of forgetting.
func gradeCognitionProbe(
	answers *overviewProbeAnswers,
	indexSHA256 string,
	generation int,
	targets []overviewChallengeTarget,
) *cognitionProbeResult {
	current := buildCognitionProbe(indexSHA256, generation, targets)
	result := &cognitionProbeResult{Version: cognitionProbeV1, Total: cognitionProbeCount}
	if current == nil || answers == nil || answers.Version != cognitionProbeV1 || answers.Digest != current.Digest {
		result.Result = probeResultStale
		result.Guidance = probeGuidanceStale
		if current != nil {
			result.Total = len(current.Ordinals)
		}
		return result
	}
	result.Total = len(current.Ordinals)
	byOrdinal := map[int]overviewChallengeTarget{}
	for position, target := range targets {
		byOrdinal[position+1] = target
	}
	answered := map[int]bool{}
	for _, answer := range answers.Answers {
		target, ok := byOrdinal[answer.Ordinal]
		if !ok || answered[answer.Ordinal] || !containsOrdinal(current.Ordinals, answer.Ordinal) {
			continue
		}
		answered[answer.Ordinal] = true
		if overviewAnswerIdentityMatches(answer.ObjectIdentity, target.ObjectIdentity) &&
			answer.Tag == target.Tag &&
			strings.TrimSpace(answer.CoreF) == strings.TrimSpace(target.CoreF) {
			result.Passed++
		} else {
			result.MismatchedOrdinals = append(result.MismatchedOrdinals, answer.Ordinal)
		}
	}
	for _, ordinal := range current.Ordinals {
		if !answered[ordinal] {
			result.MismatchedOrdinals = append(result.MismatchedOrdinals, ordinal)
		}
	}
	if result.Passed == result.Total {
		result.Result = probeResultPass
		result.Guidance = probeGuidancePass
	} else {
		result.Result = probeResultFail
		result.Guidance = probeGuidanceFail
	}
	return result
}

func containsOrdinal(ordinals []int, ordinal int) bool {
	for _, value := range ordinals {
		if value == ordinal {
			return true
		}
	}
	return false
}
