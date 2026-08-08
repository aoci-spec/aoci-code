// 普通CLI的统一JSON失败信封。
//
// 成功业务对象保持既有顶层协议，不强制套ok/data外壳。
// 本信封只用于--json请求在失败前尚未输出业务JSON的场景。
package cli

import (
	"encoding/json"
	"errors"
	"io"
	"runtime"
)

const cliJSONErrorVersion = 1

// cliJSONErrorEnvelope是普通CLI稳定失败协议。
type cliJSONErrorEnvelope struct {
	Version   int    `json:"version"`
	OK        bool   `json:"ok"`
	ExitCode  int    `json:"exit_code"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
}

// writeCLIJSONError输出唯一JSON错误对象。
func writeCLIJSONError(
	writer io.Writer,
	err error,
	exitCode int,
) error {
	return writeCLIJSONErrorForPlatform(
		writer,
		err,
		exitCode,
		runtime.GOOS,
	)
}

// writeCLIJSONErrorForPlatform按显式平台输出错误信封，供Windows行为测试。
func writeCLIJSONErrorForPlatform(
	writer io.Writer,
	err error,
	exitCode int,
	goos string,
) error {
	envelope := cliJSONErrorEnvelope{
		Version:  cliJSONErrorVersion,
		OK:       false,
		ExitCode: exitCode,
		ErrorCode: classifyCLIErrorCode(
			err,
			exitCode,
		),
		Message: localizedCLIErrorMessage(err, exitCode),
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		envelope.Details = exitErr.Details
	}

	data, marshalErr := json.MarshalIndent(
		envelope,
		"",
		"  ",
	)
	if marshalErr != nil {
		return marshalErr
	}

	data = append(
		data,
		'\n',
	)

	return writeCLIDataForPlatform(
		writer,
		data,
		true,
		goos,
	)
}

// classifyCLIErrorCode把错误映射为稳定机器分类。
//
// 调用方已经掌握write_conflict等精确领域失败时，通过ExitError.MachineCode
// 显式透传；它优先于通用退出码映射，但不改变进程退出码。没有领域码时继续
// 使用既有drift/invalid/config/internal分类。
//
// exit 1既可能表示治理漂移，也可能来自尚未迁移的Doctor等命令，因此只有
// 明确的ExitError业务漂移才分类为drift，其他exit 1归为command_failed。
func classifyCLIErrorCode(
	err error,
	exitCode int,
) string {
	var exitErr *ExitError

	if errors.As(
		err,
		&exitErr,
	) {
		if exitErr.MachineCode != "" {
			return exitErr.MachineCode
		}

		switch exitErr.Code {
		case ExitDrift:
			return "drift"
		case ExitInvalid:
			return "invalid"
		case ExitConfig:
			return "config"
		case ExitInternal:
			return "internal"
		}
	}

	switch exitCode {
	case ExitInvalid:
		return "invalid"
	case ExitConfig:
		return "config"
	case ExitInternal:
		return "internal"
	default:
		return "command_failed"
	}
}
