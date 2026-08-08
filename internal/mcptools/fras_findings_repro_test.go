package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestRepairFindingMachineJSONIncludesZeroValues(t *testing.T) {
	data, err := json.Marshal(cognition.RepairFinding{})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{
		`"candidate_index":0`, `"path":""`, `"canonical_object_identity":""`,
		`"domain":""`, `"field":""`, `"rule_code":""`, `"expected":""`,
		`"actual":""`, `"cause":""`, `"safe_repair_action":""`,
	} {
		if !strings.Contains(string(data), token) {
			t.Fatalf("Repair Finding omitted zero-value machine field %s: %s", token, data)
		}
	}
}

func TestRepairRetryScopeUsesCanonicalIdentityAcrossDomains(t *testing.T) {
	findings := []cognition.RepairFinding{
		{Path: "src/ignored.go"},
		{Path: "src/item.go", CanonicalObjectIdentity: "code:src/item.go"},
		{Path: "aoci.database.txt", CanonicalObjectIdentity: "database://primary/public/users"},
		{Path: "src/item.go", CanonicalObjectIdentity: "code:src/item.go"},
	}
	want := []string{"code:src/item.go", "database://primary/public/users"}
	if actual := RepairRetryScope(findings); !reflect.DeepEqual(actual, want) {
		t.Fatalf("retry_scope did not use deduplicated canonical identities: got=%v want=%v", actual, want)
	}
}

func buildBatchFRASFixture(t *testing.T, total int) (string, []updateEntryItemIn) {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	var code strings.Builder
	code.WriteString(cognition.CodeVolumeMarker + "\n")
	code.WriteString("===FRAS batch" + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n")
	for candidateIndex := 0; candidateIndex < total; candidateIndex++ {
		rel := fmt.Sprintf("src/item%02d.go", candidateIndex)
		name := filepath.Base(rel)
		writeVolumeTestFile(t, root, rel, fmt.Sprintf("package fixture\n\nconst Item%02d = %d\n", candidateIndex, candidateIndex))
		code.WriteString(fmt.Sprintf("%s[CD7S]: F:describe fixture item %02d | R:- | A:Item%02d | S:-\n", name, candidateIndex, candidateIndex))
	}
	writeVolumeTestFile(t, root, "aoci.code.txt", code.String())
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

	input := make([]updateEntryItemIn, 0, total)
	for candidateIndex := 0; candidateIndex < total; candidateIndex++ {
		rel := fmt.Sprintf("src/item%02d.go", candidateIndex)
		input = append(input, updateEntryItemIn{
			Path:         rel,
			NewEntry:     fmt.Sprintf("item%02d.go[CD7S]: F:describe updated fixture item %02d | R:- | A:Item%02d | S:-", candidateIndex, candidateIndex, candidateIndex),
			SourceSHA256: volumeSourceSHA(t, root, rel),
		})
	}
	return root, input
}

var deterministicFRASPermutation = []int{9, 2, 13, 0, 5, 1, 11, 4, 8, 6, 10, 3, 12, 7}

func permuteBatchFRASInput(t *testing.T, input []updateEntryItemIn) []updateEntryItemIn {
	t.Helper()
	if len(input) != len(deterministicFRASPermutation) {
		t.Fatalf("deterministic FRAS permutation requires %d candidates, got %d", len(deterministicFRASPermutation), len(input))
	}
	permuted := make([]updateEntryItemIn, 0, len(input))
	for _, candidateIndex := range deterministicFRASPermutation {
		permuted = append(permuted, input[candidateIndex])
	}
	return permuted
}

func batchFRASLine(candidate updateEntryItemIn, relation, api string) string {
	name := filepath.Base(candidate.Path)
	return fmt.Sprintf("%s[CD7S]: F:describe updated fixture %s | R:%s | A:%s | S:-", name, name, relation, api)
}

func buildNonLexicographicCodeFixture(t *testing.T) (string, updateEntryItemIn, updateEntryItemIn) {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, "a.go", "package fixture\n\nconst A = true\n")
	writeVolumeTestFile(t, root, "z.go", "package fixture\n\nconst Z = true\n")
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Non-lexicographic "+filepath.ToSlash(root)+"/===\n"+
		"a.go[CD7S]: F:describe fixture a | R:- | A:A | S:-\n"+
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
	zCandidate := updateEntryItemIn{Path: "z.go", NewEntry: "z.go[CD7S]: F:describe updated fixture z | R:- | A:a,b,c,d,e,f,g | S:-", SourceSHA256: volumeSourceSHA(t, root, "z.go")}
	aCandidate := updateEntryItemIn{Path: "a.go", NewEntry: "a.go[CD7S]: F:describe updated fixture a | R:- | A:A | S:-", SourceSHA256: volumeSourceSHA(t, root, "a.go")}
	return root, zCandidate, aCandidate
}

