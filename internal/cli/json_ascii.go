// Windows普通CLI机器JSON的ASCII安全输出。
//
// 背景:
//
// Windows PowerShell 5直接捕获原生程序stdout时，会使用本地控制台代码页解释
// 字节。AOCI输出UTF-8中文JSON时，多字节字符可能被错误解码，极端情况下还会
// 破坏后续ASCII双引号，使ConvertFrom-Json无法解析。
//
// 裁决:
//
//   - 仅Windows启用；Linux、macOS保持原始UTF-8 JSON；
//   - 仅普通CLI的合法--json输出启用；人读文本保持原样；
//   - 非ASCII字符编码为JSON标准\uXXXX转义；
//   - 补充平面字符使用UTF-16代理对；
//   - MCP不经过普通CLI缓冲写出路径，继续保持原始UTF-8 JSON-RPC字节流。
//
// JSON语义不变：任何标准JSON解析器都会把\u转义还原为原Unicode字符串。
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"runtime"
	"unicode/utf16"
	"unicode/utf8"
)

// writeCLIData写出普通CLI的缓冲结果。
//
// 写错误沿用历史普通输出纪律：根层最终输出不再制造第二个业务错误。
func writeCLIData(
	writer io.Writer,
	data []byte,
	jsonMode bool,
) {
	_ = writeCLIDataWithError(
		writer,
		data,
		jsonMode,
	)
}

// writeCLIDataWithError供需要显式感知写失败的JSON错误信封使用。
func writeCLIDataWithError(
	writer io.Writer,
	data []byte,
	jsonMode bool,
) error {
	return writeCLIDataForPlatform(
		writer,
		data,
		jsonMode,
		runtime.GOOS,
	)
}

// writeCLIDataForPlatform按显式平台写出数据，使Windows行为可在Linux CI测试。
func writeCLIDataForPlatform(
	writer io.Writer,
	data []byte,
	jsonMode bool,
	goos string,
) error {
	if len(data) == 0 {
		return nil
	}

	output := data

	if goos == "windows" &&
		jsonMode &&
		json.Valid(
			bytes.TrimSpace(data),
		) {
		output = escapeJSONNonASCII(
			data,
		)
	}

	_, err := writer.Write(
		output,
	)
	return err
}

// escapeJSONNonASCII把合法UTF-8 JSON中的全部非ASCII码点转为JSON Unicode转义。
//
// JSON结构字符本身均为ASCII，因此无需重解析对象；逐码点替换不会改变字段、
// 数字、布尔值、null、已有反斜杠转义、缩进或结尾换行。
func escapeJSONNonASCII(
	data []byte,
) []byte {
	if isASCIIBytes(data) {
		return append(
			[]byte{},
			data...,
		)
	}

	var output bytes.Buffer

	// 中文通常由3字节UTF-8变成6字节\uXXXX，预留两倍可减少扩容。
	output.Grow(
		len(data) * 2,
	)

	for len(data) > 0 {
		current, size := utf8.DecodeRune(
			data,
		)

		if current == utf8.RuneError &&
			size == 1 {
			// 调用入口只处理json.Valid数据，正常不会进入本分支。
			// 保守保留原字节，避免辅助函数在异常输入上静默丢数据。
			output.WriteByte(
				data[0],
			)
			data = data[1:]
			continue
		}

		if current <= 0x7f {
			output.WriteByte(
				byte(current),
			)
		} else if current <= 0xffff {
			appendJSONUnicodeEscape(
				&output,
				current,
			)
		} else {
			high, low := utf16.EncodeRune(
				current,
			)

			appendJSONUnicodeEscape(
				&output,
				high,
			)
			appendJSONUnicodeEscape(
				&output,
				low,
			)
		}

		data = data[size:]
	}

	return output.Bytes()
}

// appendJSONUnicodeEscape追加固定四位小写十六进制JSON转义。
func appendJSONUnicodeEscape(
	output *bytes.Buffer,
	value rune,
) {
	const hexadecimal = "0123456789abcdef"

	output.WriteString(
		`\u`,
	)
	output.WriteByte(
		hexadecimal[(value>>12)&0x0f],
	)
	output.WriteByte(
		hexadecimal[(value>>8)&0x0f],
	)
	output.WriteByte(
		hexadecimal[(value>>4)&0x0f],
	)
	output.WriteByte(
		hexadecimal[value&0x0f],
	)
}

// isASCIIBytes判断字节序列是否全部落在ASCII范围。
func isASCIIBytes(
	data []byte,
) bool {
	for _, current := range data {
		if current >= utf8.RuneSelf {
			return false
		}
	}

	return true
}
