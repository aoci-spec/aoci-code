// ASCII-safe output of machine JSON for the ordinary CLI on Windows.
//
// Background:
//
// When Windows PowerShell 5 captures a native program's stdout directly, it interprets the bytes with the local console code page.
// When AOCI prints UTF-8 Chinese JSON, the multi-byte characters may be decoded wrongly, and in the extreme case
// the following ASCII double quotes are broken too, so ConvertFrom-Json cannot parse.
//
// Decision:
//
//   - enabled on Windows only; Linux and macOS keep the raw UTF-8 JSON;
//   - enabled only for the legal --json output of the ordinary CLI; human-readable text stays as it is;
//   - non-ASCII characters are encoded as the standard JSON \uXXXX escapes;
//   - supplementary-plane characters use UTF-16 surrogate pairs;
//   - MCP does not pass through the ordinary CLI's buffered write path and keeps the raw UTF-8 JSON-RPC byte stream.
//
// The JSON semantics are unchanged: any standard JSON parser restores a \u escape to the original Unicode string.
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"runtime"
	"unicode/utf16"
	"unicode/utf8"
)

// writeCLIData writes the buffered result of the ordinary CLI.
//
// A write error follows the historical ordinary output discipline: the root layer's final output manufactures no second business error.
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

// writeCLIDataWithError serves the JSON error envelope that needs to notice a write failure explicitly.
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

// writeCLIDataForPlatform writes data for an explicit platform, so the Windows behaviour can be tested in Linux CI.
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

// escapeJSONNonASCII turns every non-ASCII code point of legal UTF-8 JSON into a JSON Unicode escape.
//
// The JSON structural characters are all ASCII, so the object need not be re-parsed; the code-point replacement changes no field,
// number, boolean, null, existing backslash escape, indentation, or trailing newline.
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

	// A Chinese character usually turns from 3 UTF-8 bytes into 6 bytes of \uXXXX; reserving twice the size reduces growth.
	output.Grow(
		len(data) * 2,
	)

	for len(data) > 0 {
		current, size := utf8.DecodeRune(
			data,
		)

		if current == utf8.RuneError &&
			size == 1 {
			// The call entry handles only json.Valid data, so this branch is normally never entered.
			// The raw bytes are kept conservatively, so the helper does not silently lose data on abnormal input.
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

// appendJSONUnicodeEscape appends a fixed four-digit lower-case hexadecimal JSON escape.
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

// isASCIIBytes reports whether every byte of the sequence falls in the ASCII range.
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
