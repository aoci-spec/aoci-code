package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func TestCodeTargetApplyBindsChangedAndExplicitReuseInOneBatch(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeAttestationEntries(t, root, []string{"main.go", "reuse.go"})
	formal := volumeFileText(t, root, "aoci.code.txt")
	mainBefore := codeTargetTestEntry(t, formal, "main.go")
	reuseBefore := codeTargetTestEntry(t, formal, "reuse.go")
	mainAfter := strings.Replace(mainBefore, "own current attestation responsibility", "own finalized target behavior", 1)
	target := strings.Replace(formal, mainBefore, mainAfter, 1)
	target = strings.Replace(target, cognition.CodeVolumeMarker+"\n",
		cognition.CodeVolumeMarker+"\n"+codeTargetReusePrefix+"code:reuse.go\n", 1)
	writeVolumeTestFile(t, root, codeTargetIndexPath, target)

	for _, path := range []string{"main.go", "reuse.go"} {
		file := filepath.Join(root, path)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, append(raw, []byte("// finalized\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	session := connectMCPClient(t, root)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_update_entry", Arguments: map[string]any{
		"target_index": codeTargetIndexPath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var applied autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Attempted != 2 || applied.Applied != 2 {
		t.Fatalf("target batch did not apply once: %#v", applied)
	}
	after := volumeFileText(t, root, "aoci.code.txt")
	if !strings.Contains(after, mainAfter) || !strings.Contains(after, reuseBefore) {
		t.Fatalf("formal Code does not contain changed+reused Entries:\n%s", after)
	}
	targetAfter := volumeFileText(t, root, codeTargetIndexPath)
	if targetAfter != after || strings.Contains(targetAfter, codeTargetReusePrefix) {
		t.Fatalf("target plan was not consumed and synchronized:\n%s", targetAfter)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	for _, path := range []string{"main.go", "reuse.go"} {
		current, err := baseline.HashFile(filepath.Join(root, path))
		if err != nil || state.Files[path].SHA256 != current.SHA256 {
			t.Fatalf("final source binding missing for %s", path)
		}
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_update_entry", Arguments: map[string]any{
		"target_index": codeTargetIndexPath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var duplicate autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Status != autoStatusApplied || !duplicate.Aligned || duplicate.Attempted != 0 || duplicate.Applied != 0 {
		t.Fatalf("consumed target did not receive a read-only aligned result: %#v", duplicate)
	}
}

func TestCodeTargetApplyRejectsUnmarkedReuseWithoutWrites(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	formal := volumeFileText(t, root, "aoci.code.txt")
	writeVolumeTestFile(t, root, codeTargetIndexPath, formal)
	file := filepath.Join(root, "main.go")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(raw, []byte("// unmarked\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	result := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	var stopped autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Status != autoStatusStopped || stopped.FormalWritesStarted ||
		volumeFileText(t, root, "aoci.code.txt") != formal {
		t.Fatalf("unmarked reuse did not fail before formal writes: %#v", stopped)
	}
}

func TestCodeTargetApplyCommitsUpdateReuseAndDeleteOnce(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeAttestationEntries(t, root, []string{"main.go", "reuse.go", "gone.go"})
	formal := volumeFileText(t, root, "aoci.code.txt")
	mainBefore := codeTargetTestEntry(t, formal, "main.go")
	reuseBefore := codeTargetTestEntry(t, formal, "reuse.go")
	goneBefore := codeTargetTestEntry(t, formal, "gone.go")
	mainAfter := strings.Replace(mainBefore, "own current attestation responsibility", "own mixed target behavior", 1)
	target := strings.Replace(formal, mainBefore, mainAfter, 1)
	target = strings.Replace(target, goneBefore+"\n", "", 1)
	target = strings.Replace(target, cognition.CodeVolumeMarker+"\n", cognition.CodeVolumeMarker+"\n"+
		codeTargetReusePrefix+"code:reuse.go\n"+codeTargetDeletePrefix+"code:gone.go\n", 1)
	writeVolumeTestFile(t, root, codeTargetIndexPath, target)
	for _, path := range []string{"main.go", "reuse.go"} {
		file := filepath.Join(root, path)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, append(raw, []byte("// mixed target\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}

	originalWrite := writeAtomicIndex
	writes := 0
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes++
		return originalWrite(path, data, expected)
	}
	defer func() { writeAtomicIndex = originalWrite }()

	result := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	var applied autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Attempted != 3 || applied.Applied != 3 || writes != 1 {
		t.Fatalf("mixed target was not one atomic apply: result=%#v writes=%d", applied, writes)
	}
	after := volumeFileText(t, root, "aoci.code.txt")
	if !strings.Contains(after, mainAfter) || !strings.Contains(after, reuseBefore) || strings.Contains(after, goneBefore) {
		t.Fatalf("mixed target postimage is wrong:\n%s", after)
	}
	if targetAfter := volumeFileText(t, root, codeTargetIndexPath); targetAfter != after ||
		strings.Contains(targetAfter, codeTargetReusePrefix) || strings.Contains(targetAfter, codeTargetDeletePrefix) {
		t.Fatalf("target controls were not consumed:\n%s", targetAfter)
	}
}

func TestCodeTargetDeleteRequiresAbsentSource(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	formal := volumeFileText(t, root, "aoci.code.txt")
	main := codeTargetTestEntry(t, formal, "main.go")
	target := strings.Replace(formal, main+"\n", "", 1)
	target = strings.Replace(target, cognition.CodeVolumeMarker+"\n",
		cognition.CodeVolumeMarker+"\n"+codeTargetDeletePrefix+"code:main.go\n", 1)
	writeVolumeTestFile(t, root, codeTargetIndexPath, target)
	result := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	var stopped autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Status != autoStatusStopped || stopped.FormalWritesStarted || volumeFileText(t, root, "aoci.code.txt") != formal {
		t.Fatalf("present delete source did not stop before writes: %#v", stopped)
	}
}

func TestCodeTargetDeleteRecoversBaselineWithoutSecondWrite(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	formal := volumeFileText(t, root, "aoci.code.txt")
	main := codeTargetTestEntry(t, formal, "main.go")
	target := strings.Replace(formal, main+"\n", "", 1)
	target = strings.Replace(target, cognition.CodeVolumeMarker+"\n",
		cognition.CodeVolumeMarker+"\n"+codeTargetDeletePrefix+"code:main.go\n", 1)
	writeVolumeTestFile(t, root, codeTargetIndexPath, target)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}

	originalSave := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected delete baseline failure") }
	first := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	saveAtomicBaseline = originalSave
	t.Cleanup(func() { saveAtomicBaseline = originalSave })
	var stopped autoResult
	if err := json.Unmarshal([]byte(resText(t, first)), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Status != autoStatusStopped || strings.Contains(volumeFileText(t, root, "aoci.code.txt"), main) {
		t.Fatalf("delete Baseline failure did not retain the formal postimage: %#v", stopped)
	}

	writes := 0
	originalWrite := writeAtomicIndex
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes++
		return originalWrite(path, data, expected)
	}
	defer func() { writeAtomicIndex = originalWrite }()
	retry := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	var recovered autoResult
	if err := json.Unmarshal([]byte(resText(t, retry)), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Status != autoStatusApplied || !recovered.Aligned || writes != 0 {
		t.Fatalf("delete recovery did not converge without a second Code write: %#v writes=%d", recovered, writes)
	}
}

func TestCodeTargetDeleteRechecksAbsentSourceBeforeWrite(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	formal := volumeFileText(t, root, "aoci.code.txt")
	main := codeTargetTestEntry(t, formal, "main.go")
	target := strings.Replace(formal, main+"\n", "", 1)
	target = strings.Replace(target, cognition.CodeVolumeMarker+"\n",
		cognition.CodeVolumeMarker+"\n"+codeTargetDeletePrefix+"code:main.go\n", 1)
	writeVolumeTestFile(t, root, codeTargetIndexPath, target)
	sourcePath := filepath.Join(root, "main.go")
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}
	originalHook := beforeVolumeTargetWrites
	beforeVolumeTargetWrites = func() {
		if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { beforeVolumeTargetWrites = originalHook }()
	result := handleMCPApplyCodeTarget(root, "test", codeTargetIndexPath, newCognitionRefreshSession())
	var stopped autoResult
	if err := json.Unmarshal([]byte(resText(t, result)), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Status != autoStatusStopped || stopped.FormalWritesStarted || volumeFileText(t, root, "aoci.code.txt") != formal {
		t.Fatalf("reappeared source did not stop before Code write: %#v", stopped)
	}
}

func codeTargetTestEntry(t *testing.T, volume, path string) string {
	t.Helper()
	for lineNumber, line := range strings.Split(volume, "\n") {
		entry, ok := index.ParseEntryLine(line, lineNumber+1)
		if ok && entry.Filename == path {
			return entry.FullLine
		}
	}
	t.Fatalf("Entry missing for %s", path)
	return ""
}
