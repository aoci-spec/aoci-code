// Host-Agent Curation Stage→Diff→Apply→Entries Plan完整流测试。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

func TestAgentCurationFlowPromotesIncludedEmptyFile(
	t *testing.T,
) {
	root := buildPendingCurationRepo(t)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(
		config.AutomationModeAuto,
	); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	cfg, doc, indexPath := agentPlanLoadDocument(
		t,
		root,
	)

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	target := plan.CurationTargets[0]

	result, err := stageAgentCuration(
		root,
		cfg,
		doc,
		indexPath,
		agentCurationStageRequest{
			Version: agentCurationStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Model:   "host-model",
			Decisions: []agentCurationDecision{
				{
					Path:         target.Path,
					SourceSHA256: target.SourceSHA256,
					Decision:     curation.DecisionInclude,
					Role:         "PEP 561类型信息标记",
					Reason:       "空文件本身声明发行包提供类型信息",
					Confidence:   100,
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Include != 1 ||
		result.Exclude != 0 ||
		result.ApprovalRequired ||
		result.StopBeforeApply {
		t.Fatalf(
			"auto Curation Stage结果不符: %+v",
			result,
		)
	}

	oldRepo := flagRepo
	oldJSON := flagJSON

	flagRepo = root
	flagJSON = false

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	diffCommand := newAgentCurationDiffCmd()
	diffCommand.SilenceUsage = true
	diffCommand.SilenceErrors = true
	diffCommand.SetArgs(
		[]string{
			result.RunID,
		},
	)

	var diffOutput bytes.Buffer

	diffCommand.SetOut(
		&diffOutput,
	)
	diffCommand.SetErr(
		&diffOutput,
	)

	if err := diffCommand.Execute(); err != nil {
		t.Fatalf(
			"Curation Diff失败: %v\n%s",
			err,
			diffOutput.String(),
		)
	}

	applyCommand := newAgentCurationApplyCmd()
	applyCommand.SilenceUsage = true
	applyCommand.SilenceErrors = true
	applyCommand.SetArgs(
		[]string{
			result.RunID,
		},
	)

	var applyOutput bytes.Buffer

	applyCommand.SetOut(
		&applyOutput,
	)
	applyCommand.SetErr(
		&applyOutput,
	)

	if err := applyCommand.Execute(); err != nil {
		t.Fatalf(
			"Curation Apply失败: %v\n%s",
			err,
			applyOutput.String(),
		)
	}

	document, exists, _, err := curation.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !exists ||
		len(document.Decisions) != 1 ||
		document.Decisions[0].Decision != curation.DecisionInclude {
		t.Fatalf(
			"正式策展资产不符: %+v",
			document,
		)
	}

	cfg, nextDocument, nextIndexPath := agentPlanLoadDocument(
		t,
		root,
	)

	nextPlan, err := buildAgentPlan(
		root,
		cfg,
		nextDocument,
		nextIndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if nextPlan.Stage != agentPlanStageEntriesRequired ||
		len(nextPlan.Targets) != 1 ||
		nextPlan.Targets[0].Path != "py.typed" ||
		nextPlan.Summary.IncludedMissing != 1 ||
		nextPlan.Summary.PendingCuration != 0 {
		t.Fatalf(
			"include决策后应进入Entries: %+v",
			nextPlan,
		)
	}

	indexData, err := os.ReadFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(
		indexData,
		[]byte("py.typed["),
	) {
		t.Fatal(
			"Curation Apply不得直接写正式索引条目",
		)
	}
}
