package mcptools

import (
	"strings"
	"testing"
)

func lineTestReceipt(scopeIdentity string) cognitionReceipt {
	return cognitionReceipt{
		Version: 2, RuntimeRepositoryRoot: "/repo", MCPServiceVersion: "v-test",
		Scope: "all", LayoutMode: "volumes-v1", RequestedScope: "all", EffectiveScope: "all",
		ScopeIdentity: scopeIdentity, ModelScopeReliable: true, ModelFullReliable: true,
	}
}

// The cognition line is the first step against both extremes: in the normal state it gives the licence "no recall needed", escalates only on a real signal,
// and never fabricates a verdict when the identity has moved past what the session knows.
func TestCognitionStatusLineStates(t *testing.T) {
	current := lineTestReceipt("aaaa")

	// A new process without a delivery: suggest the opening recall.
	session := newCognitionRefreshSession()
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_ready_for_overview") ||
		!strings.Contains(line, "no-argument aoci_overview") {
		t.Fatalf("初始态应提示完整召回: %q", line)
	}

	// Attested and the identity unchanged: the licence.
	session.established = true
	session.generation = 1
	session.lastReceipt = lineTestReceipt("aaaa")
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_not_required") ||
		strings.Contains(line, "next:") {
		t.Fatalf("身份未动应是不带指令的许可证: %q", line)
	}

	// Our own write advanced the identity, but the write path already proved the new identity aligned: still the licence.
	// This is exactly the core branch against excessive recall.
	moved := lineTestReceipt("bbbb")
	session.RecordAlignedIdentity("bbbb", true)
	if line := session.cognitionStatusLine(moved); !strings.Contains(line, "refresh_status=refresh_not_required") {
		t.Fatalf("对齐缓存命中应是许可证: %q", line)
	}

	// The identity has moved past everything the session knows: fabricate no refresh_status, only suggest a cheap checkpoint.
	unknown := lineTestReceipt("cccc")
	if line := session.cognitionStatusLine(unknown); !strings.Contains(line, "checkpoint=recommended") ||
		strings.Contains(line, "refresh_status=") {
		t.Fatalf("未知身份不得伪造判定: %q", line)
	}

	// A host-declared reason is pending and the identity unchanged: escalate to ready_for_overview with the complete chain instructions.
	session.pendingReasons["context_compaction"] = true
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_ready_for_overview") ||
		!strings.Contains(line, "reasons=context_compaction") {
		t.Fatalf("挂起理由应升级并点名理由: %q", line)
	}

	// A nil session and the Legacy layout: the suffix is empty and the read tool outputs as it is.
	if suffix := sessionCognitionSuffix("/repo", "v-test", nil, session); suffix != "" {
		t.Fatalf("缺 set 时后缀应为空: %q", suffix)
	}
}

// Probe question determinism plus the scoring boundary: an unanswered question counts as a mismatch, and stale is not forgetting.
func TestCognitionProbeBuildAndGrade(t *testing.T) {
	targets := make([]overviewChallengeTarget, 0, 9)
	for index := 0; index < 9; index++ {
		targets = append(targets, overviewChallengeTarget{
			ObjectIdentity: "src/file" + string(rune('a'+index)) + ".go",
			Tag:            "CG5T",
			CoreF:          "responsibility " + string(rune('a'+index)),
		})
	}
	first := buildCognitionProbe("1111", 3, targets)
	second := buildCognitionProbe("1111", 3, targets)
	if first.Digest != second.Digest || len(first.Ordinals) != cognitionProbeCount ||
		first.Ordinals[0] == first.Ordinals[1] {
		t.Fatalf("同一 (index, generation) 出题必须确定且不重复: %+v vs %+v", first, second)
	}
	if third := buildCognitionProbe("2222", 3, targets); third.Digest == first.Digest {
		t.Fatal("索引变化必须改变题目身份")
	}

	correct := make([]overviewChallengeAnswer, 0, len(first.Ordinals))
	for _, ordinal := range first.Ordinals {
		target := targets[ordinal-1]
		correct = append(correct, overviewChallengeAnswer{
			Ordinal: ordinal, ObjectIdentity: target.ObjectIdentity, Tag: target.Tag, CoreF: target.CoreF,
		})
	}
	pass := gradeCognitionProbe(&overviewProbeAnswers{Version: cognitionProbeV1, Digest: first.Digest, Answers: correct},
		"1111", 3, targets)
	if pass.Result != probeResultPass || pass.Passed != pass.Total {
		t.Fatalf("正确作答应 pass: %+v", pass)
	}

	// Only one question answered: the unanswered one counts as a mismatch, and the result is fail.
	partial := gradeCognitionProbe(&overviewProbeAnswers{Version: cognitionProbeV1, Digest: first.Digest, Answers: correct[:1]},
		"1111", 3, targets)
	if partial.Result != probeResultFail || len(partial.MismatchedOrdinals) != 1 {
		t.Fatalf("漏答应 fail 并点名: %+v", partial)
	}

	// A stale digest: stale rather than fail; staleness is not evidence of forgetting.
	stale := gradeCognitionProbe(&overviewProbeAnswers{Version: cognitionProbeV1, Digest: strings.Repeat("0", 64), Answers: correct},
		"1111", 3, targets)
	if stale.Result != probeResultStale {
		t.Fatalf("题不对版应 stale: %+v", stale)
	}
}
