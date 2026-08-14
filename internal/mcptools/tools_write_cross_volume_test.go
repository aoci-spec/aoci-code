package mcptools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

const (
	crossCodeLine     = "main.go[CD9S]: F:run the cross-volume fixture | R:database://primary/public/users | A:main | S:Keep execution deterministic"
	crossDatabaseLine = "users[DB9S]: F:store changed canonical user account state | R:code:main.go | A:user_id,UserRepository | S:Hard deletion is forbidden because retained ownership records require the identity"
)

func crossVolumeItems(t *testing.T, root string) []AtomicUpdateItem {
	t.Helper()
	return []AtomicUpdateItem{
		{Path: "main.go", NewEntry: crossCodeLine, SourceSHA256: volumeSourceSHA(t, root, "main.go")},
		{ObjectRef: "database://primary/public/users", NewEntry: crossDatabaseLine, SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt")},
	}
}

func TestCrossVolumeCodeAndDatabaseUseOneGovernancePipeline(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	rootBefore := volumeFileText(t, root, "aoci.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	items := crossVolumeItems(t, root)
	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		t.Fatalf("cross-Volume plan failed: %+v", fail)
	}
	if plan.changeEnvelope == nil || strings.Join(plan.changeEnvelope.WriteSet, ",") != "code,database" ||
		strings.Join(plan.changeEnvelope.GuardSet, ",") != "root,meta,code,database" || plan.volumePlan == nil {
		t.Fatalf("unexpected internal impact envelope: %#v", plan.changeEnvelope)
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != 2 ||
		strings.Join(outcome.Volumes, ",") != "code,database" {
		t.Fatalf("cross-Volume apply failed: outcome=%#v fail=%+v", outcome, fail)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), crossCodeLine) ||
		!strings.Contains(volumeFileText(t, root, "aoci.database.txt"), crossDatabaseLine) {
		t.Fatal("both model-authored postimages were not committed")
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore || volumeFileText(t, root, "aoci.meta.txt") != metaBefore {
		t.Fatal("cross-Volume update modified Root or Meta")
	}
	assertVolumeBaselineAligned(t, root, "main.go", "aoci.code.txt", "aoci.database.txt")
}

func TestCrossVolumeDatabaseOnlyDoesNotModifyCode(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	item := AtomicUpdateItem{ObjectRef: "database://primary/public/users", NewEntry: crossDatabaseLine, SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt")}
	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{item})
	if fail != nil {
		t.Fatalf("Database-only plan failed: %+v", fail)
	}
	if strings.Join(plan.changeEnvelope.WriteSet, ",") != "database" || strings.Join(plan.changeEnvelope.GuardSet, ",") != "root,meta,code,database" {
		t.Fatalf("Review/Write/Guard discipline changed: %#v", plan.changeEnvelope)
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.Volume != "database" {
		t.Fatalf("Database-only apply failed: outcome=%#v fail=%+v", outcome, fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore {
		t.Fatal("Database-only cognition candidate modified Code")
	}
}

func TestCrossVolumeGuardCASConflictIsZeroWrite(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	writeVolumeTestFile(t, root, "aoci.meta.txt", volumeFileText(t, root, "aoci.meta.txt")+"# external change\n")
	_, _, fail = commitAtomicBatch(root, ledger.SourceAgent, plan, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("guard conflict was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore || volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("prewrite conflict changed a formal object Volume")
	}
}

func TestCrossVolumeUnprovenTargetPostimageConflictsBeforeOtherWrite(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	code := targetByVolume(plan.volumePlan.targets, "code")
	if code == nil {
		t.Fatal("Code target is missing from the plan")
	}
	writeVolumeTestFile(t, root, code.Path, string(code.PostRaw))
	_, _, fail = commitAtomicBatch(root, ledger.SourceAgent, plan, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("an unproven externally visible postimage bypassed target CAS: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("target conflict allowed the Database Volume write")
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || pending {
		t.Fatalf("prewrite target conflict created active recovery evidence: pending=%v err=%v", pending, err)
	}
}

func TestCrossVolumeFirstWriteFailureIsZeroFormalWrite(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(string, []byte, string) error {
		return errors.New("simulated first Volume write failure")
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail == nil || outcome != nil {
		t.Fatalf("preimage write failure was not returned as a hard stop: outcome=%#v fail=%+v", outcome, fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore || volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("first write failure changed a formal cognition Volume")
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || pending {
		t.Fatalf("zero-write failure left an active recovery receipt: pending=%v err=%v", pending, err)
	}
}

func TestCrossVolumeSecondWriteFailureRequiresIdempotentRecovery(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.AppliedCount != 0 || !strings.Contains(outcome.BaselineNote, "recovery_required") {
		t.Fatalf("partial write was reported incorrectly: outcome=%#v fail=%+v", outcome, fail)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), crossCodeLine) || strings.Contains(volumeFileText(t, root, "aoci.database.txt"), crossDatabaseLine) {
		t.Fatal("fault injection did not create the expected recoverable partial state")
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || !pending {
		t.Fatalf("partial state lacks existing recovery evidence: pending=%v err=%v", pending, err)
	}
	recovered, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || recovered.AppliedCount != 2 {
		t.Fatalf("same candidate did not roll forward safely: outcome=%#v fail=%+v", recovered, fail)
	}
	pending, err = UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || pending {
		t.Fatalf("completed recovery remained active: pending=%v err=%v", pending, err)
	}
	repeated, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || repeated == nil || !repeated.BaselineComplete || !repeated.AlreadyApplied || repeated.AppliedCount != 0 {
		t.Fatalf("completed recovery was not idempotent: outcome=%#v fail=%+v", repeated, fail)
	}
}

func TestCrossVolumeIntermediatePostimageIsHiddenFromOrdinaryReads(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	var searchResult *mcp.CallToolResult
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if err := original(target, data, expected); err != nil {
			return err
		}
		if filepath.Base(target) == "aoci.code.txt" {
			searchResult = handleSearch(root, "test-version", searchIn{Keyword: "cross-volume"}, nil)
		}
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return nil
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || outcome == nil || outcome.BaselineComplete {
		t.Fatalf("partial fixture was not created: outcome=%#v fail=%+v", outcome, fail)
	}
	if searchResult == nil || !searchResult.IsError || !strings.Contains(resText(t, searchResult), errCognitionSnapshotUnavailable) {
		t.Fatalf("ordinary read observed an intermediate CognitionSet: %#v", searchResult)
	}
}

func TestCrossVolumePartialWriteSupportsExactPolicyRollback(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	codeBefore := []byte(volumeFileText(t, root, "aoci.code.txt"))
	databaseBefore := []byte(volumeFileText(t, root, "aoci.database.txt"))
	baselineBefore, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	if outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false); fail != nil || outcome == nil || outcome.BaselineComplete {
		t.Fatalf("partial rollback fixture was not created: outcome=%#v fail=%+v", outcome, fail)
	}
	writeAtomicIndex = original
	if err := RollbackUpdateEntriesAtomicRecovery(root, items); err != nil {
		t.Fatalf("exact policy rollback failed: %v", err)
	}
	codeAfter, _ := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	databaseAfter, _ := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
	baselineAfter, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if string(codeAfter) != string(codeBefore) || string(databaseAfter) != string(databaseBefore) || string(baselineAfter) != string(baselineBefore) {
		t.Fatal("policy rollback did not restore exact formal preimages")
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || pending {
		t.Fatalf("exact rollback retained an active recovery receipt: pending=%t err=%v", pending, err)
	}
	if outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false); fail != nil || outcome == nil || !outcome.BaselineComplete {
		t.Fatalf("fresh Apply after rollback did not converge: outcome=%#v fail=%+v", outcome, fail)
	}
}

func TestCrossVolumeReceiptFailureResumesSameTransaction(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := saveVolumeGovernanceReceipt
	t.Cleanup(func() { saveVolumeGovernanceReceipt = original })
	failed := false
	saveVolumeGovernanceReceipt = func(root string, plan *atomicBatchPlan, recovery *atomicBatchRecovery) (string, error) {
		if !failed {
			failed = true
			return "", errors.New("simulated governance Receipt failure")
		}
		return original(root, plan, recovery)
	}
	first, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || first == nil || first.BaselineComplete {
		t.Fatalf("Receipt failure was reported as complete: outcome=%#v fail=%+v", first, fail)
	}
	second, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || second == nil || !second.BaselineComplete || !second.AlreadyApplied {
		t.Fatalf("same transaction did not resume after Receipt failure: outcome=%#v fail=%+v", second, fail)
	}
}

func TestCrossVolumeLedgerFailureResumesSameTransaction(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := ensureVolumeLedger
	t.Cleanup(func() { ensureVolumeLedger = original })
	failed := false
	ensureVolumeLedger = func(root string, enabled bool, event ledger.Event) error {
		if !failed {
			failed = true
			return errors.New("simulated Ledger failure")
		}
		return original(root, enabled, event)
	}
	first, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || first == nil || first.BaselineComplete {
		t.Fatalf("Ledger failure was reported as complete: outcome=%#v fail=%+v", first, fail)
	}
	second, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || second == nil || !second.BaselineComplete || !second.AlreadyApplied {
		t.Fatalf("same transaction did not resume after Ledger failure: outcome=%#v fail=%+v", second, fail)
	}
}

func TestTwoConcurrentCrossVolumeBatchesSerializeWithoutMixedPostimage(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	first := crossVolumeItems(t, root)
	second := []AtomicUpdateItem{
		{Path: "main.go", NewEntry: "main.go[CD9S]: F:run the alternative concurrent fixture | R:database://primary/public/users | A:main | S:Keep execution deterministic",
			SourceSHA256: first[0].SourceSHA256},
		{ObjectRef: "database://primary/public/users",
			NewEntry:     "users[DB9S]: F:store alternative concurrent user account state | R:code:main.go | A:user_id,UserRepository | S:Hard deletion is forbidden because retained ownership records require the identity",
			SourceSHA256: first[1].SourceSHA256},
	}
	start := make(chan struct{})
	type result struct {
		outcome *AtomicBatchOutcome
		fail    *Fail
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, items := range [][]AtomicUpdateItem{first, second} {
		items := items
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
			results <- result{outcome: outcome, fail: fail}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for current := range results {
		if current.fail == nil && current.outcome != nil && current.outcome.BaselineComplete {
			succeeded++
		} else {
			rejected++
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent batches did not serialize: succeeded=%d rejected=%d", succeeded, rejected)
	}
	code := volumeFileText(t, root, "aoci.code.txt")
	database := volumeFileText(t, root, "aoci.database.txt")
	firstVisible := strings.Contains(code, crossCodeLine) && strings.Contains(database, crossDatabaseLine)
	secondVisible := strings.Contains(code, second[0].NewEntry) && strings.Contains(database, second[1].NewEntry)
	if firstVisible == secondVisible {
		t.Fatalf("concurrent transaction exposed a mixed or ambiguous postimage: first=%t second=%t", firstVisible, secondVisible)
	}
}

func TestCrossVolumeRecoveryNeverOverwritesThirdPartyTargetState(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	if outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false); fail != nil || outcome == nil || outcome.BaselineComplete {
		t.Fatalf("partial fixture was not created: outcome=%#v fail=%+v", outcome, fail)
	}
	writeAtomicIndex = original
	thirdParty := cognition.DatabaseMarker + "\n===Primary tables/database://primary/public/===\n" +
		"users[DB9S]: F:store independently changed user state | R:- | A:user_id | S:Hard deletion is forbidden because retained ownership records require the identity\n"
	writeVolumeTestFile(t, root, "aoci.database.txt", thirdParty)

	_, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("third-party target state was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.database.txt") != thirdParty {
		t.Fatal("recovery overwrote unproven third-party Database content")
	}
}

func TestCrossVolumeRecoveryNeverOverwritesThirdPartyCodeState(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	if outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false); fail != nil || outcome == nil || outcome.BaselineComplete {
		t.Fatalf("partial fixture was not created: outcome=%#v fail=%+v", outcome, fail)
	}
	writeAtomicIndex = original
	thirdParty := cognition.CodeVolumeMarker + "\n===Main" + filepath.ToSlash(root) + "/===\n" +
		"main.go[CD9S]: F:run independently changed source cognition | R:- | A:main | S:Keep execution deterministic\n"
	writeVolumeTestFile(t, root, "aoci.code.txt", thirdParty)

	_, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("third-party Code state was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != thirdParty {
		t.Fatal("recovery overwrote unproven third-party Code content")
	}
}

func TestCrossVolumeGuardRequiresRegularRootAndMetaFiles(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	metaPath := filepath.Join(root, "aoci.meta.txt")
	if err := os.Remove(metaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("aoci.txt", metaPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, _, fail = commitAtomicBatch(root, ledger.SourceAgent, plan, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("non-regular Meta guard was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore ||
		volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("non-regular guard failure changed object Volumes")
	}
}

func TestCrossVolumeBaselineFailureRetainsRecoveryEvidence(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := saveAtomicBaseline
	t.Cleanup(func() { saveAtomicBaseline = original })
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("simulated Baseline failure") }
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	saveAtomicBaseline = original
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.AppliedCount != 0 {
		t.Fatalf("Baseline failure was reported incorrectly: outcome=%#v fail=%+v", outcome, fail)
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || !pending {
		t.Fatalf("Baseline failure discarded recovery evidence: pending=%v err=%v", pending, err)
	}
	recovered, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || !recovered.AlreadyApplied {
		t.Fatalf("Baseline recovery did not converge: outcome=%#v fail=%+v", recovered, fail)
	}
}

// 跨卷指向一个不存在的对象只是模型的语义选择, 写入照常完成, 而且只动它自己
// 那个卷 —— 关系既不阻断写入, 也不把别的卷卷进来。
func TestCrossVolumeDanglingRelationWritesOnlyItsOwnVolume(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	line := "users[DB9S]: F:store changed user state | R:code:missing.go | A:user_id | S:-"
	result, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		ObjectRef: "database://primary/public/users", NewEntry: line, SourceSHA256: volumeSourceSHA(t, root, "aoci.database.txt"),
	}}, ledger.SourceAgent, false)
	if fail != nil {
		t.Fatalf("悬空关系不应阻断写入: %+v", fail)
	}
	if result == nil || result.AppliedCount != 1 {
		t.Fatalf("写入未生效: %+v", result)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore {
		t.Fatal("关系把无关的卷改了")
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.database.txt"), "R:code:missing.go") {
		t.Fatal("模型写下的关系没有被原样保留")
	}
}

func TestMCPCrossVolumeSuccessRemainsSimple(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{"entries": []any{
		map[string]any{"path": items[0].Path, "new_entry": items[0].NewEntry, "source_sha256": items[0].SourceSHA256},
		map[string]any{"object_ref": items[1].ObjectRef, "new_entry": items[1].NewEntry, "source_sha256": items[1].SourceSHA256},
	}})
	for _, want := range []string{`"status":"applied"`, `"aligned":true`, `"applied":2`, `"volumes":["code","database"]`} {
		if !strings.Contains(output, want) {
			t.Fatalf("cross-Volume result missing %q:\n%s", want, output)
		}
	}
	lower := strings.ToLower(output)
	for _, forbidden := range []string{"guard_set", "change_envelope", "participant", "recovery journal", "\"cas\""} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("Agent result leaked internal %q:\n%s", forbidden, output)
		}
	}
}

func TestMCPCrossVolumeRecoveryRequiredRemainsSimple(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{"entries": []any{
		map[string]any{"path": items[0].Path, "new_entry": items[0].NewEntry, "source_sha256": items[0].SourceSHA256},
		map[string]any{"object_ref": items[1].ObjectRef, "new_entry": items[1].NewEntry, "source_sha256": items[1].SourceSHA256},
	}})
	writeAtomicIndex = original
	for _, want := range []string{`"status":"stopped"`, `"aligned":false`, `"applied":0`, `cross_volume_recovery_required`} {
		if !strings.Contains(output, want) {
			t.Fatalf("recovery-required result missing %q:\n%s", want, output)
		}
	}
	lower := strings.ToLower(output)
	for _, forbidden := range []string{"guard_set", "change_envelope", "participant", "recovery journal", "transaction", "commit", `"cas"`} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("recovery-required result leaked internal %q:\n%s", forbidden, output)
		}
	}
}

func TestDatabaseOnlyVolumeUpdateWorksWhenCodeIsAbsent(t *testing.T) {
	root := buildVolumeRepo(t, false, true)
	writeVolumeTestFile(t, root, "aoci.database.txt",
		cognition.DatabaseMarker+"\n===Primary tables/database://primary/public/===\n"+
			"users[DB9S]: F:store canonical user account state | R:- | A:user_id | S:Hard deletion is forbidden because retained ownership records require the identity\n")
	line := "users[DB9S]: F:store changed canonical user account state | R:- | A:user_id | S:Hard deletion is forbidden because retained ownership records require the identity"
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"object_ref":    "database://primary/public/users",
		"new_entry":     line,
		"source_sha256": volumeSourceSHA(t, root, "aoci.database.txt"),
	})
	for _, want := range []string{`"status":"applied"`, `"aligned":true`, `"applied":1`, `"volume":"database"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("Database-only result missing %q:\n%s", want, output)
		}
	}
}

func TestCrossVolumeRecoveryV2RejectsUnsafeOrDuplicateAssets(t *testing.T) {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	batchKey := strings.Repeat("c", 64)
	valid := atomicBatchRecovery{
		Version: 2, BatchKey: batchKey, PreIndexSHA256: hashA, PostIndexSHA256: hashB,
		Assets: []atomicBatchRecoveryAsset{{VolumeID: "code", Path: "aoci.code.txt", PreSHA256: hashA, PostSHA256: hashB}},
	}
	encode := func(value atomicBatchRecovery) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if _, err := decodeAtomicBatchRecovery(encode(valid), batchKey); err != nil {
		t.Fatalf("valid v2 recovery receipt was rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*atomicBatchRecovery)
	}{
		{"absolute_path", func(value *atomicBatchRecovery) { value.Assets[0].Path = "/aoci.code.txt" }},
		{"equal_images", func(value *atomicBatchRecovery) { value.Assets[0].PostSHA256 = hashA }},
		{"duplicate_volume", func(value *atomicBatchRecovery) {
			value.Assets = append(value.Assets, atomicBatchRecoveryAsset{VolumeID: "code", Path: "other.txt", PreSHA256: hashA, PostSHA256: hashB})
		}},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			candidate := valid
			candidate.Assets = append([]atomicBatchRecoveryAsset{}, valid.Assets...)
			current.mutate(&candidate)
			if _, err := decodeAtomicBatchRecovery(encode(candidate), batchKey); err == nil {
				t.Fatal("invalid v2 recovery receipt did not fail closed")
			}
		})
	}
}

func assertVolumeBaselineAligned(t *testing.T, root string, paths ...string) {
	t.Helper()
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state == nil {
		t.Fatalf("Baseline unavailable: exists=%v err=%v", exists, err)
	}
	for _, rel := range paths {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		stored, ok := state.Files[rel]
		if err != nil || !ok || stored.SHA256 != current.SHA256 {
			t.Fatalf("Baseline is not aligned for %s: current=%#v stored=%#v err=%v", rel, current, stored, err)
		}
	}
}

func TestCrossVolumeWriteDoesNotCreateUnexpectedRootAssets(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	items := crossVolumeItems(t, root)
	if _, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false); fail != nil {
		t.Fatal(fail.Msg)
	}
	for _, name := range []string{"aoci.api.txt", "aoci.deploy.txt", "aoci.security.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected cognition asset created: %s", name)
		}
	}
}
