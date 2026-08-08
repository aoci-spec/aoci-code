// index update消费有效include特殊文件的端到端测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

func TestUpdateAutomationAutoAppliesIncludedEmptyFile(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "marker.empty"),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	profile, err := curation.ProfilePath(
		root,
		"marker.empty",
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
					Path:         "marker.empty",
					Decision:     curation.DecisionInclude,
					Role:         "声明包级协议能力",
					Reason:       "空文件存在本身被运行时识别",
					Confidence:   97,
					SourceSHA256: profile.SourceSHA256,
					Agent:        "codex",
					UpdatedAt:    "2026-07-15T00:00:00Z",
				},
			},
		},
	); err != nil {
		t.Fatal(err)
	}

	endpoint := newUpdateAutomationEndpoint(
		t,
		"marker.empty[XC5T]: F:声明包级协议能力 | R:- | A:- | S:文件存在本身被运行时识别。",
		200,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	output, err := runUpdateAutomationCommand(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"有效include自动更新失败: %v\n%s",
			err,
			output,
		)
	}

	if endpoint.calls.Load() != 1 {
		t.Fatalf(
			"有效include特殊文件应调用端点一次,实际%d",
			endpoint.calls.Load(),
		)
	}

	if !strings.Contains(
		output,
		"included_missing(有效include)",
	) {
		t.Fatalf(
			"update输出缺Included子集:\n%s",
			output,
		)
	}

	indexText := readEntriesIndex(t, root)
	if !strings.Contains(
		indexText,
		"marker.empty[XC5T]",
	) {
		t.Fatalf(
			"auto未应用有效include特殊文件条目:\n%s",
			indexText,
		)
	}
}
