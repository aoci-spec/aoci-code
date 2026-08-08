// Host-Agent Plan/Guide对PendingCuration终态测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

func buildPendingCurationRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	agentPlanWriteFile(
		t,
		root,
		"keep.go",
		"package main\n",
	)
	agentPlanWriteFile(
		t,
		root,
		"py.typed",
		"",
	)

	indexText := agentPlanHeader(true) +
		"\n===代码索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"keep.go[XAP7T]: F:对齐文件 | R:- | A:- | S:-\n"

	agentPlanWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	snapshot := map[string]baseline.Fingerprint{}
	for _, rel := range []string{
		"aoci.txt",
		"keep.go",
		"py.typed",
	} {
		fingerprint, err :=
			baseline.HashFile(
				filepath.Join(
					root,
					rel,
				),
			)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[rel] = fingerprint
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(
			snapshot,
		),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestAgentPlanAndGuideRequireCurationForEmptyFile(
	t *testing.T,
) {
	root := buildPendingCurationRepo(t)

	cfg, doc, indexPath :=
		agentPlanLoadDocument(
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

	if plan.Stage !=
		agentPlanStageCurationRequired ||
		plan.NextAction !=
			agentPlanActionStageCuration ||
		plan.Summary.Missing != 1 ||
		plan.Summary.ActionableNew != 0 ||
		plan.Summary.SkippedMissing != 1 ||
		plan.Summary.PendingCuration != 1 ||
		len(plan.CurationTargets) != 1 ||
		plan.CurationTargets[0].Path !=
			"py.typed" {
		t.Fatalf(
			"空语义文件应进入Curation Plan: %+v",
			plan,
		)
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if guide.Complete ||
		guide.Mode !=
			agentGuideModePrepareReview ||
		guide.CurationStageRequest == nil ||
		guide.CurationBatch == nil ||
		guide.EntriesStageRequest != nil {
		t.Fatalf(
			"Curation Guide不符: %+v",
			guide,
		)
	}

	if !strings.Contains(
		guide.Commands.CurationStage,
		"--request-file",
	) {
		t.Fatalf(
			"Curation Guide必须发放request-file命令: %+v",
			guide.Commands,
		)
	}

	if _, err := os.Stat(
		filepath.Join(
			root,
			".aoci",
			"ledger.jsonl",
		),
	); !os.IsNotExist(err) {
		t.Fatalf(
			"Plan/Guide必须零Ledger: %v",
			err,
		)
	}
}
