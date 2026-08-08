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

func TestMaintainDictionaryUnparseableMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := maintainDictionaryUnparseableMessage() + "\n"
	expected := readMaintainGolden(
		t,
		"maintain_dictionary_unparseable.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Maintain dictionary-unparseable message changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestMaintainDictionaryMissingMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := maintainDictionaryMissingMessage() + "\n"
	expected := readMaintainGolden(
		t,
		"maintain_dictionary_missing.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Maintain dictionary-missing message changed during asset migration:\n%s",
			actual,
		)
	}
}
