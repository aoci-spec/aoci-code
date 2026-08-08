package prompt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type promptSnapshot struct {
	Name   string `json:"name"`
	System string `json:"system"`
	User   string `json:"user"`
}

func TestCompletePromptOutputSnapshot(t *testing.T) {
	headerSystem, headerUser, err := BuildHeaderMessages(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	headerMinimalInput := sampleInput()
	headerMinimalInput.ProjectName = ""
	headerMinimalInput.CurrentHeader = ""
	headerMinimalInput.TotalFiles = -1
	headerMinimalInput.Dirs = nil
	headerMinimalInput.Exts = nil
	headerMinimalInput.SampleFiles = nil
	headerMinimalSystem, headerMinimalUser, err := BuildHeaderMessages(headerMinimalInput)
	if err != nil {
		t.Fatal(err)
	}
	entryInput := sampleEntryInput()
	entrySystem, entryUser, err := BuildEntryMessages(entryInput)
	if err != nil {
		t.Fatal(err)
	}
	entryMinimalInput := sampleEntryInput()
	entryMinimalInput.SuggestedSection = ""
	entryMinimalInput.NeighborEntries = nil
	entryMinimalSystem, entryMinimalUser, err := BuildEntryMessages(entryMinimalInput)
	if err != nil {
		t.Fatal(err)
	}
	entryInput.OldEntry = "store.go[MStore5S]: F:旧职责 | R:- | A:New | S:非线程安全"
	updateSystem, updateUser, err := BuildEntryMessages(entryInput)
	if err != nil {
		t.Fatal(err)
	}
	curationInput := sampleEntryCurationInput()
	curationSystem, curationUser, err := BuildEntryMessages(curationInput)
	if err != nil {
		t.Fatal(err)
	}
	curationInput.SourceText = "marker\n"
	curationSourceSystem, curationSourceUser, err := BuildEntryMessages(curationInput)
	if err != nil {
		t.Fatal(err)
	}

	snapshots := []promptSnapshot{
		{Name: "header-existing", System: headerSystem, User: headerUser},
		{Name: "header-minimal", System: headerMinimalSystem, User: headerMinimalUser},
		{Name: "entry-create", System: entrySystem, User: entryUser},
		{Name: "entry-minimal", System: entryMinimalSystem, User: entryMinimalUser},
		{Name: "entry-update", System: updateSystem, User: updateUser},
		{Name: "entry-curation-no-source", System: curationSystem, User: curationUser},
		{Name: "entry-curation-with-source", System: curationSourceSystem, User: curationSourceUser},
	}
	actual, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	digest := sha256.Sum256(actual)
	actualHash := fmt.Sprintf("%x", digest[:])
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "prompt_complete_outputs.sha256")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := strings.TrimSpace(string(expected))
	if actualHash != expectedHash {
		t.Fatalf("complete prompt output changed: actual=%s expected=%s", actualHash, expectedHash)
	}
}
