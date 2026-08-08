// Entries Apply生产命令包装器的结构化JSON协议测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runApplyWrapperJSON在JSON模式下执行一个固定Run的Apply包装器。
func runApplyWrapperJSON(
	t *testing.T,
	root,
	runID string,
	command *cobra.Command,
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

func TestEntriesApplyJSONSuccessReportsSideEffects(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(
		t,
	)

	if output, err := runManualAtomicCheck(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"Apply前Check应成功: %v\n%s",
			err,
			output,
		)
	}

	output, err := runApplyWrapperJSON(
		t,
		root,
		runID,
		newEntriesApplyJSONCmd(),
	)
	if err != nil {
		t.Fatalf(
			"Entries JSON Apply应成功: %v\n%s",
			err,
			output,
		)
	}

	var report entriesApplyJSONReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Entries Apply JSON不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.Operation != applyKindEntries ||
		report.Outcome != applyOutcomeApplied ||
		report.RunID != runID ||
		report.DraftHash == "" ||
		report.ReviewHash != report.DraftHash ||
		report.AssetPath != "aoci.txt" ||
		!report.AssetWritten ||
		!report.BaselineApplicable ||
		!report.BaselineAdvanced ||
		!report.AuditRecorded ||
		!report.ApplicationRecorded ||
		!report.ManifestApplied ||
		report.Attempted != 2 ||
		report.Applied != 2 ||
		report.Rejected != 0 ||
		len(report.Paths) != 2 ||
		report.Error != nil ||
		report.Recovery != "" ||
		!strings.Contains(
			report.NextCommand,
			"verify",
		) {
		t.Fatalf(
			"Entries成功报告不符: %+v",
			report,
		)
	}

	if strings.Contains(
		output,
		"生成计划核对:",
	) ||
		strings.Contains(
			output,
			"合计: 原子应用",
		) {
		t.Fatalf(
			"JSON不得混入人读Apply文本: %s",
			output,
		)
	}
}

func TestEntriesApplyJSONConflictReportsZeroWrite(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(
		t,
	)

	if output, err := runManualAtomicCheck(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"制造冲突前Check应成功: %v\n%s",
			err,
			output,
		)
	}

	indexPath := filepath.Join(
		root,
		"aoci.txt",
	)
	current, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	tampered := strings.Replace(
		string(current),
		manualAtomicOldB,
		manualAtomicOldB+"\n"+manualAtomicOldB,
		1,
	)
	if tampered == string(current) {
		t.Fatal(
			"未能制造重复旧条目冲突",
		)
	}

	if err := os.WriteFile(
		indexPath,
		[]byte(tampered),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, runErr := runApplyWrapperJSON(
		t,
		root,
		runID,
		newEntriesApplyJSONCmd(),
	)

	var exitErr *ExitError
	if !errors.As(
		runErr,
		&exitErr,
	) ||
		exitErr.Code == ExitOK ||
		exitErr.Err != nil ||
		exitErr.Msg != "" {
		t.Fatalf(
			"JSON拒绝应静默返回原退出码: %v\n%s",
			runErr,
			output,
		)
	}

	var report entriesApplyJSONReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"Entries拒绝报告不可解析: %v\n%s",
			err,
			output,
		)
	}

	if report.OK ||
		report.Outcome != applyOutcomeRejected ||
		report.AssetWritten ||
		report.BaselineAdvanced ||
		!report.AuditRecorded ||
		!report.ApplicationRecorded ||
		report.ManifestApplied ||
		report.Applied != 0 ||
		report.Rejected != 2 ||
		report.RejectKinds != "conflict" ||
		report.Error == nil ||
		report.Error.ExitCode != exitErr.Code ||
		report.NextCommand != "" ||
		!strings.Contains(
			report.Recovery,
			"零写入",
		) ||
		len(report.Diagnostics) == 0 {
		t.Fatalf(
			"Entries零写入拒绝报告不符: %+v",
			report,
		)
	}

	after, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != tampered {
		t.Fatal(
			"JSON包装器不得改变原子冲突的零写入语义",
		)
	}
}

func TestEntriesApplyJSONFinalizerKeepsSingleObject(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(
		t,
	)

	if output, err := runManualAtomicCheck(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"Apply前Check应成功: %v\n%s",
			err,
			output,
		)
	}

	indexPath := filepath.Join(
		root,
		"aoci.txt",
	)
	current, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	tampered := strings.Replace(
		string(current),
		manualAtomicOldB,
		manualAtomicOldB+"\n"+manualAtomicOldB,
		1,
	)

	if err := os.WriteFile(
		indexPath,
		[]byte(tampered),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	commandOutput, commandErr := runApplyWrapperJSON(
		t,
		root,
		runID,
		newEntriesApplyJSONCmd(),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := finishBufferedExecution(
		commandErr,
		true,
		[]byte(commandOutput),
		nil,
		&stdout,
		&stderr,
	)

	if code == ExitOK {
		t.Fatal(
			"冲突Apply不得返回成功",
		)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"完整Apply失败报告不得追加stderr: %s",
			stderr.String(),
		)
	}

	var report entriesApplyJSONReport
	if err := json.Unmarshal(
		stdout.Bytes(),
		&report,
	); err != nil {
		t.Fatalf(
			"根层finalizer后必须仍为单一JSON对象: %v\n%s",
			err,
			stdout.String(),
		)
	}

	if report.OK ||
		report.AssetWritten ||
		report.Outcome != applyOutcomeRejected {
		t.Fatalf(
			"根层后的Apply报告不符: %+v",
			report,
		)
	}
}
