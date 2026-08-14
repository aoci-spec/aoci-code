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

// 认知行是防两个极端的第一级: 常态给"不必召回"的许可证, 只在真实信号下升级,
// 身份越过会话所知时绝不伪造判定。
func TestCognitionStatusLineStates(t *testing.T) {
	current := lineTestReceipt("aaaa")

	// 新进程未交付: 提示开场召回。
	session := newCognitionRefreshSession()
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_ready_for_overview") ||
		!strings.Contains(line, "no-argument aoci_overview") {
		t.Fatalf("初始态应提示完整召回: %q", line)
	}

	// 已宣誓且身份未动: 许可证。
	session.established = true
	session.generation = 1
	session.lastReceipt = lineTestReceipt("aaaa")
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_not_required") ||
		strings.Contains(line, "next:") {
		t.Fatalf("身份未动应是不带指令的许可证: %q", line)
	}

	// 自己的写入让身份前移, 但写入路径已证明新身份对齐: 仍是许可证。
	// 这正是防过度召回的核心分支。
	moved := lineTestReceipt("bbbb")
	session.RecordAlignedIdentity("bbbb", true)
	if line := session.cognitionStatusLine(moved); !strings.Contains(line, "refresh_status=refresh_not_required") {
		t.Fatalf("对齐缓存命中应是许可证: %q", line)
	}

	// 身份越过会话一切所知: 不伪造 refresh_status, 只建议廉价 checkpoint。
	unknown := lineTestReceipt("cccc")
	if line := session.cognitionStatusLine(unknown); !strings.Contains(line, "checkpoint=recommended") ||
		strings.Contains(line, "refresh_status=") {
		t.Fatalf("未知身份不得伪造判定: %q", line)
	}

	// 挂起了宿主声明的理由且身份未动: 升级为带完整链指令的 ready_for_overview。
	session.pendingReasons["context_compaction"] = true
	if line := session.cognitionStatusLine(current); !strings.Contains(line, "refresh_status=refresh_ready_for_overview") ||
		!strings.Contains(line, "reasons=context_compaction") {
		t.Fatalf("挂起理由应升级并点名理由: %q", line)
	}

	// nil 会话与 Legacy 布局: 后缀为空, 读工具原样输出。
	if suffix := sessionCognitionSuffix("/repo", "v-test", nil, session); suffix != "" {
		t.Fatalf("缺 set 时后缀应为空: %q", suffix)
	}
}

// 探针出题确定性 + 判分边界: 漏答计入失配, 陈旧不算遗忘。
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

	// 只答一题: 漏答的那题计入失配, 结果 fail。
	partial := gradeCognitionProbe(&overviewProbeAnswers{Version: cognitionProbeV1, Digest: first.Digest, Answers: correct[:1]},
		"1111", 3, targets)
	if partial.Result != probeResultFail || len(partial.MismatchedOrdinals) != 1 {
		t.Fatalf("漏答应 fail 并点名: %+v", partial)
	}

	// 陈旧 digest: stale 而非 fail —— 陈旧不是遗忘的证据。
	stale := gradeCognitionProbe(&overviewProbeAnswers{Version: cognitionProbeV1, Digest: strings.Repeat("0", 64), Answers: correct},
		"1111", 3, targets)
	if stale.Result != probeResultStale {
		t.Fatalf("题不对版应 stale: %+v", stale)
	}
}
