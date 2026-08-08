// Windows PowerShell 5普通CLI JSON的ASCII安全输出测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestEscapeJSONNonASCIIPreservesUnicodeSemantics(
	t *testing.T,
) {
	original := []byte(
		"{\n" +
			"  \"message\": \"仓库对齐\",\n" +
			"  \"emoji\": \"😀\",\n" +
			"  \"existing_escape\": \"\\u4e2d\",\n" +
			"  \"ascii\": \"AOCI\"\n" +
			"}\n",
	)

	escaped := escapeJSONNonASCII(
		original,
	)

	if !isASCIIBytes(escaped) {
		t.Fatalf(
			"转义后必须全部为ASCII:\n%s",
			escaped,
		)
	}

	for _, anchor := range []string{
		`\u4ed3\u5e93\u5bf9\u9f50`,
		`\ud83d\ude00`,
		`"existing_escape": "\u4e2d"`,
	} {
		if !bytes.Contains(
			escaped,
			[]byte(anchor),
		) {
			t.Fatalf(
				"转义结果缺少%q:\n%s",
				anchor,
				escaped,
			)
		}
	}

	var decoded struct {
		Message        string `json:"message"`
		Emoji          string `json:"emoji"`
		ExistingEscape string `json:"existing_escape"`
		ASCII          string `json:"ascii"`
	}

	if err := json.Unmarshal(
		escaped,
		&decoded,
	); err != nil {
		t.Fatalf(
			"ASCII安全JSON不可解析: %v\n%s",
			err,
			escaped,
		)
	}

	if decoded.Message != "仓库对齐" ||
		decoded.Emoji != "😀" ||
		decoded.ExistingEscape != "中" ||
		decoded.ASCII != "AOCI" {
		t.Fatalf(
			"转义前后语义变化: %+v",
			decoded,
		)
	}
}

func TestWriteCLIDataForPlatformWindowsEscapesValidJSON(
	t *testing.T,
) {
	input := []byte(
		"{\"message\":\"配置缺失\",\"ok\":false}\n",
	)

	var output bytes.Buffer

	if err := writeCLIDataForPlatform(
		&output,
		input,
		true,
		"windows",
	); err != nil {
		t.Fatal(err)
	}

	if !isASCIIBytes(
		output.Bytes(),
	) {
		t.Fatalf(
			"Windows JSON必须ASCII安全:\n%s",
			output.String(),
		)
	}

	var decoded map[string]any

	if err := json.Unmarshal(
		output.Bytes(),
		&decoded,
	); err != nil {
		t.Fatalf(
			"Windows JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if decoded["message"] != "配置缺失" {
		t.Fatalf(
			"中文语义未恢复: %#v",
			decoded,
		)
	}
}

func TestWriteCLIDataForPlatformLinuxPreservesUTF8JSON(
	t *testing.T,
) {
	input := []byte(
		"{\"message\":\"仓库对齐\"}\n",
	)

	var output bytes.Buffer

	if err := writeCLIDataForPlatform(
		&output,
		input,
		true,
		"linux",
	); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		output.Bytes(),
		input,
	) {
		t.Fatalf(
			"非Windows JSON必须保持原始UTF-8:\n得到=%q\n期望=%q",
			output.Bytes(),
			input,
		)
	}
}

func TestWriteCLIDataForPlatformWindowsLeavesHumanText(
	t *testing.T,
) {
	input := []byte(
		"条目草稿预检通过\n",
	)

	var output bytes.Buffer

	if err := writeCLIDataForPlatform(
		&output,
		input,
		true,
		"windows",
	); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		output.Bytes(),
		input,
	) {
		t.Fatalf(
			"非JSON输出不得伪装为JSON或Unicode转义:\n得到=%q\n期望=%q",
			output.Bytes(),
			input,
		)
	}
}

func TestWriteCLIJSONErrorForWindowsIsASCIISafe(
	t *testing.T,
) {
	var output bytes.Buffer

	err := writeCLIJSONErrorForPlatform(
		&output,
		&ExitError{
			Code: ExitConfig,
			Err:  errors.New("配置缺失"),
		},
		ExitConfig,
		"windows",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !isASCIIBytes(
		output.Bytes(),
	) {
		t.Fatalf(
			"Windows错误信封必须ASCII安全:\n%s",
			output.String(),
		)
	}

	if !strings.Contains(
		output.String(),
		`\u914d\u7f6e\u7f3a\u5931`,
	) {
		t.Fatalf(
			"错误信封缺少中文Unicode转义:\n%s",
			output.String(),
		)
	}

	var envelope cliJSONErrorEnvelope

	if err := json.Unmarshal(
		output.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf(
			"错误信封不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if envelope.Message != "配置缺失" ||
		envelope.ExitCode != ExitConfig ||
		envelope.ErrorCode != "config" {
		t.Fatalf(
			"错误信封语义不符: %+v",
			envelope,
		)
	}
}
