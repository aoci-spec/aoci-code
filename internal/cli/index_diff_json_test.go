// Header与Curation Diff结构化JSON协议测试。
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func runHeaderDiffJSON(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	oldJSON := flagJSON
	oldQuiet := flagQuiet

	flagRepo = root
	flagJSON = true
	flagQuiet = false

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
		flagQuiet = oldQuiet
	})

	command := newHeaderDiffCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(
		&output,
	)
	command.SetErr(
		&output,
	)

	err := command.RunE(
		command,
		[]string{
			runID,
		},
	)
	return output.String(), err
}

func runCurationDiffJSON(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	oldJSON := flagJSON
	oldQuiet := flagQuiet

	flagRepo = root
	flagJSON = true
	flagQuiet = false

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
		flagQuiet = oldQuiet
	})

	command := newAgentCurationDiffCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(
		&output,
	)
	command.SetErr(
		&output,
	)

	err := command.RunE(
		command,
		[]string{
			runID,
		},
	)
	return output.String(), err
}

func TestHeaderDiffJSONContainsFullHeadersAndReview(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#结构化JSON新头部\n#第二行\n",
	)

	output, err := runHeaderDiffJSON(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"Header JSON Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	var report headerDiffReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Header JSON报告不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.RunID != runID ||
		report.DraftHash == "" ||
		report.CurrentHeader == "" ||
		!strings.Contains(
			report.DraftHeader,
			"结构化JSON新头部",
		) ||
		report.DiffText == "" ||
		!report.ManifestPresent ||
		!report.ReviewRecorded ||
		report.LegacyCompatibility ||
		len(report.Warnings) != 0 ||
		!strings.Contains(
			report.NextCommand,
			"header apply "+runID,
		) {
		t.Fatalf(
			"Header JSON报告不符: %+v",
			report,
		)
	}

	if strings.Contains(
		output,
		"头部对照:",
	) {
		t.Fatalf(
			"Header JSON不得混入人读文本: %s",
			output,
		)
	}

	manifest, loadErr := draft.LoadManifest(
		root,
		runID,
	)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Action != draft.ReviewActionDiff ||
		manifest.Reviews[0].DraftHash != report.DraftHash {
		t.Fatalf(
			"Header JSON必须追加同一P-23摘要: %+v",
			manifest.Reviews,
		)
	}
}

func TestHeaderDiffJSONFailureWritesNoPartialReport(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#不会形成成功JSON\n",
	)

	runDirectory, err := draft.RunDir(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			runDirectory,
			draft.ManifestFileName,
		),
		[]byte("{not-json"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runHeaderDiffJSON(
		t,
		root,
		runID,
	)
	if err == nil {
		t.Fatal(
			"损坏Manifest必须使Header Diff失败",
		)
	}
	if output != "" {
		t.Fatalf(
			"Header审计前失败不得输出部分成功报告: %q",
			output,
		)
	}
}

func TestCurationDiffJSONCreateReport(
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

	output, err := runCurationDiffJSON(
		t,
		root,
		stageResult.RunID,
	)
	if err != nil {
		t.Fatalf(
			"Curation JSON Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	var report curationDiffReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Curation JSON报告不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.RunID != stageResult.RunID ||
		report.DraftHash == "" ||
		report.Total != 1 ||
		report.Include != 1 ||
		report.Exclude != 0 ||
		report.CurrentExists ||
		report.CurrentSHA256 != curation.HashBytes(nil) ||
		!report.ReviewRecorded ||
		len(report.Items) != 1 ||
		report.Items[0].Change != "create" ||
		report.Items[0].OldExists ||
		report.Items[0].OldDecision != nil ||
		report.Items[0].NewDecision.Decision !=
			curation.DecisionInclude ||
		!strings.Contains(
			report.NextCommand,
			"curation apply "+stageResult.RunID,
		) {
		t.Fatalf(
			"Curation create报告不符: %+v",
			report,
		)
	}

	if strings.Contains(
		output,
		"Curation草稿对照:",
	) {
		t.Fatalf(
			"Curation JSON不得混入人读文本: %s",
			output,
		)
	}

	manifest, loadErr := draft.LoadManifest(
		root,
		stageResult.RunID,
	)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].DraftHash != report.DraftHash {
		t.Fatalf(
			"Curation JSON必须追加同一P-23摘要: %+v",
			manifest.Reviews,
		)
	}
}

func buildCurationDiffUpdateRepo(
	t *testing.T,
) (string, string) {
	t.Helper()

	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(
			root,
			"marker.empty",
		),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false
	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	profile, err := curation.ProfilePath(
		root,
		"marker.empty",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := curation.Save(
		root,
		&curation.Document{
			Version: curation.Version,
			Decisions: []curation.Decision{
				{
					Path:         "marker.empty",
					Decision:     curation.DecisionInclude,
					Role:         "旧协议角色",
					Reason:       "旧决策认为应形成文件级条目",
					Confidence:   90,
					SourceSHA256: profile.SourceSHA256,
					Agent:        "codex",
					Model:        "old-model",
					UpdatedAt:    "2026-07-16T00:00:00Z",
				},
			},
		},
	); err != nil {
		t.Fatal(err)
	}

	runID, err := draft.NewRun(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	draftDocument := &curation.Document{
		Version: curation.Version,
		Decisions: []curation.Decision{
			{
				Path:         "marker.empty",
				Decision:     curation.DecisionExclude,
				Role:         "新协议角色",
				Reason:       "新决策认为语义可由目录与相邻模块承载",
				Confidence:   98,
				SourceSHA256: profile.SourceSHA256,
			},
		},
	}

	data, err := json.MarshalIndent(
		draftDocument,
		"",
		"  ",
	)
	if err != nil {
		t.Fatal(err)
	}
	data = append(
		data,
		'\n',
	)

	if err := draft.WriteFile(
		root,
		runID,
		draft.CurationFileName,
		data,
	); err != nil {
		t.Fatal(err)
	}

	if err := draft.SaveManifest(
		root,
		&draft.Manifest{
			RunID: runID,
			Kind:  draft.KindCuration,
			Entries: []draft.EntryStatus{
				{
					Path:         "marker.empty",
					Status:       "drafted",
					SourceSHA256: profile.SourceSHA256,
				},
			},
			Files: []string{
				draft.CurationFileName,
			},
		},
	); err != nil {
		t.Fatal(err)
	}

	return root, runID
}

func TestCurationDiffJSONUpdateIncludesOldAndNew(
	t *testing.T,
) {
	root, runID := buildCurationDiffUpdateRepo(
		t,
	)

	output, err := runCurationDiffJSON(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"Curation update JSON Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	var report curationDiffReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Curation update报告不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.CurrentExists ||
		report.CurrentSHA256 == "" ||
		report.CurrentSHA256 == curation.HashBytes(nil) ||
		report.Total != 1 ||
		report.Include != 0 ||
		report.Exclude != 1 ||
		len(report.Items) != 1 {
		t.Fatalf(
			"Curation update摘要不符: %+v",
			report,
		)
	}

	item := report.Items[0]
	if item.Change != "update" ||
		!item.OldExists ||
		item.OldDecision == nil ||
		item.OldDecision.Decision != curation.DecisionInclude ||
		item.OldDecision.Agent != "codex" ||
		item.NewDecision.Decision != curation.DecisionExclude ||
		item.NewDecision.Role != "新协议角色" ||
		item.NewDecision.SourceSHA256 == "" {
		t.Fatalf(
			"Curation旧新决策不符: %+v",
			item,
		)
	}
}
