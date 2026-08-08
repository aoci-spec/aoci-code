// R58 automation四模式边界测试。
//
// 本文件与automation主测试分离，避免单个测试文件超过600行。
// 覆盖legacy、off无AI配置、review拒绝和Auto Warning-only四个边界。
package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestUpdateAutomationOffWorksWithoutAIConfig(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// off且无AI配置\n",
	)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.SetAutomationMode(
		config.AutomationModeOff,
	); err != nil {
		t.Fatal(err)
	}

	cfg.AI.Enabled = false
	cfg.AI.BaseURL = ""
	cfg.AI.Model = ""
	cfg.AI.APIKeyEnv = "AOCI_TEST_MISSING_KEY"

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

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
			"off不得要求AI配置: %v\n%s",
			err,
			output,
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
			"off不得创建草稿: %+v",
			runIDs,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"off不得修改正式索引",
		)
	}

	if !strings.Contains(
		output,
		"未构造 AI client",
	) {
		t.Fatalf(
			"off输出未证明在AI client前早退: %s",
			output,
		)
	}
}

func TestUpdateAutomationLegacyDraftsOnly(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// legacy漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:legacy草稿版 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeLegacy,
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
			"legacy应保持旧起草行为: %v\n%s",
			err,
			output,
		)
	}

	if endpoint.calls.Load() != 1 {
		t.Fatalf(
			"legacy应调用端点一次,得到%d",
			endpoint.calls.Load(),
		)
	}

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Entries) != 1 {
		t.Fatalf(
			"legacy应保留generation state: %+v",
			manifest,
		)
	}

	if len(manifest.Reviews) != 0 {
		t.Fatalf(
			"legacy不得自动Check或Diff: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"legacy不得自动Apply: %+v",
			manifest,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"legacy不得修改正式索引",
		)
	}

	if !strings.Contains(
		output,
		"entries check <run_id>",
	) {
		t.Fatalf(
			"legacy输出缺人工后续链: %s",
			output,
		)
	}
}

func TestUpdateAutomationReviewRejectsAndStops(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// review拒绝漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"wrong.go[XC5T]: F:文件名错误 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeReview,
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
		manifest.Entries[0].Status != "warned" {
		t.Fatalf(
			"review应保留带病generation: %+v",
			manifest.Entries,
		)
	}

	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Action !=
			draft.ReviewActionCheck ||
		manifest.Reviews[0].Rejected != 1 {
		t.Fatalf(
			"review拒绝摘要不符: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"review拒绝后不得应用: %+v",
			manifest,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"review拒绝后正式索引必须零变化",
		)
	}

	if !strings.Contains(
		output,
		"已停在草稿区",
	) {
		t.Fatalf(
			"review拒绝输出缺停点说明: %s",
			output,
		)
	}
}

func TestUpdateAutomationAutoAppliesWarningOnly(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// warning-only漂移\n",
	)

	longConstraint := strings.Repeat(
		"高熵约束",
		20,
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC3T]: F:warning放行版 | R:- | A:- | S:"+
			longConstraint,
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
			"Auto Warning-only应允许应用: %v\n%s",
			err,
			output,
		)
	}

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Entries) != 1 ||
		manifest.Entries[0].Status != "warned" {
		t.Fatalf(
			"generation应保留warned: %+v",
			manifest.Entries,
		)
	}

	if len(manifest.Reviews) != 2 {
		t.Fatalf(
			"Warning-only应形成Check与Diff审计: %+v",
			manifest.Reviews,
		)
	}

	checkReview := manifest.Reviews[0]
	diffReview := manifest.Reviews[1]

	if checkReview.Action !=
		draft.ReviewActionCheck ||
		checkReview.Warned != 1 ||
		checkReview.Rejected != 0 ||
		checkReview.Skipped != 0 ||
		checkReview.Passed != 1 ||
		diffReview.Action !=
			draft.ReviewActionDiff ||
		diffReview.Warned != 0 ||
		diffReview.Rejected != 0 ||
		diffReview.Skipped != 0 ||
		diffReview.Passed != 1 ||
		checkReview.DraftHash == "" ||
		diffReview.DraftHash !=
			checkReview.DraftHash {
		t.Fatalf(
			"Warning-only Check与Diff审计不符: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 1 ||
		manifest.Applications[0].Applied != 1 ||
		manifest.Applications[0].Rejected != 0 ||
		manifest.Applications[0].DraftHash !=
			diffReview.DraftHash ||
		manifest.AppliedAt == "" {
		t.Fatalf(
			"Warning-only应整批应用: %+v",
			manifest,
		)
	}

	if !strings.Contains(
		readEntriesIndex(t, root),
		"F:warning放行版",
	) {
		t.Fatal(
			"Warning-only条目未进入正式索引",
		)
	}

	for _, anchor := range []string{
		"已完成Diff审计 1 项",
		"已原子应用 1 个条目",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"Warning-only输出缺少%q: %s",
				anchor,
				output,
			)
		}
	}
}
