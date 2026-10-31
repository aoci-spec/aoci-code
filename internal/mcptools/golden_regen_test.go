package mcptools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRegenerateListToolsGolden writes the current real ListTools protocol back to the Golden only when AOCI_UPDATE_GOLDEN=1
// is set explicitly. Ordinary runs always skip it; a Golden change must be deliberate.
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
