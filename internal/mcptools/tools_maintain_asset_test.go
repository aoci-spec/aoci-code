// Maintain稳定长合同资产迁移的字节级兼容测试。
package mcptools

import (
	"os"
	"path/filepath"
	"testing"
)

func readMaintainGolden(
	t *testing.T,
	name string,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			name,
		),
	)
	if err != nil {
		t.Fatalf(
			"read Maintain golden %s: %v",
			name,
			err,
		)
	}

	return string(canonicalGoldenBytes(data))
}

func TestMaintainToolDescriptionMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := maintainToolDescription() + "\n"
	// AOCI_UPDATE_GOLDEN=1 (make update-goldens) writes the production
	// rendering back instead of comparing against it.
	if os.Getenv("AOCI_UPDATE_GOLDEN") == "1" {
		path := filepath.Join("..", "..", "testdata", "golden", "maintain_tool_description.txt")
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected := readMaintainGolden(
		t,
		"maintain_tool_description.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Maintain MCP Description changed during asset migration:\n%s",
			actual,
		)
	}
}
