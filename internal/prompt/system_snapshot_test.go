package prompt

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPromptGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name))
	if err != nil {
		t.Fatalf("read prompt golden %s: %v", name, err)
	}
	return string(data)
}

func TestSystemPromptSnapshotsRemainCompatible(
	t *testing.T,
) {
	headerSystem, _, err := BuildHeaderMessages(
		sampleInput(),
	)
	if err != nil {
		t.Fatal(err)
	}

	entryInput := sampleEntryInput()
	entrySystem, _, err := BuildEntryMessages(
		entryInput,
	)
	if err != nil {
		t.Fatal(err)
	}

	entryInput.OldEntry =
		"store.go[MStore5S]: F:旧职责 | R:- | A:New | S:非线程安全"
	entryUpdateSystem, _, err := BuildEntryMessages(
		entryInput,
	)
	if err != nil {
		t.Fatal(err)
	}

	entryCurationSystem, _, err := BuildEntryMessages(
		sampleEntryCurationInput(),
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		actual     string
		goldenFile string
	}{
		{
			name:       "header",
			actual:     headerSystem,
			goldenFile: "header_system_message.sha256",
		},
		{
			name:       "entry",
			actual:     entrySystem,
			goldenFile: "entry_system_message.sha256",
		},
		{
			name:       "entry-update",
			actual:     entryUpdateSystem,
			goldenFile: "entry_update_system_message.sha256",
		},
		{
			name:       "entry-curation",
			actual:     entryCurationSystem,
			goldenFile: "entry_curation_system_message.sha256",
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				digest := sha256.Sum256(
					[]byte(current.actual),
				)
				actual := fmt.Sprintf(
					"%x",
					digest[:],
				)
				expected := strings.TrimSpace(
					readPromptGolden(
						t,
						current.goldenFile,
					),
				)
				if actual != expected {
					t.Fatalf(
						"system Prompt changed: actual=%s expected=%s",
						actual,
						expected,
					)
				}
			},
		)
	}
}
