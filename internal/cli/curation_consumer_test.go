// Verify、Score与Check消费正式curation.json的端到端测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func writeCurationConsumerFile(
	t *testing.T,
	root,
	rel,
	content string,
) {
	t.Helper()

	target := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(target),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		target,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func curationConsumerDecision(
	t *testing.T,
	root,
	rel,
	decision string,
	confidence int,
) curation.Decision {
	t.Helper()

	profile, err := curation.ProfilePath(
		root,
		rel,
	)
	if err != nil {
		t.Fatal(err)
	}

	return curation.Decision{
		Path:         rel,
		Decision:     decision,
		Role:         "测试文件级语义角色",
		Reason:       "测试正式策展资产的生产消费语义",
		Confidence:   confidence,
		SourceSHA256: profile.SourceSHA256,
		Agent:        "codex",
		Model:        "test-model",
		UpdatedAt:    "2026-07-15T00:00:00Z",
	}
}

func buildCurationConsumerRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	writeCurationConsumerFile(t, root, "aoci.txt", indexText)
	writeCurationConsumerFile(t, root, "pending.marker", "")
	writeCurationConsumerFile(t, root, "include.marker", "")
	writeCurationConsumerFile(t, root, "exclude.marker", "")
	writeCurationConsumerFile(t, root, "stale.marker", "")

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	document := &curation.Document{
		Version: curation.Version,
		Decisions: []curation.Decision{
			curationConsumerDecision(
				t,
				root,
				"include.marker",
				curation.DecisionInclude,
				98,
			),
			curationConsumerDecision(
				t,
				root,
				"exclude.marker",
				curation.DecisionExclude,
				96,
			),
			curationConsumerDecision(
				t,
				root,
				"stale.marker",
				curation.DecisionExclude,
				90,
			),
		},
	}

	if err := curation.Save(root, document); err != nil {
		t.Fatal(err)
	}

	// 令决策摘要失效，同时把当前物理画像转为普通文本文件。
	writeCurationConsumerFile(
		t,
		root,
		"stale.marker",
		"changed\n",
	)

	snapshot, warnings, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf(
			"测试快照不应有警告: %v",
			warnings,
		)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestVerifyAndScoreExposeFileCurationState(
	t *testing.T,
) {
	root := buildCurationConsumerRepo(t)

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

	command := newVerifyCmd()

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	err := command.RunE(command, nil)
	var exitError *ExitError
	if !errors.As(err, &exitError) ||
		exitError.Code != ExitDrift {
		t.Fatalf(
			"存在原始Missing时verify应ExitDrift: %v",
			err,
		)
	}

	var report verifyReport
	if err := json.Unmarshal(
		output.Bytes(),
		&report,
	); err != nil {
		t.Fatalf(
			"verify JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if len(report.Result.Missing) != 4 ||
		len(report.ActionableMissing) != 2 ||
		len(report.IncludedMissing) != 1 ||
		report.IncludedMissing[0] != "include.marker" ||
		len(report.CurationExcludedMissing) != 1 ||
		report.CurationExcludedMissing[0] != "exclude.marker" ||
		len(report.SkippedMissing) != 1 ||
		report.SkippedMissing[0].Path != "pending.marker" ||
		len(report.PendingCurationMissing) != 1 ||
		report.PendingCurationMissing[0].Path != "pending.marker" ||
		len(report.StaleCurationDecisions) != 1 ||
		report.StaleCurationDecisions[0] != "stale.marker" {
		t.Fatalf(
			"verify文件级策展分型不符: %+v",
			report,
		)
	}

	if report.CurationSHA256 == "" ||
		report.CurationSHA256 == curation.HashBytes(nil) {
		t.Fatalf(
			"verify应暴露正式策展资产摘要: %q",
			report.CurationSHA256,
		)
	}

	if len(report.CurationExcludedDetails) != 1 ||
		report.CurationExcludedDetails[0].Source != "curation.json" ||
		report.CurationExcludedDetails[0].Confidence != 96 ||
		report.CurationExcludedDetails[0].Agent != "codex" {
		t.Fatalf(
			"verify应暴露排除决策来源、置信度与Agent: %+v",
			report.CurationExcludedDetails,
		)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	indexData, err := os.ReadFile(
		filepath.Join(root, "aoci.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc, _ := index.Parse(string(indexData))
	index.ResolveRelPaths(doc, root)

	score, err := indexgen.BuildScore(
		root,
		cfg,
		doc,
	)
	if err != nil {
		t.Fatal(err)
	}

	if score.CurationSHA256 != report.CurationSHA256 ||
		score.Drift.Missing != 4 ||
		score.Drift.ActionableMissing != 2 ||
		score.Drift.IncludedMissing != 1 ||
		score.Drift.CurationExcludedMissing != 1 ||
		score.Drift.SkippedMissing != 1 ||
		score.Drift.PendingCuration != 1 ||
		score.Drift.StaleCurationDecisions != 1 {
		t.Fatalf(
			"Score必须与Verify消费同一策展事实: %+v",
			score,
		)
	}
}

func buildSingleCurationCheckRepo(
	t *testing.T,
	decision string,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	writeCurationConsumerFile(t, root, "aoci.txt", indexText)
	writeCurationConsumerFile(t, root, "marker.empty", "")

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	if decision != "" {
		document := &curation.Document{
			Version: curation.Version,
			Decisions: []curation.Decision{
				curationConsumerDecision(
					t,
					root,
					"marker.empty",
					decision,
					99,
				),
			},
		}
		if err := curation.Save(root, document); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, _, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestCheckFileCurationDecisionSemantics(
	t *testing.T,
) {
	cases := []struct {
		name        string
		decision    string
		wantExit    bool
		wantAnchors []string
	}{
		{
			name:     "pending blocks",
			decision: "",
			wantExit: true,
			wantAnchors: []string{
				"待策展Missing 1 项",
				"其中Pending 1",
			},
		},
		{
			name:     "exclude allows commit",
			decision: curation.DecisionExclude,
			wantExit: false,
			wantAnchors: []string{
				"✓ 可提交",
				"策展排除 1",
			},
		},
		{
			name:     "include enters entries debt",
			decision: curation.DecisionInclude,
			wantExit: true,
			wantAnchors: []string{
				"ActionableMissing 1",
				"其中Included 1",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := buildSingleCurationCheckRepo(
				t,
				testCase.decision,
			)

			output, err := runCheck(t, root)

			if testCase.wantExit {
				var exitError *ExitError
				if !errors.As(err, &exitError) ||
					exitError.Code != ExitDrift {
					t.Fatalf(
						"应ExitDrift: %v\n%s",
						err,
						output,
					)
				}
			} else if err != nil {
				t.Fatalf(
					"有效exclude应允许提交: %v\n%s",
					err,
					output,
				)
			}

			for _, anchor := range testCase.wantAnchors {
				if !strings.Contains(output, anchor) {
					t.Fatalf(
						"输出缺锚点%q:\n%s",
						anchor,
						output,
					)
				}
			}
		})
	}
}

func TestCheckPendingCurationDraftBlocks(
	t *testing.T,
) {
	root := buildSingleCurationCheckRepo(
		t,
		curation.DecisionExclude,
	)

	runID, err := draft.NewRun(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := draft.SaveManifest(
		root,
		&draft.Manifest{
			RunID: runID,
			Kind:  draft.KindCuration,
		},
	); err != nil {
		t.Fatal(err)
	}

	output, err := runCheck(t, root)

	var exitError *ExitError
	if !errors.As(err, &exitError) ||
		exitError.Code != ExitDrift {
		t.Fatalf(
			"Curation草稿悬置应阻断check: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"Curation草稿批次悬置 run "+runID,
	) {
		t.Fatalf(
			"check应显示Curation run_id:\n%s",
			output,
		)
	}
}
