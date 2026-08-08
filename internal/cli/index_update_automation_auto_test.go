// Endpoint Automation Auto主链测试。
//
// 覆盖干净Apply、generation失败、确定性Missing提前跳过和机器Check硬拒。
// Warning-only、冲突和有效include等边界位于独立测试文件。
package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestUpdateAutomationAutoAppliesCleanBatch(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// auto 漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:auto原子应用版 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"auto全净批次应成功: %v\n%s",
			err,
			output,
		)
	}

	indexText := readEntriesIndex(
		t,
		root,
	)

	if !strings.Contains(
		indexText,
		"F:auto原子应用版",
	) {
		t.Fatalf(
			"auto未写入正式索引:\n%s",
			indexText,
		)
	}

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Reviews) != 2 ||
		len(manifest.Applications) != 1 {
		t.Fatalf(
			"auto Check/Diff/Application审计不完整: %+v",
			manifest,
		)
	}

	checkReview := manifest.Reviews[0]
	diffReview := manifest.Reviews[1]
	application := manifest.Applications[0]

	if checkReview.Action !=
		draft.ReviewActionCheck ||
		diffReview.Action !=
			draft.ReviewActionDiff ||
		checkReview.DraftHash == "" ||
		diffReview.DraftHash !=
			checkReview.DraftHash ||
		application.DraftHash !=
			diffReview.DraftHash ||
		application.Applied != 1 ||
		application.Rejected != 0 ||
		manifest.AppliedAt !=
			application.At {
		t.Fatalf(
			"auto Check/Diff/Application摘要不一致: %+v",
			manifest,
		)
	}

	baselineState, exists, loadErr :=
		baseline.Load(root)

	if loadErr != nil ||
		!exists ||
		baselineState == nil {
		t.Fatalf(
			"auto后基线不可读: exists=%v err=%v",
			exists,
			loadErr,
		)
	}

	stale, unbaselined :=
		baseline.IsStaleFile(
			root,
			"f.go",
			baselineState,
		)

	if stale ||
		unbaselined {
		t.Fatalf(
			"auto后目标基线未前移: stale=%v unbaselined=%v",
			stale,
			unbaselined,
		)
	}

	events, _ := ledger.Recent(
		root,
		50,
	)

	foundDiff := false
	foundApply := false
	foundBatch := false

	for _, event := range events {
		if event.Op == "entries_diff" &&
			event.Source ==
				ledger.SourceCLIAI &&
			event.PathsCount == 1 {
			foundDiff = true
		}

		if event.Op == "entries_apply" &&
			event.Source ==
				ledger.SourceCLIAI &&
			event.AppliedCount == 1 {
			foundApply = true
		}

		if event.Op == "update_entries_batch" &&
			event.Source ==
				ledger.SourceCLIAI &&
			event.AppliedCount == 1 {
			foundBatch = true
		}
	}

	if !foundDiff ||
		!foundApply ||
		!foundBatch {
		t.Fatalf(
			"auto ledger不完整: diff=%v apply=%v batch=%v events=%+v",
			foundDiff,
			foundApply,
			foundBatch,
			events,
		)
	}

	for _, anchor := range []string{
		"已完成Diff审计 1 项",
		"已原子应用 1 个条目",
		"check/diff/apply draft_hash=",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"auto输出缺少%q: %s",
				anchor,
				output,
			)
		}
	}
}

func TestUpdateAutomationAutoStopsOnGenerationFailure(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// failed 漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"",
		http.StatusInternalServerError,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	indexBefore := readEntriesIndex(
		t,
		root,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)

	requireUpdateAutomationExit(
		t,
		err,
		ExitInvalid,
	)

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Entries) != 1 ||
		manifest.Entries[0].Status !=
			"failed" {
		t.Fatalf(
			"failed generation未保留: %+v",
			manifest,
		)
	}

	if len(manifest.Reviews) != 0 ||
		len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"generation不完整不得进入审阅或应用: %+v",
			manifest,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"generation failed后正式索引必须零变化",
		)
	}

	if !strings.Contains(
		output,
		"generation 完整性硬闸未通过",
	) {
		t.Fatalf(
			"failed输出缺硬闸说明: %s",
			output,
		)
	}
}

// 空文件等确定性SkippedMissing在Missing三分阶段排除，不进入模型generation。
func TestUpdateAutomationAutoSkipsDeterministicMissingBeforeGeneration(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"empty.go",
		"",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"empty.go[XC5T]: F:不应调用 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	indexBefore := readEntriesIndex(
		t,
		root,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"确定性SkippedMissing应在generation前成功收敛: %v\n%s",
			err,
			output,
		)
	}

	if endpoint.calls.Load() != 0 {
		t.Fatalf(
			"空文件不得进入端点调用,实际%d次",
			endpoint.calls.Load(),
		)
	}

	runIDs, err := draft.ListRunIDs(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(runIDs) != 0 {
		t.Fatalf(
			"只有确定性SkippedMissing时不得创建草稿: %+v",
			runIDs,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"确定性SkippedMissing不得修改正式索引",
		)
	}

	if !strings.Contains(
		output,
		"skipped_missing(确定性跳过)",
	) ||
		!strings.Contains(
			output,
			"无可执行changed/ActionableMissing目标",
		) {
		t.Fatalf(
			"输出缺Missing三分收敛说明: %s",
			output,
		)
	}
}

func TestUpdateAutomationAutoRejectsWarnedHardError(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// warned硬错漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"wrong.go[XC5T]: F:文件名错误 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	indexBefore := readEntriesIndex(
		t,
		root,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)

	requireUpdateAutomationExit(
		t,
		err,
		ExitInvalid,
	)

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Entries) != 1 ||
		manifest.Entries[0].Status !=
			"warned" {
		t.Fatalf(
			"warned generation未保留: %+v",
			manifest,
		)
	}

	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Action !=
			draft.ReviewActionCheck ||
		manifest.Reviews[0].Rejected != 1 {
		t.Fatalf(
			"warned中的硬错必须被Check拒绝且不得形成Diff: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"机器Check拒绝后不得形成application: %+v",
			manifest,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"机器Check拒绝后正式索引必须零变化",
		)
	}

	if !strings.Contains(
		output,
		"机器预检未通过",
	) {
		t.Fatalf(
			"warned硬错输出缺停点说明: %s",
			output,
		)
	}
}
