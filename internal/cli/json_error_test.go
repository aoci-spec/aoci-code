// 根层JSON失败信封、领域机器码透传、已有业务JSON防重复和MCP旁路测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFinishBufferedExecutionWritesJSONErrorEnvelope(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := &ExitError{
		Code: ExitConfig,
		Err:  errors.New("配置缺失"),
	}

	code := finishBufferedExecution(
		err,
		true,
		nil,
		nil,
		&stdout,
		&stderr,
	)
	if code != ExitConfig {
		t.Fatalf(
			"退出码应为%d，得到%d",
			ExitConfig,
			code,
		)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"JSON失败不应重复写人读stderr: %q",
			stderr.String(),
		)
	}

	var envelope cliJSONErrorEnvelope
	if decodeErr := json.Unmarshal(
		stdout.Bytes(),
		&envelope,
	); decodeErr != nil {
		t.Fatalf(
			"错误信封不可解析: %v\n%s",
			decodeErr,
			stdout.String(),
		)
	}

	if envelope.Version != cliJSONErrorVersion ||
		envelope.OK ||
		envelope.ExitCode != ExitConfig ||
		envelope.ErrorCode != "config" ||
		envelope.Message != "配置缺失" {
		t.Fatalf(
			"错误信封内容不符: %+v",
			envelope,
		)
	}
}

// TestFinishBufferedExecutionPreservesMachineErrorCode锁定领域码与退出码分层。
//
// write_conflict仍使用ExitConfig=3作为进程退出码，但JSON机器分类必须保留
// 精确领域码，不能仅依据数字3降级成config。
func TestFinishBufferedExecutionPreservesMachineErrorCode(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := &ExitError{
		Code:        ExitConfig,
		MachineCode: "write_conflict",
		Err: errors.New(
			"原子批量索引写锁获取超时",
		),
	}

	code := finishBufferedExecution(
		err,
		true,
		nil,
		nil,
		&stdout,
		&stderr,
	)
	if code != ExitConfig {
		t.Fatalf(
			"领域错误不得改变进程退出码: 得到%d，期望%d",
			code,
			ExitConfig,
		)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"领域JSON失败不应追加stderr: %q",
			stderr.String(),
		)
	}

	var envelope cliJSONErrorEnvelope
	if decodeErr := json.Unmarshal(
		stdout.Bytes(),
		&envelope,
	); decodeErr != nil {
		t.Fatalf(
			"领域错误信封不可解析: %v\n%s",
			decodeErr,
			stdout.String(),
		)
	}

	if envelope.ExitCode != ExitConfig ||
		envelope.ErrorCode != "write_conflict" ||
		!strings.Contains(
			envelope.Message,
			"写锁获取超时",
		) {
		t.Fatalf(
			"领域错误码没有准确透传: %+v",
			envelope,
		)
	}
}

// TestSetEntriesAutoFinalizeErrorPreservesMachineErrorCode验证Host-Agent
// auto_finalize.error使用同一精确领域分类，而不是按退出码推导为config。
func TestSetEntriesAutoFinalizeErrorPreservesMachineErrorCode(
	t *testing.T,
) {
	result := &entriesAutoFinalizeResult{
		Status:     entriesAutoStatusStopped,
		FailedStep: entriesAutoStepApply,
		RunID:      "20260724T170005Z",
	}

	setEntriesAutoFinalizeError(
		result,
		&ExitError{
			Code:        ExitConfig,
			MachineCode: "write_conflict",
			Err: errors.New(
				"auto原子应用失败[write_conflict]",
			),
		},
	)

	if result.Error == nil ||
		result.Error.ExitCode != ExitConfig ||
		result.Error.Code != "write_conflict" ||
		!strings.Contains(
			result.Error.Message,
			"write_conflict",
		) {
		t.Fatalf(
			"Auto领域错误码透传不符: %+v",
			result.Error,
		)
	}
}

func TestFinishBufferedExecutionDoesNotAppendSecondJSON(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	report := []byte(
		"{\"ok\":false,\"issues\":[\"drift\"]}\n",
	)

	code := finishBufferedExecution(
		&ExitError{
			Code: ExitDrift,
		},
		true,
		report,
		nil,
		&stdout,
		&stderr,
	)
	if code != ExitDrift {
		t.Fatalf(
			"退出码应为%d，得到%d",
			ExitDrift,
			code,
		)
	}
	if stdout.String() != string(report) {
		t.Fatalf(
			"业务JSON不得被二次包裹: %q",
			stdout.String(),
		)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"静默业务非零结果不应写stderr: %q",
			stderr.String(),
		)
	}
}

