// Header与Curation Apply结构化JSON及结果分类测试。
package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

func TestHeaderApplyJSONReportsBackupBaselineAndAudit(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#JSON Header Apply\n",
	)

	if output, err := runHeaderDiffForP23(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"Header Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	output, err := runApplyWrapperJSON(
		t,
		root,
		runID,
		newHeaderApplyJSONCmd(),
	)
	if err != nil {
		t.Fatalf(
			"Header JSON Apply应成功: %v\n%s",
			err,
			output,
		)
	}

	var report headerApplyJSONReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Header Apply JSON不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.Operation != applyKindHeader ||
		report.Outcome != applyOutcomeApplied ||
		report.AssetPath != "aoci.txt" ||
		!report.AssetWritten ||
		!report.BaselineAdvanced ||
		!report.AuditRecorded ||
		report.ApplicationRecorded ||
		!report.ManifestApplied ||
		!report.BackupCreated ||
		report.Attempted != 1 ||
		report.Applied != 1 ||
		report.Rejected != 0 ||
		report.Error != nil ||
		!strings.Contains(
			report.NextCommand,
			"verify",
		) {
		t.Fatalf(
			"Header成功报告不符: %+v",
			report,
		)
	}
}

func TestCurationApplyJSONReportsDecisionCounts(
	t *testing.T,
) {
	root := buildPendingCurationRepo(
		t,
	)

	baseConfig, err := config.LoadBase(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := baseConfig.SetAutomationMode(
		config.AutomationModeAuto,
	); err != nil {
		t.Fatal(err)
	}

	if err := config.Save(
		root,
		baseConfig,
	); err != nil {
		t.Fatal(err)
	}

	cfg, document, indexPath := agentPlanLoadDocument(
		t,
		root,
	)

	plan, err := buildAgentPlan(
		root,
		cfg,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	target := plan.CurationTargets[0]

	stageResult, err := stageAgentCuration(
		root,
		cfg,
		document,
		indexPath,
		agentCurationStageRequest{
			Version: agentCurationStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Model:   "test-model",
			Decisions: []agentCurationDecision{
				{
					Path:         target.Path,
					SourceSHA256: target.SourceSHA256,
					Decision:     curation.DecisionInclude,
					Role:         "文件级协议标记",
					Reason:       "文件存在本身具有独立语义",
					Confidence:   99,
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if output, err := runCurationDiffJSON(
		t,
		root,
		stageResult.RunID,
	); err != nil {
		t.Fatalf(
			"Curation Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	output, err := runApplyWrapperJSON(
		t,
		root,
		stageResult.RunID,
		newAgentCurationApplyJSONCmd(),
	)
	if err != nil {
		t.Fatalf(
			"Curation JSON Apply应成功: %v\n%s",
			err,
			output,
		)
	}

	var report curationApplyJSONReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Curation Apply JSON不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.Operation != applyKindCuration ||
		report.Outcome != applyOutcomeApplied ||
		report.AssetPath != ".aoci/curation.json" ||
		!report.AssetWritten ||
		report.BaselineApplicable ||
		report.BaselineAdvanced ||
		!report.AuditRecorded ||
		!report.ApplicationRecorded ||
		!report.ManifestApplied ||
		report.Attempted != 1 ||
		report.Applied != 1 ||
		report.Rejected != 0 ||
		report.Include != 1 ||
		report.Exclude != 0 ||
		len(report.Paths) != 1 ||
		report.Paths[0] != target.Path ||
		report.Error != nil ||
		!strings.Contains(
			report.NextCommand,
			"index agent guide",
		) {
		t.Fatalf(
			"Curation成功报告不符: %+v",
			report,
		)
	}
}

func TestClassifyApplyOutcomeDistinguishesPostWriteFailure(
	t *testing.T,
) {
	if got := classifyApplyOutcome(
		errors.New("application audit failed"),
		true,
		nil,
	); got != applyOutcomeAssetWrittenAuditFailed {
		t.Fatalf(
			"写后失败分类不符: %s",
			got,
		)
	}

	if got := classifyApplyOutcome(
		errors.New("pre-write rejected"),
		false,
		nil,
	); got != applyOutcomeRejected {
		t.Fatalf(
			"零写入拒绝分类不符: %s",
			got,
		)
	}

	if got := classifyApplyOutcome(
		nil,
		true,
		[]string{
			"baseline not advanced",
		},
	); got != applyOutcomeAppliedWithWarnings {
		t.Fatalf(
			"带警告成功分类不符: %s",
			got,
		)
	}
}
