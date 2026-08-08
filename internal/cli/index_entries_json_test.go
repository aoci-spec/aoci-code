// Entries Check与Diff结构化JSON协议测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func runEntriesCommandJSON(
	t *testing.T,
	root string,
	command *cobraCommandAdapter,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = root
	flagJSON = true

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	var output bytes.Buffer
	command.SetOut(
		&output,
	)
	command.SetErr(
		&output,
	)

	err := command.Run(
		[]string{
			runID,
		},
	)
	return output.String(), err
}

// cobraCommandAdapter只暴露专项测试需要的命令能力。
type cobraCommandAdapter struct {
	setOut func(*bytes.Buffer)
	setErr func(*bytes.Buffer)
	run    func([]string) error
}

func (adapter *cobraCommandAdapter) SetOut(
	output *bytes.Buffer,
) {
	adapter.setOut(
		output,
	)
}

func (adapter *cobraCommandAdapter) SetErr(
	output *bytes.Buffer,
) {
	adapter.setErr(
		output,
	)
}

func (adapter *cobraCommandAdapter) Run(
	args []string,
) error {
	return adapter.run(
		args,
	)
}

func entriesCheckJSONAdapter() *cobraCommandAdapter {
	command := newEntriesCheckCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	return &cobraCommandAdapter{
		setOut: func(output *bytes.Buffer) {
			command.SetOut(
				output,
			)
		},
		setErr: func(output *bytes.Buffer) {
			command.SetErr(
				output,
			)
		},
		run: func(args []string) error {
			return command.RunE(
				command,
				args,
			)
		},
	}
}

func entriesDiffJSONAdapter() *cobraCommandAdapter {
	command := newEntriesDiffCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	return &cobraCommandAdapter{
		setOut: func(output *bytes.Buffer) {
			command.SetOut(
				output,
			)
		},
		setErr: func(output *bytes.Buffer) {
			command.SetErr(
				output,
			)
		},
		run: func(args []string) error {
			return command.RunE(
				command,
				args,
			)
		},
	}
}

func TestEntriesCheckJSONSuccessIsSingleObject(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:合规新条目 | R:- | A:- | S:-",
		},
	)

	output, err := runEntriesCommandJSON(
		t,
		root,
		entriesCheckJSONAdapter(),
		runID,
	)
	if err != nil {
		t.Fatalf(
			"JSON Check应成功: %v\n%s",
			err,
			output,
		)
	}

	var report entriesCheckReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"JSON Check不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.RunID != runID ||
		report.DraftHash == "" ||
		report.Total != 1 ||
		report.Passed != 1 ||
		report.Rejected != 0 ||
		len(report.Items) != 1 ||
		report.Items[0].Outcome != "passed" ||
		!strings.Contains(
			report.NextCommand,
			"entries diff "+runID,
		) ||
		strings.Contains(
			report.NextCommand,
			"entries apply",
		) ||
		report.Recovery != "" {
		t.Fatalf(
			"JSON Check报告不符: %+v",
			report,
		)
	}

	if strings.Contains(
		output,
		"条目草稿预检",
	) {
		t.Fatalf(
			"JSON stdout不得混入人读文本: %s",
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
		manifest.Reviews[0].DraftHash != report.DraftHash {
		t.Fatalf(
			"JSON Check必须追加同一P-23摘要: %+v",
			manifest.Reviews,
		)
	}
}

func TestEntriesCheckJSONRejectReturnsRecoveryAndExitInvalid(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[ZQQ5T]: F:臆造标签 | R:- | A:- | S:-",
		},
	)

	output, err := runEntriesCommandJSON(
		t,
		root,
		entriesCheckJSONAdapter(),
		runID,
	)

	var exitErr *ExitError
	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != ExitInvalid ||
		exitErr.Err != nil ||
		exitErr.Msg != "" {
		t.Fatalf(
			"JSON拒绝应以静默ExitInvalid结束: %v\n%s",
			err,
			output,
		)
	}

	var report entriesCheckReport
	if decodeErr := json.Unmarshal(
		[]byte(output),
		&report,
	); decodeErr != nil {
		t.Fatalf(
			"拒绝报告不可解析: %v\n%s",
			decodeErr,
			output,
		)
	}

	if report.OK ||
		report.Rejected != 1 ||
		len(report.Items) != 1 ||
		len(report.Items[0].Errors) != 1 ||
		report.Items[0].Errors[0].Code != "dict" ||
		report.NextCommand != "" ||
		!strings.Contains(
			report.Recovery,
			"修正草稿文件",
		) ||
		!strings.Contains(
			report.Recovery,
			"entries check "+runID,
		) {
		t.Fatalf(
			"拒绝JSON报告或恢复动作不符: %+v",
			report,
		)
	}
}

