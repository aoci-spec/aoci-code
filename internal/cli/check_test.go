// aoci check command tests: exit codes, issues assembly, curation exclusion wording, and draft suspension.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// runCheck runs `aoci check` against root and returns both streams with the
// exit code as an error, the shape the assertions below were written for.
func runCheck(
	t *testing.T,
	root string,
) (string, error) {
	t.Helper()

	stdout, stderr, code := runCLI(t, root, "check")
	return stdout + stderr, exitError(stderr, code)
}

// TestCheckCleanRepo verifies that a fully clean repository returns committable.
func TestCheckCleanRepo(t *testing.T) {
	root := buildUpdateRepo(t)

	output, err := runCheck(t, root)
	if err != nil {
		t.Fatalf(
			"全净仓check应成功: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"✓ 可提交",
	) {
		t.Fatalf(
			"应结论先行报可提交: %s",
			output,
		)
	}

	if !strings.Contains(
		output,
		"RawMissing 0",
	) {
		t.Fatalf(
			"全净仓应明确报告RawMissing为0: %s",
			output,
		)
	}

	assertCheckLedger(t, root, 0)
}

// TestCheckDriftRepo verifies that Stale and ActionableMissing are typed by action.
func TestCheckDriftRepo(t *testing.T) {
	root := buildUpdateRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "f.go"),
		[]byte("package f\n// 改\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte("package n\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runCheck(t, root)

	exitErr, ok := err.(*ExitError)
	if !ok ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"漂移仓应ExitDrift: err=%v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"ActionableMissing 1,Stale 1",
	) ||
		!strings.Contains(
			output,
			"aoci cognition onboard start",
		) {
		t.Fatalf(
			"issues应按行动分型并给出建议: %s",
			output,
		)
	}

	if !strings.Contains(
		output,
		"RawMissing 1",
	) {
		t.Fatalf(
			"明细必须保留原始Missing事实: %s",
			output,
		)
	}

	if strings.Contains(
		output,
		"Stale 2",
	) ||
		strings.Contains(
			output,
			"漂移 3 项",
		) {
		t.Fatalf(
			"文案不得跨维度双计新文件: %s",
			output,
		)
	}

	assertCheckLedger(t, root, 1)
}

// TestCheckCurationExcludedUsesGovernanceWording locks the terminology of a completed exclusion decision.
func TestCheckCurationExcludedUsesGovernanceWording(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	docsDirectory := filepath.Join(
		root,
		"docs",
	)
	if err := os.MkdirAll(
		docsDirectory,
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			docsDirectory,
			"x.md",
		),
		[]byte("# doc\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	cfg.CurationExclude = []string{
		"docs",
	}

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

	output, checkErr := runCheck(
		t,
		root,
	)
	if checkErr != nil {
		t.Fatalf(
			"只有已解释排除项时check应成功: %v\n%s",
			checkErr,
			output,
		)
	}

	for _, anchor := range []string{
		"CurationExcludedMissing 1",
		"已经完成的排除治理决策",
		"不阻断提交",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"check输出缺少措辞%q:\n%s",
				anchor,
				output,
			)
		}
	}

	if strings.Contains(
		output,
		"负空间",
	) {
		t.Fatalf(
			"check输出不得继续使用负空间术语:\n%s",
			output,
		)
	}

	assertCheckLedger(t, root, 0)
}

// assertCheckLedger asserts the WarningsCount of the latest check event.
func assertCheckLedger(
	t *testing.T,
	root string,
	wantIssues int,
) {
	t.Helper()

	events, _ := ledger.Recent(
		root,
		10,
	)

	for _, event := range events {
		if event.Op != "check" {
			continue
		}

		if event.WarningsCount != wantIssues {
			t.Fatalf(
				"check事件WarningsCount应为%d: %+v",
				wantIssues,
				event,
			)
		}

		return
	}

	t.Fatalf(
		"ledger未见check事件: %+v",
		events,
	)
}
