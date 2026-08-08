// index build --missing消费正式include决策的集成测试。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func buildIndexBuildIncludeRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心 5常规\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "py.typed"),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	profile, err := curation.ProfilePath(
		root,
		"py.typed",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := curation.Save(
		root,
		&curation.Document{
			Version: curation.Version,
			Decisions: []curation.Decision{
				{
					Path:         "py.typed",
					Decision:     curation.DecisionInclude,
					Role:         "声明Python包提供类型信息",
					Reason:       "空文件存在本身是包级类型协议标记",
					Confidence:   99,
					SourceSHA256: profile.SourceSHA256,
					Agent:        "codex",
					UpdatedAt:    "2026-07-15T00:00:00Z",
				},
			},
		},
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestIndexBuildMissingGeneratesIncludedEmptyFile(
	t *testing.T,
) {
	root := buildIndexBuildIncludeRepo(t)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"py.typed[XRT5T]: F:声明Python包提供类型信息 | R:- | A:- | S:文件存在本身启用类型检查器识别。",
		200,
	)

	cfg := legacyTestConfig()
	cfg.AI.Enabled = true
	cfg.AI.Provider = "openai-compatible"
	cfg.AI.BaseURL = endpoint.server.URL
	cfg.AI.Model = "test-model"
	cfg.AI.PromptSnapshot = "none"

	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() {
		flagRepo = oldRepo
	})

	command := newIndexBuildCmd()
	if err := command.Flags().Set(
		"missing",
		"true",
	); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	if err := command.RunE(
		command,
		nil,
	); err != nil {
		t.Fatalf(
			"build有效include失败: %v\n%s",
			err,
			output.String(),
		)
	}

	if endpoint.calls.Load() != 1 {
		t.Fatalf(
			"有效include应调用端点一次,实际%d",
			endpoint.calls.Load(),
		)
	}

	if !strings.Contains(
		output.String(),
		"有效include 1 条进入AI起草队列",
	) {
		t.Fatalf(
			"build输出缺include说明:\n%s",
			output.String(),
		)
	}

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

	if len(manifest.Entries) != 1 ||
		manifest.Entries[0].Status != "drafted" {
		t.Fatalf(
			"有效include应形成Entry草稿: %+v",
			manifest.Entries,
		)
	}
}
