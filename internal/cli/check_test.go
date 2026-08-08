// aoci check命令测试: 退出码、issues组装、策展排除措辞与草稿悬置。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

// findRegisteredCommand从subCommands按名取得命令。
func findRegisteredCommand(
	t *testing.T,
	name string,
) *cobra.Command {
	t.Helper()

	for _, command := range subCommands {
		if command.Name() == name {
			return command
		}
	}

	t.Fatalf("命令%s未注册", name)
	return nil
}

// runCheck使用flagRepo指定测试仓库并执行aoci check。
func runCheck(
	t *testing.T,
	root string,
) (string, error) {
	t.Helper()

	old := flagRepo
	flagRepo = root

	t.Cleanup(func() {
		flagRepo = old
	})

	command := findRegisteredCommand(
		t,
		"check",
	)
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{})

	err := command.Execute()
	return output.String(), err
}

// TestCheckCleanRepo验证全净仓返回可提交。
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

// TestCheckDriftRepo验证Stale与ActionableMissing按行动分型。
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
			"aoci index update",
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

// TestCheckCurationExcludedUsesGovernanceWording锁定已完成排除决策的术语。
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

// TestCheckDraftPendingThenApplied验证草稿悬置只由applied_at决定。
func TestCheckDraftPendingThenApplied(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	runID, err := draft.NewRun(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := draft.SaveManifest(
		root,
		&draft.Manifest{
			RunID: runID,
			Kind:  draft.KindEntries,
		},
	); err != nil {
		t.Fatal(err)
	}

	output, pendingErr := runCheck(
		t,
		root,
	)

	exitErr, ok := pendingErr.(*ExitError)
	if !ok ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"未打标应ExitDrift: err=%v\n%s",
			pendingErr,
			output,
		)
	}

	if !strings.Contains(
		output,
		"草稿批次悬置 run "+runID,
	) ||
		!strings.Contains(
			output,
			"未经全量干净应用",
		) {
		t.Fatalf(
			"issues应含run_id与判定文案: %s",
			output,
		)
	}

	assertCheckLedger(t, root, 1)

	if err := draft.MarkApplied(
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"MarkApplied失败: %v",
			err,
		)
	}

	output, appliedErr := runCheck(
		t,
		root,
	)
	if appliedErr != nil {
		t.Fatalf(
			"打标后check应成功: %v\n%s",
			appliedErr,
			output,
		)
	}

	if !strings.Contains(
		output,
		"✓ 可提交",
	) {
		t.Fatalf(
			"打标后应报可提交: %s",
			output,
		)
	}

	if strings.Contains(
		output,
		runID,
	) {
		t.Fatalf(
			"打标后不得再报该批次: %s",
			output,
		)
	}
}

// assertCheckLedger断言最近check事件的WarningsCount。
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
