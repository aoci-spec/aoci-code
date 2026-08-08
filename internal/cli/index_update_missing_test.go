// index update 对确定性跳过的AI前置防线测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestIndexUpdateOnlySkippedReturnsBeforeAI(t *testing.T) {
	root := buildUpdateRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "empty.go"),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(
		config.AutomationModeAuto,
	); err != nil {
		t.Fatal(err)
	}

	// 故意不配置AI。若Skip分类发生在buildAIClient之后，本测试会失败。
	cfg.AI.Enabled = false
	cfg.AI.BaseURL = ""
	cfg.AI.Model = ""
	cfg.AI.APIKeyEnv = "AOCI_TEST_MISSING_KEY"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	output, err := runUpdateAutomationCommand(t, root)
	if err != nil {
		t.Fatalf("只有SkippedMissing时update应成功早退: %v\n%s", err, output)
	}
	if !strings.Contains(output, "skipped_missing(确定性跳过)") ||
		!strings.Contains(output, "empty.go") ||
		!strings.Contains(output, "无可执行changed/ActionableMissing目标") {
		t.Fatalf("update应明确输出SkippedMissing终态:\n%s", output)
	}

	runIDs, err := draft.ListRunIDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 0 {
		t.Fatalf("只有SkippedMissing时不得创建草稿: %+v", runIDs)
	}
}