func TestFinishBufferedExecutionPreservesExistingOutputOnError(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	partial := []byte(
		"条目草稿预检\n",
	)

	code := finishBufferedExecution(
		&ExitError{
			Code: ExitInvalid,
			Err:  errors.New("草稿拒绝"),
		},
		true,
		partial,
		nil,
		&stdout,
		&stderr,
	)
	if code != ExitInvalid {
		t.Fatalf(
			"退出码应为%d，得到%d",
			ExitInvalid,
			code,
		)
	}
	if stdout.String() != string(partial) {
		t.Fatalf(
			"已有输出必须原样保留: %q",
			stdout.String(),
		)
	}
	if !strings.Contains(
		stderr.String(),
		"草稿拒绝",
	) {
		t.Fatalf(
			"已有非JSON输出场景应保持人读错误: %q",
			stderr.String(),
		)
	}
}

func TestRequestedJSON(
	t *testing.T,
) {
	if !requestedJSON(
		[]string{
			"--repo",
			"/tmp/repo",
			"index",
			"agent",
			"guide",
			"--json",
		},
	) {
		t.Fatal(
			"应识别--json",
		)
	}
}

func TestMCPInvocationUsesTopLevelCommandOnly(
	t *testing.T,
) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "direct_mcp",
			args: []string{
				"mcp",
			},
			want: true,
		},
		{
			name: "mcp_after_global_repo",
			args: []string{
				"--repo",
				"/tmp/repo",
				"mcp",
			},
			want: true,
		},
		{
			name: "mcp_after_global_json",
			args: []string{
				"--json",
				"mcp",
			},
			want: true,
		},
		{
			name: "agent_value_mcp_is_not_command",
			args: []string{
				"--json",
				"index",
				"agent",
				"guide",
				"--agent",
				"mcp",
			},
			want: false,
		},
		{
			name: "request_path_contains_mcp",
			args: []string{
				"index",
				"agent",
				"stage",
				"--request-file",
				"/tmp/mcp/request.json",
			},
			want: false,
		},
		{
			name: "unknown_flag_before_mcp",
			args: []string{
				"--unknown",
				"mcp",
			},
			want: false,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				got := isMCPInvocation(
					current.args,
				)
				if got != current.want {
					t.Fatalf(
						"isMCPInvocation=%t，期望%t，args=%v",
						got,
						current.want,
						current.args,
					)
				}
			},
		)
	}
}

func TestExecuteCLIUnknownCommandJSON(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := executeCLI(
		[]string{
			"--json",
			"not-a-command",
		},
		&stdout,
		&stderr,
	)
	if code == ExitOK {
		t.Fatal(
			"未知命令必须失败",
		)
	}

	var envelope cliJSONErrorEnvelope
	if err := json.Unmarshal(
		stdout.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf(
			"未知命令应返回单一JSON错误信封: %v\nstdout=%s\nstderr=%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	if envelope.OK ||
		envelope.ExitCode != code ||
		envelope.Message == "" {
		t.Fatalf(
			"未知命令错误信封不符: %+v",
			envelope,
		)
	}
}

func TestExecuteCLIAgentNamedMCPStillUsesJSONEnvelope(
	t *testing.T,
) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := executeCLI(
		[]string{
			"--json",
			"index",
			"agent",
			"guide",
			"--agent",
			"mcp",
			"--repo",
			t.TempDir(),
		},
		&stdout,
		&stderr,
	)
	if code == ExitOK {
		t.Fatal(
			"未初始化测试目录中的Guide必须失败",
		)
	}

	var envelope cliJSONErrorEnvelope
	if err := json.Unmarshal(
		stdout.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf(
			"--agent mcp必须仍走普通JSON失败信封: %v\nstdout=%s\nstderr=%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	if envelope.OK ||
		envelope.ExitCode != code ||
		envelope.ErrorCode == "" {
		t.Fatalf(
			"--agent mcp错误信封不符: %+v",
			envelope,
		)
	}
}
