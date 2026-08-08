// Host-Agent Stage请求输入选择、普通文件检查与UTF-8字节读取。
//
// Windows PowerShell 5的文本管道可能按本地代码页重编码中文JSON，因此
// Entries、Header与Curation Stage同时支持:
//   - --request-file <path>: 推荐。CLI直接按字节读取UTF-8普通文件;
//   - --stdin-json: 兼容能够保证UTF-8字节流的环境。
//
// 两种输入方式互斥且必须二选一。文件与stdin都接受UTF-8无BOM或UTF-8
// BOM；目录、非普通文件、非法UTF-8、空输入和超限输入在JSON解析前拒绝。
// 未知字段、尾随对象、Plan、源码摘要和automation.mode等后续防线仍由
// 各协议与Stage内核裁决。
package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// agentRequestInputError保留输入选择、文件读取和字节编码错误的退出码。
type agentRequestInputError struct {
	Code int
	Err  error
}

func (e *agentRequestInputError) Error() string {
	if e == nil ||
		e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *agentRequestInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// loadAgentRequestInput选择唯一输入源并返回去除UTF-8 BOM后的内存字节流。
//
// requestFile不做仓库相对路径约束：它是操作者显式提供的本机请求文件，必须
// 支持Windows绝对路径。路径只TrimSpace，不改写盘符、反斜杠或大小写。
func loadAgentRequestInput(
	stdinJSON bool,
	requestFile string,
	stdin io.Reader,
	maxBytes int64,
	subject string,
) (io.Reader, string, error) {
	requestFile = strings.TrimSpace(
		requestFile,
	)

	switch {
	case stdinJSON &&
		requestFile != "":
		return nil, "", &agentRequestInputError{
			Code: ExitConfig,
			Err:  fmt.Errorf("%s", cliMessage("agent.input.conflict", subject)),
		}

	case !stdinJSON &&
		requestFile == "":
		return nil, "", &agentRequestInputError{
			Code: ExitConfig,
			Err:  fmt.Errorf("%s", cliMessage("agent.input.selection_required", subject)),
		}

	case maxBytes <= 0:
		return nil, "", &agentRequestInputError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("agent.input.limit_invalid", subject, maxBytes)),
		}
	}

	var (
		reader io.Reader
		source string
		file   *os.File
		err    error
	)

	if requestFile != "" {
		file, err = os.Open(
			requestFile,
		)
		if err != nil {
			return nil, "", &agentRequestInputError{
				Code: ExitConfig,
				Err: fmt.Errorf("%s", cliMessage(
					"agent.input.open_failed",
					subject,
					requestFile,
					localeSafeCLIDetail(err.Error()),
				)),
			}
		}
		defer file.Close()

		fileInfo, statErr := file.Stat()
		if statErr != nil {
			return nil, "", &agentRequestInputError{
				Code: ExitConfig,
				Err: fmt.Errorf("%s", cliMessage(
					"agent.input.stat_failed",
					subject,
					requestFile,
					localeSafeCLIDetail(statErr.Error()),
				)),
			}
		}

		switch {
		case fileInfo.IsDir():
			return nil, "", &agentRequestInputError{
				Code: ExitConfig,
				Err:  fmt.Errorf("%s", cliMessage("agent.input.is_directory", subject, requestFile)),
			}

		case !fileInfo.Mode().IsRegular():
			return nil, "", &agentRequestInputError{
				Code: ExitConfig,
				Err:  fmt.Errorf("%s", cliMessage("agent.input.not_regular", subject, requestFile)),
			}

		case fileInfo.Size() > maxBytes:
			return nil, "", &agentRequestInputError{
				Code: ExitInvalid,
				Err: fmt.Errorf("%s", cliMessage(
					"agent.input.file_too_large",
					subject,
					maxBytes,
					fileInfo.Size(),
					requestFile,
				)),
			}
		}

		reader = file
		source = fmt.Sprintf(
			"request-file %q",
			requestFile,
		)
	} else {
		if stdin == nil {
			return nil, "", &agentRequestInputError{
				Code: ExitInvalid,
				Err:  fmt.Errorf("%s", cliMessage("agent.input.stdin_nil", subject)),
			}
		}

		reader = stdin
		source = "stdin JSON"
	}

	data, err := io.ReadAll(
		io.LimitReader(
			reader,
			maxBytes+1,
		),
	)
	if err != nil {
		return nil, "", &agentRequestInputError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"agent.input.read_failed",
				source,
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	if int64(len(data)) >
		maxBytes {
		return nil, "", &agentRequestInputError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"agent.input.too_large_exact",
				source,
				maxBytes,
				len(data),
			)),
		}
	}

	data = bytes.TrimPrefix(
		data,
		[]byte{
			0xEF,
			0xBB,
			0xBF,
		},
	)

	if !utf8.Valid(data) {
		return nil, "", &agentRequestInputError{
			Code: ExitInvalid,
			Err:  fmt.Errorf("%s", cliMessage("agent.input.invalid_utf8", source)),
		}
	}

	if strings.TrimSpace(
		string(data),
	) == "" {
		return nil, "", &agentRequestInputError{
			Code: ExitInvalid,
			Err:  fmt.Errorf("%s", cliMessage("agent.input.empty", source)),
		}
	}

	return bytes.NewReader(
		data,
	), source, nil
}
