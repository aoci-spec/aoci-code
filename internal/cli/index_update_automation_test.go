// index update automation公共测试夹具与Off/Review主链测试。
//
// Auto成功、失败和跳过链路拆到index_update_automation_auto_test.go；
// Warning、冲突等边界继续位于独立测试文件，避免单文件超过600行。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

type updateAutomationEndpoint struct {
	server *httptest.Server
	calls  atomic.Int32
}

func newUpdateAutomationEndpoint(
	t *testing.T,
	reply string,
	statusCode int,
) *updateAutomationEndpoint {
	t.Helper()

	endpoint := &updateAutomationEndpoint{}

	endpoint.server = httptest.NewServer(
		http.HandlerFunc(func(
			writer http.ResponseWriter,
			request *http.Request,
		) {
			endpoint.calls.Add(1)

			writer.Header().Set(
				"Content-Type",
				"application/json",
			)

			if statusCode != http.StatusOK {
				writer.WriteHeader(
					statusCode,
				)

				_ = json.NewEncoder(
					writer,
				).Encode(
					map[string]any{
						"error": map[string]any{
							"message": "测试端点失败",
						},
					},
				)

				return
			}

			_ = json.NewEncoder(
				writer,
			).Encode(
				map[string]any{
					"choices": []any{
						map[string]any{
							"message": map[string]any{
								"content": reply,
							},
							"finish_reason": "stop",
						},
					},
					"usage": map[string]any{
						"prompt_tokens":     20,
						"completion_tokens": 10,
					},
				},
			)
		}),
	)

	t.Cleanup(
		endpoint.server.Close,
	)

	return endpoint
}

func configureUpdateAutomation(
	t *testing.T,
	root,
	mode,
	baseURL string,
) {
	t.Helper()

	cfg, err := config.LoadBase(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.SetAutomationMode(
		mode,
	); err != nil {
		t.Fatal(err)
	}

	cfg.AI.Enabled = true
	cfg.AI.Provider = "openai-compatible"
	cfg.AI.BaseURL = baseURL
	cfg.AI.Model = "test-model"
	cfg.AI.APIKeyEnv = ""
	cfg.AI.TimeoutSeconds = 5
	cfg.AI.MaxConcurrency = 1
	cfg.AI.PromptSnapshot = "none"

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}
}

func runUpdateAutomationCommand(
	t *testing.T,
	root string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root

	t.Cleanup(func() {
		flagRepo = oldRepo
	})

	command := newIndexUpdateCmd()
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
		nil,
	)

	return output.String(), err
}

func writeUpdateAutomationFile(
	t *testing.T,
	root,
	rel,
	content string,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)

	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		absolutePath,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func latestUpdateAutomationManifest(
	t *testing.T,
	root string,
) *draft.Manifest {
	t.Helper()

	runID, err := draft.LatestRunID(
		root,
		draft.KindEntries,
	)
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}

func requireUpdateAutomationExit(
	t *testing.T,
	err error,
	code int,
) {
	t.Helper()

	var exitErr *ExitError

	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != code {
		t.Fatalf(
			"期望 ExitError(%d),得到 %v",
			code,
			err,
		)
	}
}

func TestUpdateAutomationOffSkipsAIAndDrafts(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// off 漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:不应被调用 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeOff,
		endpoint.server.URL,
	)

	indexBefore := readEntriesIndex(
		t,
		root,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"off模式应成功: %v\n%s",
			err,
			output,
		)
	}

	if endpoint.calls.Load() != 0 {
		t.Fatalf(
			"off模式不得调用端点,实际%d次",
			endpoint.calls.Load(),
		)
	}

	if !strings.Contains(
		output,
		"automation.mode=off",
	) ||
		!strings.Contains(
			output,
			"未创建草稿",
		) {
		t.Fatalf(
			"off输出缺治理结论: %s",
			output,
		)
	}

	runIDs, err := draft.ListRunIDs(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(runIDs) != 0 {
		t.Fatalf(
			"off模式不得创建草稿: %+v",
			runIDs,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"off模式不得修改正式索引",
		)
	}
}

func TestUpdateAutomationReviewChecksAndStops(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// review 漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:review机器预检版 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeReview,
		endpoint.server.URL,
	)

	indexBefore := readEntriesIndex(
		t,
		root,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"review全净批次应成功: %v\n%s",
			err,
			output,
		)
	}

	if endpoint.calls.Load() != 1 {
		t.Fatalf(
			"review应调用端点一次,得到%d",
			endpoint.calls.Load(),
		)
	}

	manifest := latestUpdateAutomationManifest(
		t,
		root,
	)

	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Action !=
			draft.ReviewActionCheck ||
		manifest.Reviews[0].Rejected != 0 {
		t.Fatalf(
			"review审计不符: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"review绝不得应用: %+v",
			manifest,
		)
	}

	if readEntriesIndex(
		t,
		root,
	) != indexBefore {
		t.Fatal(
			"review模式不得修改正式索引",
		)
	}

	if !strings.Contains(
		output,
		"等待人工 diff/apply",
	) {
		t.Fatalf(
			"review输出缺人工停点: %s",
			output,
		)
	}
}
