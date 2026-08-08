// Host-Agent --request-file UTF-8 输入与 Windows 中文保真测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func writeAgentRequestFile(
	t *testing.T,
	path string,
	value any,
	withBOM bool,
) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	if withBOM {
		data = append(
			[]byte{
				0xEF,
				0xBB,
				0xBF,
			},
			data...,
		)
	}

	if err := os.WriteFile(
		path,
		data,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAgentRequestInputModesAndUTF8(
	t *testing.T,
) {
	root := t.TempDir()

	validPath := filepath.Join(
		root,
		"中文请求.json",
	)

	if err := os.WriteFile(
		validPath,
		append(
			[]byte{
				0xEF,
				0xBB,
				0xBF,
			},
			[]byte(
				`{"message":"中文无损"}`,
			)...,
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	reader, source, err :=
		loadAgentRequestInput(
			false,
			validPath,
			nil,
			1024,
			"测试Stage",
		)
	if err != nil {
		t.Fatal(err)
	}

	data := make(
		[]byte,
		1024,
	)

	count, err := reader.Read(data)
	if err != nil {
		t.Fatal(err)
	}

	text := string(data[:count])

	if text !=
		`{"message":"中文无损"}` {
		t.Fatalf(
			"UTF-8 BOM应剥离且中文保持原样: %q",
			text,
		)
	}

	if want := fmt.Sprintf("request-file %q", validPath); source != want {
		t.Fatalf(
			"输入源说明未使用可逆引用格式: got=%q want=%q",
			source,
			want,
		)
	}

	for _, current := range []struct {
		name        string
		stdinJSON   bool
		requestFile string
		wantCode    int
		wantText    string
	}{
		{
			name:        "both",
			stdinJSON:   true,
			requestFile: validPath,
			wantCode:    ExitConfig,
			wantText:    "只能选择一个",
		},
		{
			name:     "neither",
			wantCode: ExitConfig,
			wantText: "必须显式选择",
		},
	} {
		t.Run(
			current.name,
			func(t *testing.T) {
				_, _, inputErr :=
					loadAgentRequestInput(
						current.stdinJSON,
						current.requestFile,
						bytes.NewBufferString("{}"),
						1024,
						"测试Stage",
					)

				var typed *agentRequestInputError

				if !errors.As(
					inputErr,
					&typed,
				) ||
					typed.Code !=
						current.wantCode ||
					!strings.Contains(
						inputErr.Error(),
						current.wantText,
					) {
					t.Fatalf(
						"输入模式错误不符: %v",
						inputErr,
					)
				}
			},
		)
	}
}

func TestLoadAgentRequestInputRejectsInvalidUTF8AndOversize(
	t *testing.T,
) {
	root := t.TempDir()

	invalidPath := filepath.Join(
		root,
		"invalid.json",
	)

	if err := os.WriteFile(
		invalidPath,
		[]byte{
			0x7B,
			0x22,
			0xFF,
			0x22,
			0x7D,
		},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadAgentRequestInput(
		false,
		invalidPath,
		nil,
		1024,
		"测试Stage",
	)

	var inputErr *agentRequestInputError

	if !errors.As(
		err,
		&inputErr,
	) ||
		inputErr.Code != ExitInvalid ||
		!strings.Contains(
			err.Error(),
			"不是合法UTF-8",
		) {
		t.Fatalf(
			"非法UTF-8必须硬拒: %v",
			err,
		)
	}

	oversizePath := filepath.Join(
		root,
		"oversize.json",
	)

	if err := os.WriteFile(
		oversizePath,
		[]byte("12345"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, _, err = loadAgentRequestInput(
		false,
		oversizePath,
		nil,
		4,
		"测试Stage",
	)

	if !errors.As(
		err,
		&inputErr,
	) ||
		inputErr.Code != ExitInvalid ||
		!strings.Contains(
			err.Error(),
			"超过上限",
		) {
		t.Fatalf(
			"超限请求必须硬拒: %v",
			err,
		)
	}
}

func TestEntriesStageRequestFilePreservesChinese(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	plan, _ := agentStageCurrentPlan(
		t,
		root,
	)

	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	requestPath := filepath.Join(
		t.TempDir(),
		"entries-request.json",
	)

	writeAgentRequestFile(
		t,
		requestPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path: "new.go",
					SourceSHA256: target.
						SourceSHA256,
					Entry: "new.go[XAP7T]: F:中文职责完整保留 | " +
						"R:- | A:- | S:不得把中文替换成问号",
				},
			},
		},
		true,
	)

	oldRepo := flagRepo
	oldJSON := flagJSON

	flagRepo = root
	flagJSON = true

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	command := newIndexAgentStageCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--request-file",
		requestPath,
	})

	if err := command.Execute(); err != nil {
		t.Fatalf(
			"Entries request-file Stage失败: %v\n%s",
			err,
			output.String(),
		)
	}

	var result agentStageResult

	if err := json.Unmarshal(
		output.Bytes(),
		&result,
	); err != nil {
		t.Fatalf(
			"Stage JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	data, err := draft.ReadFile(
		root,
		result.RunID,
		entryDraftFileName("new.go"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(data),
		"中文职责完整保留",
	) ||
		!strings.Contains(
			string(data),
			"不得把中文替换成问号",
		) {
		t.Fatalf(
			"Entries中文未无损进入草稿: %s",
			data,
		)
	}
}

func TestHeaderStageRequestFilePreservesChinese(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)

	plan, _ := agentHeaderStagePlan(
		t,
		root,
	)

	requestPath := filepath.Join(
		t.TempDir(),
		"header-request.json",
	)

	header := validAgentHeaderCandidate() +
		"#【中文保真】不得把中文替换成问号\n"

	writeAgentRequestFile(
		t,
		requestPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  header,
		},
		false,
	)

	oldRepo := flagRepo
	oldJSON := flagJSON

	flagRepo = root
	flagJSON = true

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	command :=
		newIndexAgentHeaderStageCmd()

	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--request-file",
		requestPath,
	})

	if err := command.Execute(); err != nil {
		t.Fatalf(
			"Header request-file Stage失败: %v\n%s",
			err,
			output.String(),
		)
	}

	var result agentHeaderStageResult

	if err := json.Unmarshal(
		output.Bytes(),
		&result,
	); err != nil {
		t.Fatalf(
			"Header Stage JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	data, err := draft.ReadFile(
		root,
		result.RunID,
		draft.HeaderFileName,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(data),
		"中文保真",
	) ||
		!strings.Contains(
			string(data),
			"不得把中文替换成问号",
		) {
		t.Fatalf(
			"Header中文未无损进入草稿: %s",
			data,
		)
	}
}
