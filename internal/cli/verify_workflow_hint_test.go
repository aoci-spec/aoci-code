// Verify对Pending阶段提示与排除治理措辞的状态机一致性测试。
package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

func pendingHintTestReport() *verifyReport {
	return &verifyReport{
		Root:           "/repo",
		IndexEntries:   1,
		DiskFiles:      3,
		BaselineExists: true,
		Result: &baseline.DetectResult{
			Missing:        []string{"empty.txt"},
			Orphan:         []string{},
			Stale:          []string{},
			Unbaselined:    []string{},
			LineEndingOnly: []string{},
		},
		RawMissing: []string{
			"empty.txt",
		},
		SkippedMissing: nil,
		PendingCurationMissing: []curation.PendingCandidate{
			{
				Path:          "empty.txt",
				ProfileReason: "empty",
				SourceSHA256:  strings.Repeat("a", 64),
			},
		},
		GeneratedAt: "2026-07-21T00:00:00Z",
	}
}

func TestRenderVerifyMixedActionableAndPendingExplainsEntriesFirst(
	t *testing.T,
) {
	report := pendingHintTestReport()
	report.Result.Missing = []string{
		"src/main.go",
		"empty.txt",
	}
	report.RawMissing = append(
		[]string{},
		report.Result.Missing...,
	)
	report.ActionableMissing = []string{
		"src/main.go",
	}

	text := renderVerifyHuman(report)

	for _, anchor := range []string{
		"当前同时存在ActionableMissing与PendingCurationMissing",
		"先进入Entries Stage",
		"Actionable清零后再进入Curation Stage",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"混合态提示缺少%q:\n%s",
				anchor,
				text,
			)
		}
	}

	if strings.Contains(
		text,
		"请运行aoci index agent guide进入Curation Stage",
	) {
		t.Fatalf(
			"混合态不得无条件声称下一步即Curation:\n%s",
			text,
		)
	}
}

func TestRenderVerifyOnlyPendingUsesConditionalCurationHint(
	t *testing.T,
) {
	report := pendingHintTestReport()

	text := renderVerifyHuman(report)

	for _, anchor := range []string{
		"请运行aoci index agent guide取得当前真实阶段",
		"Guide将进入Curation Stage",
		"不能把Pending当作治理完成",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"仅Pending提示缺少%q:\n%s",
				anchor,
				text,
			)
		}
	}
}

func TestRenderVerifyExcludedUsesGovernanceWording(
	t *testing.T,
) {
	report := &verifyReport{
		Root:           "/repo",
		IndexEntries:   1,
		DiskFiles:      1,
		BaselineExists: true,
		Result: &baseline.DetectResult{
			Missing:        []string{"docs/x.md"},
			Orphan:         []string{},
			Stale:          []string{},
			Unbaselined:    []string{},
			LineEndingOnly: []string{},
		},
		RawMissing: []string{
			"docs/x.md",
		},
		CurationExcludedMissing: []string{
			"docs/x.md",
		},
		GeneratedAt: "2026-07-21T00:00:00Z",
	}

	text := renderVerifyHuman(report)

	for _, anchor := range []string{
		"已完成排除治理决策",
		"已解释排除治理结果",
		"不会制造失败退出码",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"排除治理输出缺少%q:\n%s",
				anchor,
				text,
			)
		}
	}

	if strings.Contains(
		text,
		"负空间",
	) {
		t.Fatalf(
			"Verify人读输出不得继续使用负空间术语:\n%s",
			text,
		)
	}
}