func atomicFRASItems(input []updateEntryItemIn) []AtomicUpdateItem {
	items := make([]AtomicUpdateItem, 0, len(input))
	for _, candidate := range input {
		items = append(items, AtomicUpdateItem{
			Path: candidate.Path, NewEntry: candidate.NewEntry, SourceSHA256: candidate.SourceSHA256,
		})
	}
	return items
}

func TestBatchFRASFindingsRepairAndAtomicApply(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 14)
	input = permuteBatchFRASInput(t, input)
	input[3].NewEntry = batchFRASLine(input[3], "-", "a,b,c,d,e,f,g")
	input[10].NewEntry = batchFRASLine(input[10], "a,b,c,d,e,f,g,h,i", "Item10")
	originalCandidates := append([]updateEntryItemIn{}, input...)

	indexBefore := volumeFileText(t, root, "aoci.code.txt")
	baselineBefore := volumeFileText(t, root, ".aoci/baseline.json")
	applicationsBefore := successfulBatchApplications(t, root)
	firstCall := handleMCPUpdateBatch(root, "repro-version", input)
	first := decodeAutoResult(t, firstCall)
	firstRaw := maintainResultText(t, firstCall)
	if first.Status != autoStatusRepairRequired || first.Attempted != 14 || first.Applied != 0 ||
		first.Remaining != 14 || first.FormalWritesStarted || first.FindingCount != 2 ||
		!first.PreserveOtherCandidates ||
		!reflect.DeepEqual(first.RetryScope, []string{"code:src/item00.go", "code:src/item10.go"}) {
		t.Fatalf("mixed FRAS batch repair contract is incomplete: %+v", first)
	}
	expected := []struct {
		candidateIndex                          int
		path, field, ruleCode, expected, actual string
	}{
		{4, "src/item00.go", "A", "fras_a_too_many_items", "max_items=6", "item_count=7"},
		{11, "src/item10.go", "R", "fras_r_too_many_items", "max_items=8", "item_count=9"},
	}
	for index, want := range expected {
		finding := first.Findings[index]
		if finding.CandidateIndex != want.candidateIndex || finding.Path != want.path ||
			finding.CanonicalObjectIdentity != "code:"+want.path || finding.Domain != "code" ||
			finding.Field != want.field || finding.RuleCode != want.ruleCode ||
			finding.Expected != want.expected || finding.Actual != want.actual ||
			finding.Cause == "" || finding.SafeRepairAction == "" {
			t.Fatalf("finding %d is not actionable: %+v", index, finding)
		}
	}
	for _, token := range []string{
		`"formal_writes_started":false`, `"finding_count":2`, `"findings":[`,
		`"candidate_index":4`, `"path":"src/item00.go"`, `"candidate_index":11`,
		`"preserve_other_candidates":true`, `"retry_scope":["code:src/item00.go","code:src/item10.go"]`,
	} {
		if !strings.Contains(firstRaw, token) {
			t.Fatalf("machine JSON omitted %q:\n%s", token, firstRaw)
		}
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore ||
		volumeFileText(t, root, ".aoci/baseline.json") != baselineBefore ||
		successfulBatchApplications(t, root) != applicationsBefore {
		t.Fatal("rejected FRAS batch changed formal Index, Baseline, or Application audit state")
	}

	input[3].NewEntry = batchFRASLine(input[3], "-", "a,b,c,d,e,f")
	input[10].NewEntry = batchFRASLine(input[10], "code:src/item00.go,code:src/item01.go,code:src/item02.go,code:src/item03.go,code:src/item04.go,code:src/item05.go,code:src/item06.go,code:src/item07.go", "Item10")
	for candidateIndex := range input {
		if candidateIndex == 3 || candidateIndex == 10 {
			continue
		}
		if input[candidateIndex].NewEntry != originalCandidates[candidateIndex].NewEntry {
			t.Fatalf("valid candidate %d changed during exact repair", candidateIndex)
		}
	}
	second := decodeAutoResult(t, handleMCPUpdateBatch(root, "repro-version", input))
	if second.Status != autoStatusApplied || !second.Aligned || second.Attempted != 14 ||
		second.Applied != 14 || second.Remaining != 0 || !second.FormalWritesStarted || second.FindingCount != 0 {
		t.Fatalf("exactly repaired complete batch was not atomically applied: %+v", second)
	}
	for _, candidate := range input {
		if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), candidate.NewEntry) {
			t.Fatalf("applied Code Volume omitted %s", candidate.Path)
		}
	}
	paths := []string{"aoci.code.txt"}
	for _, candidate := range input {
		paths = append(paths, candidate.Path)
	}
	assertVolumeBaselineAligned(t, root, paths...)
}

