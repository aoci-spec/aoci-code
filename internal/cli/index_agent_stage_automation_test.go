// Host-Agent Stage 对 automation.mode 的生产路径测试。
package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestAgentEntriesStageOffRejectsBeforeRun(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	if err := cfg.SetAutomationMode(
		config.AutomationModeOff,
	); err != nil {
		t.Fatal(err)
	}

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	_, err = stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:x | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"automation.mode=off",
		) {
		t.Fatalf(
			"off必须拒绝Entries Stage: %v",
			err,
		)
	}

	runIDs, listErr := draft.ListRunIDs(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(runIDs) != 0 {
		t.Fatalf(
			"off拒绝发生在创建Run前: %+v",
			runIDs,
		)
	}
}

func TestAgentHeaderStageOffRejectsBeforeRun(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	if err := cfg.SetAutomationMode(
		config.AutomationModeOff,
	); err != nil {
		t.Fatal(err)
	}

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = stageAgentHeader(
		root,
		cfg,
		doc,
		indexPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  validAgentHeaderCandidate(),
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"automation.mode=off",
		) {
		t.Fatalf(
			"off必须拒绝Header Stage: %v",
			err,
		)
	}

	runIDs, listErr := draft.ListRunIDs(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(runIDs) != 0 {
		t.Fatalf(
			"off拒绝发生在创建Run前: %+v",
			runIDs,
		)
	}
}

func TestAgentEntriesStageAutoReturnsNoApprovalStop(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	if err := cfg.SetAutomationMode(
		config.AutomationModeAuto,
	); err != nil {
		t.Fatal(err)
	}

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	result, err := stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:x | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.AutomationMode !=
		config.AutomationModeAuto ||
		result.ApprovalRequired ||
		result.StopBeforeApply {
		t.Fatalf(
			"auto Entries Stage停点不符: %+v",
			result,
		)
	}
}

func TestAgentHeaderStageAutoReturnsNoApprovalStop(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	if err := cfg.SetAutomationMode(
		config.AutomationModeAuto,
	); err != nil {
		t.Fatal(err)
	}

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := stageAgentHeader(
		root,
		cfg,
		doc,
		indexPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  validAgentHeaderCandidate(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.AutomationMode !=
		config.AutomationModeAuto ||
		result.ApprovalRequired ||
		result.StopBeforeApply {
		t.Fatalf(
			"auto Header Stage停点不符: %+v",
			result,
		)
	}
}
