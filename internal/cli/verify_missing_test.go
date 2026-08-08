// verify对Missing三分、规范Raw Missing字段与Pending治理阻断的生产路径测试。
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
)

func buildVerifyMissingThreeWayRepo(
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
		"===配置索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(
		filepath.Join(
			root,
			"docs",
		),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"docs",
			"x.md",
		),
		[]byte("# doc\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(
		filepath.Join(
			root,
			"src",
		),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"src",
			"main.go",
		),
		[]byte("package main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"empty.txt",
		),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.CurationExclude = []string{"docs"}
	cfg.LedgerEnabled = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

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

func TestVerifyCommandJSONExposesMissingThreeWay(
	t *testing.T,
) {
	root := buildVerifyMissingThreeWayRepo(t)

	withVerifyFlags(
		t,
		root,
		true,
	)

	command := newVerifyCmd()
	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)

	err := command.RunE(
		command,
		nil,
	)

	var exitErr *ExitError

	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"Actionable与Pending存在时应ExitDrift: %v",
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

	if len(report.Result.Missing) != 3 ||
		len(report.RawMissing) != 3 ||
		len(report.ActionableMissing) != 1 ||
		report.ActionableMissing[0] != "src/main.go" ||
		len(report.CurationExcludedMissing) != 1 ||
		report.CurationExcludedMissing[0] != "docs/x.md" ||
		len(report.SkippedMissing) != 1 ||
		report.SkippedMissing[0].Path != "empty.txt" ||
		report.SkippedMissing[0].Reason != "empty" ||
		len(report.PendingCurationMissing) != 1 ||
		report.PendingCurationMissing[0].Path != "empty.txt" {
		t.Fatalf(
			"verify Missing三分、Raw Missing规范字段或Pending子集不符: %+v",
			report,
		)
	}

	if strings.Join(
		report.RawMissing,
		"\n",
	) != strings.Join(
		report.Result.Missing,
		"\n",
	) {
		t.Fatalf(
			"raw_missing必须与历史result.missing逐项完全一致: raw=%v result=%v",
			report.RawMissing,
			report.Result.Missing,
		)
	}

	if verifyRawDriftCount(
		&report,
	) != 3 ||
		verifyUnresolvedDriftCount(
			&report,
		) != 2 {
		t.Fatalf(
			"事实层应为3，未解决治理层应为2: raw=%d unresolved=%d",
			verifyRawDriftCount(&report),
			verifyUnresolvedDriftCount(&report),
		)
	}
}

func TestVerifyOnlyPendingStillBlocks(
	t *testing.T,
) {
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
		"===配置索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"empty.txt",
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

	withVerifyFlags(
		t,
		root,
		false,
	)

	command := newVerifyCmd()
	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)

	err = command.RunE(
		command,
		nil,
	)

	var exitErr *ExitError

	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"只有PendingCuration时仍应ExitDrift: %v",
			err,
		)
	}

	text := output.String()

	for _, anchor := range []string{
		"SkippedMissing 确定性跳过(1)",
		"PendingCurationMissing 待语义裁决(1)",
		"未解决治理漂移共 1 项(exit 1)",
		"不能把Pending当作治理完成",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"verify缺少Pending治理锚点%q:\n%s",
				anchor,
				text,
			)
		}
	}
}