func TestBatchFRASReturnsAllErrorsDeterministically(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 10)
	input[1].NewEntry = "item01.go[CD7S]: F:updated item 01 | R:- | A:a,b,c,d,e,f,g | S:-"
	input[4].NewEntry = "item04.go[CD7S]: F:updated item 04 | R:a,b,c,d,e,f,g,h,i | A:Item04 | S:-"
	input[7].NewEntry = "item07.go[CD7S]: F:" + strings.Repeat("f", 161) + " | R:- | A:Item07 | S:-"
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	second := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if first.Status != autoStatusRepairRequired || len(first.Findings) != 3 ||
		!reflect.DeepEqual(first.Findings, second.Findings) || !reflect.DeepEqual(first.RetryScope, second.RetryScope) {
		t.Fatalf("all findings were not returned in stable order: first=%+v second=%+v", first, second)
	}
	first.Metrics.DeterministicMs = 0
	second.Metrics.DeterministicMs = 0
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated repair result changed outside elapsed time: first=%+v second=%+v", first, second)
	}
}

func TestBatchFRASPreviewAndMCPShareFindings(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 14)
	input = permuteBatchFRASInput(t, input)
	input[3].NewEntry = batchFRASLine(input[3], "-", "a,b,c,d,e,f,g")
	input[10].NewEntry = batchFRASLine(input[10], "a,b,c,d,e,f,g,h,i", "Item10")
	_, previewFail := ApplyUpdateEntriesAtomic(root, atomicFRASItems(input), ledger.SourceHuman, true)
	if previewFail == nil || !previewFail.Repairable || len(previewFail.Findings) != 2 {
		t.Fatalf("batch Preview did not expose the shared finding: %+v", previewFail)
	}
	mcpResult := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if mcpResult.Status != autoStatusRepairRequired || !reflect.DeepEqual(previewFail.Findings, []cognition.RepairFinding(mcpResult.Findings)) {
		t.Fatalf("Preview and MCP findings diverged: preview=%+v mcp=%+v", previewFail.Findings, mcpResult.Findings)
	}
}

