// 微型认知探针: 把"模型自述是否还记得系统"变成机器判定。
//
// 完整宣誓绑定一次完整交付、一道题、消耗代际 —— 很重。探针复用同一套形式目标
// (ordinal → 身份/Tag/核心F), 但只出两道题、只在 check_only 里走、绝不改变任何
// 会话状态: 不推进 generation、不登记刷新理由、不产生收据。它是纯测量。
//
// 反过度召回是它的第一目的: 答对即证明"认知还在, 不必重传全量索引"; 答错才是
// 值得一次召回的机器证据, 此时由宿主按既有契约声明 context_compaction —— 机器
// 测量, 宿主声明, 词表不变。作答必须凭记忆: 在收到题目与提交答案之间调用
// Search/Get Entries 查答案会让测量失去意义, 契约与完整宣誓同款禁止。
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

// buildCognitionProbe 从当前形式序列确定性地出题。同一 (index, generation) 内
// 题目稳定, 索引或代际一变题目即变, 陈旧答案由 digest 拒绝。
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

// gradeCognitionProbe 用与完整宣誓完全相同的判据(身份等价拼写/Tag 精确/核心F
// 去空白精确)给凭记忆的作答打分。索引已前移或题不对版时报 stale 而非 fail:
// 陈旧不是遗忘的证据。
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