// 本测试组合真实Check命令结果和根层finalizer，但不直接调用executeCLI。
//
// executeCLI模拟独立进程并会重置包级Cobra参数，不适合嵌入共享全局变量的
// 普通包测试。B1已分别锁定executeCLI；本测试只验证B2业务JSON交给根层后
// 不会被追加第二个错误信封。
func TestEntriesCheckJSONRejectFinalizerKeepsSingleObject(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[ZQQ5T]: F:臆造标签 | R:- | A:- | S:-",
		},
	)

	commandOutput, commandErr := runEntriesCommandJSON(
		t,
		root,
		entriesCheckJSONAdapter(),
		runID,
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

	if code != ExitInvalid {
		t.Fatalf(
			"根层拒绝退出码应为%d，得到%d\nstdout=%s\nstderr=%s",
			ExitInvalid,
			code,
			stdout.String(),
			stderr.String(),
		)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"完整业务JSON拒绝不应追加stderr: %s",
			stderr.String(),
		)
	}

	var report entriesCheckReport
	if err := json.Unmarshal(
		stdout.Bytes(),
		&report,
	); err != nil {
		t.Fatalf(
			"根层finalizer后必须仍只有一个JSON对象: %v\n%s",
			err,
			stdout.String(),
		)
	}

	if report.OK ||
		report.NextCommand != "" ||
		report.Recovery == "" {
		t.Fatalf(
			"根层finalizer后的拒绝报告不符: %+v",
			report,
		)
	}
}

func TestEntriesDiffJSONPreservesCompleteEntryLines(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"f.go": "f.go[XCR5T]: F:更新职责 | R:g.go | A:Run | S:完整约束",
		},
	)

	output, err := runEntriesCommandJSON(
		t,
		root,
		entriesDiffJSONAdapter(),
		runID,
	)
	if err != nil {
		t.Fatalf(
			"JSON Diff应成功: %v\n%s",
			err,
			output,
		)
	}

	var report entriesDiffReport
	if err := json.Unmarshal(
		[]byte(output),
		&report,
	); err != nil {
		t.Fatalf(
			"JSON Diff不可解析: %v\n%s",
			err,
			output,
		)
	}

	if !report.OK ||
		report.RunID != runID ||
		report.DraftHash == "" ||
		report.Total != 1 ||
		report.Reviewed != 1 ||
		report.Skipped != 0 ||
		len(report.Items) != 1 {
		t.Fatalf(
			"JSON Diff摘要不符: %+v",
			report,
		)
	}

	item := report.Items[0]
	if item.Change != "update" ||
		!item.Reviewed ||
		!strings.Contains(
			item.OldEntry,
			"F:x",
		) ||
		!strings.Contains(
			item.NewEntry,
			"F:更新职责",
		) ||
		!strings.Contains(
			item.NewEntry,
			"S:完整约束",
		) {
		t.Fatalf(
			"Diff必须保留完整FRAS单行: %+v",
			item,
		)
	}

	if !strings.Contains(
		report.NextCommand,
		"entries apply "+runID,
	) {
		t.Fatalf(
			"Diff成功后才可以指向Apply: %+v",
			report,
		)
	}

	if strings.Contains(
		output,
		"条目草稿对照",
	) {
		t.Fatalf(
			"JSON Diff不得混入人读文本: %s",
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
			"JSON Diff必须追加同一P-23摘要: %+v",
			manifest.Reviews,
		)
	}
}