func TestBatchFRASCandidateIndexFollowsOriginalCodeOrder(t *testing.T) {
	for _, test := range []struct {
		name      string
		order     func(zCandidate, aCandidate updateEntryItemIn) []updateEntryItemIn
		wantIndex int
	}{
		{"z_then_a", func(zCandidate, aCandidate updateEntryItemIn) []updateEntryItemIn {
			return []updateEntryItemIn{zCandidate, aCandidate}
		}, 1},
		{"a_then_z", func(zCandidate, aCandidate updateEntryItemIn) []updateEntryItemIn {
			return []updateEntryItemIn{aCandidate, zCandidate}
		}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, zCandidate, aCandidate := buildNonLexicographicCodeFixture(t)
			input := test.order(zCandidate, aCandidate)
			_, fail := ApplyUpdateEntriesAtomic(root, atomicFRASItems(input), ledger.SourceHuman, true)
			if fail == nil || !fail.Repairable || len(fail.Findings) != 1 {
				t.Fatalf("non-lexicographic batch was not rejected with one repair Finding: %+v", fail)
			}
			finding := fail.Findings[0]
			if finding.Path != "z.go" || finding.CandidateIndex != test.wantIndex || finding.RuleCode != "fras_a_too_many_items" {
				t.Fatalf("candidate_index did not follow original request order: %+v", finding)
			}
		})
	}
}

func TestBatchFRASMixedDomainIndicesRemainGlobal(t *testing.T) {
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
	items := []AtomicUpdateItem{
		{ObjectRef: "database://primary/public/users", NewEntry: "users[DB9S]: F:store updated canonical user account state | R:- | A:a,b,c,d,e,f,g | S:Hard deletion remains forbidden", SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt")},
		{Path: "z.go", NewEntry: "z.go[CD7S]: F:describe updated fixture z | R:- | A:a,b,c,d,e,f,g | S:-", SourceSHA256: volumeSourceSHA(t, root, "z.go")},
		{Path: "main.go", NewEntry: volumeUpdateLine, SourceSHA256: volumeSourceSHA(t, root, "main.go")},
	}
	_, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceHuman, true)
	if fail == nil || !fail.Repairable || len(fail.Findings) != 2 {
		t.Fatalf("mixed-domain batch did not return both repair Findings: %+v", fail)
	}
	want := map[string]int{"code:z.go": 2, "database://primary/public/users": 1}
	for _, finding := range fail.Findings {
		if finding.CandidateIndex != want[finding.CanonicalObjectIdentity] {
			t.Fatalf("mixed-domain candidate_index was renumbered inside a domain: %+v", finding)
		}
	}
}

func TestBatchFRASMultipleErrorsShareOriginalCandidateIndex(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 14)
	input = permuteBatchFRASInput(t, input)
	input[3].NewEntry = batchFRASLine(input[3], "a,b,c,d,e,f,g,h,i", "a,b,c,d,e,f,g")
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if result.Status != autoStatusRepairRequired || len(result.Findings) != 2 {
		t.Fatalf("one candidate's multiple errors were not returned together: %+v", result)
	}
	for _, finding := range result.Findings {
		if finding.CandidateIndex != 4 || finding.Path != "src/item00.go" {
			t.Fatalf("one candidate's errors received different original indices: %+v", finding)
		}
	}
}

func TestDuplicateCandidateFindingKeepsOriginalDuplicatePosition(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 14)
	batch := []updateEntryItemIn{input[9], input[2], input[9]}
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", batch))
	if result.Status != autoStatusRepairRequired || len(result.Findings) != 1 ||
		result.Findings[0].CandidateIndex != 3 || result.Findings[0].CanonicalObjectIdentity != "code:src/item09.go" ||
		!reflect.DeepEqual(result.RetryScope, []string{"code:src/item09.go"}) {
		t.Fatalf("duplicate candidate did not identify its true original position: %+v", result)
	}
}

func TestBatchFRASSafetyConflictRemainsStopped(t *testing.T) {
	root, input := buildBatchFRASFixture(t, 3)
	writeVolumeTestFile(t, root, input[1].Path, "package fixture\n\nconst externallyChanged = true\n")
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if result.Status != autoStatusStopped || result.FormalWritesStarted || result.PreserveOtherCandidates || len(result.RetryScope) != 0 {
		t.Fatalf("source SHA drift was downgraded to candidate repair: %+v", result)
	}
}
