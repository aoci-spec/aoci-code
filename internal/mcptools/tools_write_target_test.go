package mcptools

import (
	"context"
	"encoding/json"
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
