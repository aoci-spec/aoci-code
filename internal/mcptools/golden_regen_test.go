package mcptools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRegenerateListToolsGolden 仅在显式设置 AOCI_UPDATE_GOLDEN=1 时把当前
// 真实 ListTools 协议写回 Golden。日常运行始终跳过, Golden 变更必须是有意的。
func TestRegenerateListToolsGolden(t *testing.T) {
	if os.Getenv("AOCI_UPDATE_GOLDEN") != "1" {
		t.Skip("set AOCI_UPDATE_GOLDEN=1 to intentionally regenerate the golden snapshot")
	}
	actual := canonicalListTools(t)
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "mcp_list_tools.json")
	if err := os.WriteFile(goldenPath, actual, 0o644); err != nil {
		t.Fatal(err)
	}
}
