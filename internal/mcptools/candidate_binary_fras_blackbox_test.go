package mcptools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCandidateBinaryBatchFRASBlackBox runs only when the caller supplies the
// formally built candidate binary. It proves the product interaction without
// importing or inspecting implementation facts in the spawned Host flow.
func TestCandidateBinaryBatchFRASBlackBox(t *testing.T) {
	binary := os.Getenv("AOCI_CANDIDATE_BINARY")
	if binary == "" {
		t.Skip("AOCI_CANDIDATE_BINARY is required for the formal candidate black box")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	root, input := buildBatchFRASFixture(t, 14)
	for index := range input {
		rel := input[index].Path
		writeVolumeTestFile(t, root, rel, fmt.Sprintf(
			"package fixture\n\nconst Item%02d = %d\nconst Changed%02d = true\n",
			index, index, index,
		))
		input[index].SourceSHA256 = volumeSourceSHA(t, root, rel)
	}
	input = permuteBatchFRASInput(t, input)
	input[3].NewEntry = batchFRASLine(input[3], "-", "a,b,c,d,e,f,g")
	input[10].NewEntry = batchFRASLine(input[10], "a,b,c,d,e,f,g,h,i", "Item10")
	originalCandidates := append([]updateEntryItemIn{}, input...)
	correctCandidatesBefore := blackBoxCorrectCandidateDigest(t, originalCandidates, 3, 10)

	indexBefore := fileSHA256(t, filepath.Join(root, "aoci.code.txt"))
	baselineBefore := fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json"))
	applicationBefore := successfulBatchApplications(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var mcpStderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, "--repo", root, "mcp")
	command.Stderr = &mcpStderr
	client := mcp.NewClient(&mcp.Implementation{Name: "fras-black-box", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}

	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 9 {
		t.Fatalf("candidate MCP surface changed: tools=%d err=%v", len(listed.Tools), err)
	}
	maintainText := callCandidateTool(t, ctx, session, "aoci_maintain", map[string]any{})
	var maintain autoResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.Status != autoStatusRepairRequired || len(maintain.Candidates) != 14 {
		t.Fatalf("candidate Maintain did not expose the complete batch: %+v", maintain)
	}

	firstText := callCandidateTool(t, ctx, session, "aoci_update_entry", blackBoxBatchArguments(input))
	var first autoResult
	if err := json.Unmarshal([]byte(firstText), &first); err != nil {
		t.Fatal(err)
	}
	if first.Status != autoStatusRepairRequired || first.Attempted != 14 || first.Applied != 0 ||
		first.FormalWritesStarted || first.FindingCount != 2 ||
		!reflect.DeepEqual(first.RetryScope, []string{"code:src/item00.go", "code:src/item10.go"}) {
		t.Fatalf("first candidate run did not return both exact Findings: %+v", first)
	}
	if first.Findings[0].CandidateIndex != 4 || first.Findings[0].Path != "src/item00.go" || first.Findings[0].Field != "A" ||
		first.Findings[1].CandidateIndex != 11 || first.Findings[1].Path != "src/item10.go" || first.Findings[1].Field != "R" {
		t.Fatalf("candidate binary renumbered the deterministic shuffled batch: %+v", first.Findings)
	}
	if fileSHA256(t, filepath.Join(root, "aoci.code.txt")) != indexBefore ||
		fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json")) != baselineBefore ||
		successfulBatchApplications(t, root) != applicationBefore {
		t.Fatal("first candidate run changed formal Index, Baseline, or Application state")
	}

	input[3].NewEntry = batchFRASLine(input[3], "-", "a,b,c,d,e,f")
	input[10].NewEntry = batchFRASLine(input[10], "code:src/item00.go,code:src/item01.go,code:src/item02.go,code:src/item03.go,code:src/item04.go,code:src/item05.go,code:src/item06.go,code:src/item07.go", "Item10")
	for index := range input {
		if index == 3 || index == 10 {
			continue
		}
		if input[index] != originalCandidates[index] {
			t.Fatalf("unlisted candidate %d changed between black-box attempts", index+1)
		}
	}
	correctCandidatesAfter := blackBoxCorrectCandidateDigest(t, input, 3, 10)
	if correctCandidatesAfter != correctCandidatesBefore {
		t.Fatal("unlisted candidate bytes changed between black-box attempts")
	}
	secondText := callCandidateTool(t, ctx, session, "aoci_update_entry", blackBoxBatchArguments(input))
	var second autoResult
	if err := json.Unmarshal([]byte(secondText), &second); err != nil {
		t.Fatal(err)
	}
	if second.Status != autoStatusApplied || !second.Aligned || second.Attempted != 14 ||
		second.Applied != 14 || second.Remaining != 0 || !second.FormalWritesStarted || second.FindingCount != 0 {
		t.Fatalf("second candidate run was not atomically applied: %+v", second)
	}
	indexAfter := fileSHA256(t, filepath.Join(root, "aoci.code.txt"))
	baselineAfter := fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json"))
	if indexAfter == indexBefore || baselineAfter == baselineBefore {
		t.Fatal("successful candidate run did not advance Index and Baseline")
	}
	if successfulBatchApplications(t, root) != applicationBefore+1 {
		t.Fatal("successful candidate run did not record exactly one successful Application")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if mcpStderr.Len() != 0 {
		t.Fatalf("MCP stdio candidate wrote diagnostics to stderr: %s", mcpStderr.String())
	}

	verify := runCandidateCLI(t, binary, root, "--json", "verify")
	check := runCandidateCLI(t, binary, root, "--json", "check")
	guide := runCandidateCLI(t, binary, root, "--json", "index", "agent", "guide", "--agent", "codex")
	if !strings.Contains(verify, `"structure_valid": true`) || !strings.Contains(verify, `"governance_aligned": true`) ||
		!strings.Contains(check, `"ok": true`) || !strings.Contains(guide, `"stage": "aligned"`) {
		t.Fatalf("candidate closure was not aligned:\nverify=%s\ncheck=%s\nguide=%s", verify, check, guide)
	}

	t.Logf("aoci_tool_calls=3 shell_aoci_calls=3 repair_round_trips=1 source_reads=0 help_calls=0 per_candidate_previews=0 deterministic_ms=%d findings=%d correct_candidate_digest_before=%s correct_candidate_digest_after=%s failed_run=%s successful_run=%s index_before=%s index_failed=%s index_after=%s baseline_before=%s baseline_failed=%s baseline_after=%s",
		maintain.Metrics.DeterministicMs+first.Metrics.DeterministicMs+second.Metrics.DeterministicMs,
		first.FindingCount, correctCandidatesBefore, correctCandidatesAfter, firstText, secondText,
		indexBefore, indexBefore, indexAfter, baselineBefore, baselineBefore, baselineAfter,
	)
}

func TestCandidateBinaryMixedDomainCandidateIndicesBlackBox(t *testing.T) {
	binary := os.Getenv("AOCI_CANDIDATE_BINARY")
	if binary == "" {
		t.Skip("AOCI_CANDIDATE_BINARY is required for the formal candidate black box")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	root := buildSingleCodeWriteRepo(t, true)
	writeVolumeTestFile(t, root, "z.go", "package main\n\nconst Z = true\n")
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n"+
		"z.go[CD7S]: F:describe fixture z | R:- | A:Z | S:-\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	input := []updateEntryItemIn{
		{ObjectRef: "database://primary/public/users", NewEntry: "users[DB9S]: F:store updated canonical user account state | R:- | A:a,b,c,d,e,f,g | S:Hard deletion remains forbidden", SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt")},
		{Path: "z.go", NewEntry: "z.go[CD7S]: F:describe updated fixture z | R:- | A:a,b,c,d,e,f,g | S:-", SourceSHA256: volumeSourceSHA(t, root, "z.go")},
		{Path: "main.go", NewEntry: volumeUpdateLine, SourceSHA256: volumeSourceSHA(t, root, "main.go")},
	}
	codeBefore := fileSHA256(t, filepath.Join(root, "aoci.code.txt"))
	databaseBefore := fileSHA256(t, filepath.Join(root, "aoci.database.txt"))
	baselineBefore := fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json"))
	applicationBefore := successfulBatchApplications(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var mcpStderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, "--repo", root, "mcp")
	command.Stderr = &mcpStderr
	client := mcp.NewClient(&mcp.Implementation{Name: "mixed-domain-black-box", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resultText := callCandidateTool(t, ctx, session, "aoci_update_entry", blackBoxBatchArguments(input))
	var result autoResult
	if err := json.Unmarshal([]byte(resultText), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != autoStatusRepairRequired || result.Applied != 0 || result.FormalWritesStarted || len(result.Findings) != 2 {
		t.Fatalf("mixed-domain candidate did not fail before writes: %+v", result)
	}
	want := map[string]int{"code:z.go": 2, "database://primary/public/users": 1}
	for _, finding := range result.Findings {
		if finding.CandidateIndex != want[finding.CanonicalObjectIdentity] {
			t.Fatalf("candidate binary renumbered a mixed-domain candidate: %+v", finding)
		}
	}
	if fileSHA256(t, filepath.Join(root, "aoci.code.txt")) != codeBefore ||
		fileSHA256(t, filepath.Join(root, "aoci.database.txt")) != databaseBefore ||
		fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json")) != baselineBefore ||
		successfulBatchApplications(t, root) != applicationBefore {
		t.Fatal("mixed-domain repair rejection changed a formal Volume, Baseline, or Application state")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if mcpStderr.Len() != 0 {
		t.Fatalf("mixed-domain MCP candidate wrote diagnostics to stderr: %s", mcpStderr.String())
	}
	t.Logf("mixed_domain_result=%s", resultText)
}

func blackBoxCorrectCandidateDigest(t *testing.T, input []updateEntryItemIn, excluded ...int) string {
	t.Helper()
	excludedSet := map[int]bool{}
	for _, index := range excluded {
		excludedSet[index] = true
	}
	correct := make([]updateEntryItemIn, 0, len(input)-len(excludedSet))
	for index, candidate := range input {
		if !excludedSet[index] {
			correct = append(correct, candidate)
		}
	}
	data, err := json.Marshal(correct)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func blackBoxBatchArguments(input []updateEntryItemIn) map[string]any {
	entries := make([]map[string]any, 0, len(input))
	for _, candidate := range input {
		entry := map[string]any{"new_entry": candidate.NewEntry, "source_sha256": candidate.SourceSHA256}
		if candidate.Path != "" {
			entry["path"] = candidate.Path
		} else {
			entry["object_ref"] = candidate.ObjectRef
		}
		if candidate.CandidateID != "" {
			entry["candidate_id"] = candidate.CandidateID
		}
		if candidate.BatchID != "" {
			entry["batch_id"] = candidate.BatchID
		}
		entries = append(entries, entry)
	}
	return map[string]any{"entries": entries}
}

func callCandidateTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) string {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return resText(t, result)
}

func runCandidateCLI(t *testing.T, binary, root string, arguments ...string) string {
	t.Helper()
	args := append([]string{"--repo", root}, arguments...)
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("candidate CLI failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("candidate CLI wrote unexpected stderr: %s", stderr.String())
	}
	return stdout.String()
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	fingerprint, err := baseline.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint.SHA256
}
