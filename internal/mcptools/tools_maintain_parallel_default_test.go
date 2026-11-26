package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// buildToAligned plays one session alone: no-argument Maintain, author the
// batch it delivers, apply, again, until aligned. It returns the rounds and
// the candidates each round carried; any refusal or stop fails the test.
func buildToAligned(t *testing.T, session *mcp.ClientSession) (rounds int, sizes []int) {
	t.Helper()
	for rounds < 200 {
		maintain := maintainVolumeBatch(t, session)
		if maintain.Aligned {
			return rounds, sizes
		}
		if maintain.Status != autoStatusRepairRequired || maintain.CodePlan == nil || len(maintain.Candidates) == 0 {
			t.Fatalf("round %d: one session alone was stopped or left without a batch: status=%s next=%s", rounds+1, maintain.Status, maintain.NextAction)
		}
		applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(maintain)))
		if applied.Status != autoStatusApplied || applied.Applied != len(maintain.Candidates) {
			t.Fatalf("round %d: the delivered batch did not apply: %#v", rounds+1, applied)
		}
		rounds++
		sizes = append(sizes, len(maintain.Candidates))
		if applied.Aligned {
			return rounds, sizes
		}
	}
	t.Fatal("the build did not converge")
	return 0, nil
}

// The default of 2026-11-19: a first build issues three sibling batches, and
// a Host that runs no subagents loses nothing by it. One session alone takes
// the first batch of every plan at full size, because siblings nobody fetched
// are released when the first applies, and reaches aligned in the rounds a
// repository pinned to one batch takes, never stopped and never refused.
func TestOneSessionAloneBuildsUnderTheDefaultInTheSameRounds(t *testing.T) {
	if machinecontract.CodeCognitionParallelBatchesDefault != 3 {
		t.Fatalf("the first-build default is three sibling batches, not %d", machinecontract.CodeCognitionParallelBatchesDefault)
	}
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || first.CodePlan.LogicalPlan == "" || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("work beyond one batch must issue three siblings by default: %#v", first.CodePlan)
	}
	if !strings.Contains(strings.Join(first.Instructions, "\n"), "aoci_maintain") {
		t.Fatalf("the response does not say what a Host without subagents does: %v", first.Instructions)
	}
	rounds, sizes := buildToAligned(t, session)

	pinned := buildManyFileVolumesRepo(t, 150)
	pinSingleBatch(t, pinned)
	pinnedRounds, pinnedSizes := buildToAligned(t, connectMCPClient(t, pinned))
	if len(sizes) < 3 || sizes[1] < pinnedSizes[1] {
		t.Fatalf("after the first plan one session alone must get the batches a pinned repository gets: %v against %v", sizes, pinnedSizes)
	}
	if rounds > pinnedRounds+1 {
		t.Fatalf("one session alone took %d rounds %v under the default, a pinned repository %d %v", rounds, sizes, pinnedRounds, pinnedSizes)
	}
	// Work that fits one batch is the single batch whatever the setting.
	small := buildManyFileVolumesRepo(t, 5)
	only := maintainVolumeBatch(t, connectMCPClient(t, small))
	if only.CodePlan == nil || only.CodePlan.LogicalPlan != "" || len(only.CodePlan.Siblings) != 0 {
		t.Fatalf("work that fits one batch must not issue siblings: %#v", only.CodePlan)
	}
}

// A sibling some session was handed keeps its plan: the first batch applying
// releases only siblings nobody fetched.
func TestAFetchedSiblingKeepsItsPlanUnderTheDefault(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("expected three siblings: %#v", first.CodePlan)
	}
	fetchedID := first.CodePlan.Siblings[0].BatchID
	var fetched volumeMaintainResult
	if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": fetchedID, "include_instructions": true})), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.CodePlan == nil || fetched.CodePlan.BatchID != fetchedID || len(fetched.Candidates) == 0 {
		t.Fatalf("the sibling was not delivered whole: %#v", fetched.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("the first sibling did not apply: %#v", applied)
	}
	next := maintainVolumeBatch(t, session)
	if next.CodePlan == nil || next.CodePlan.LogicalPlan != first.CodePlan.LogicalPlan {
		t.Fatalf("a plan with a fetched sibling was released: %#v", next.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(fetched))); applied.Status != autoStatusApplied || applied.Applied != len(fetched.Candidates) {
		t.Fatalf("the fetched sibling no longer applies: %#v", applied)
	}
}

// A released sibling asked for by batch_id sends its Agent back to Maintain
// with zero writes; the session then gets a batch like any other.
func TestAReleasedSiblingSendsItsAgentBackToMaintain(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	releasedID := first.CodePlan.Siblings[0].BatchID
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("the first sibling did not apply: %#v", applied)
	}
	// Released, and the build is one session's from here: the next batch is
	// a single one at full size, with no sibling to hand out.
	next := maintainVolumeBatch(t, session)
	if next.CodePlan == nil || next.CodePlan.LogicalPlan != "" || len(next.CodePlan.Siblings) != 0 ||
		len(next.Candidates) <= len(first.Candidates) {
		t.Fatalf("siblings nobody fetched were not released into full single batches: first=%d next=%d plan=%#v",
			len(first.Candidates), len(next.Candidates), next.CodePlan)
	}
	late := callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": releasedID})
	if !strings.Contains(late, "maintain_batch_id_unknown") {
		t.Fatalf("a released sibling must answer the replan refusal: %s", late)
	}
	if rounds, _ := buildToAligned(t, session); rounds == 0 {
		t.Fatal("the build did not continue after the late request")
	}
}

// The solo mark ends with the authoring work: a later wave beyond one batch
// is offered as siblings again.
func TestTheSoloMarkEndsWithTheAuthoringWork(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	buildToAligned(t, session)
	if _, err := os.Stat(filepath.Join(root, ".aoci", "drafts", "code-cognition", "solo-build")); err == nil {
		t.Fatal("the solo mark outlived the build")
	}
}

// The mark names the work it was made for. Work it does not name is a new
// wave, however the earlier one ended, and is offered as siblings again.
func TestTheSoloMarkHoldsOnlyForTheWorkItNamed(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("the first sibling did not apply: %#v", applied)
	}
	if solo := maintainVolumeBatch(t, session); solo.CodePlan == nil || solo.CodePlan.LogicalPlan != "" {
		t.Fatalf("the build was not marked solo: %#v", solo.CodePlan)
	}
	// The same work on another server is still this build: single batches.
	if resumed := maintainVolumeBatch(t, connectMCPClient(t, root)); resumed.CodePlan == nil || resumed.CodePlan.LogicalPlan != "" {
		t.Fatalf("the rest of a solo build must stay single batches: %#v", resumed.CodePlan)
	}
	// One source changes: work the mark never named, so siblings again.
	changed := filepath.Join(root, "pkg", "mod014", "file149.go")
	data, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, append(data, []byte("\n// a new wave\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if wave := maintainVolumeBatch(t, connectMCPClient(t, root)); wave.CodePlan == nil || wave.CodePlan.LogicalPlan == "" {
		t.Fatalf("work the mark never named must be offered as siblings: %#v", wave.CodePlan)
	}
}

func TestSoloWorkCompletedThroughHumanApplyDoesNotSuppressLaterSiblings(t *testing.T) {
	const files = 160
	root := buildManyFileVolumesRepo(t, files)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("expected three siblings: %#v", first.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied || applied.Aligned {
		t.Fatalf("expected a successful partial sibling Apply: %s aligned=%v", applied.Status, applied.Aligned)
	}
	second := maintainVolumeBatch(t, session)
	if second.CodePlan == nil || second.CodePlan.LogicalPlan != "" || second.CodePlan.Remaining == 0 {
		t.Fatalf("remaining work must begin as solo: %#v", second.CodePlan)
	}
	arguments := codeBatchArguments(wireCodePlan(second))
	soloDigest := codebatch.SoloBuildDigest(root)
	save := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected baseline save failure") }
	t.Cleanup(func() { saveAtomicBaseline = save })
	cut := applyVolumeBatch(t, session, arguments)
	saveAtomicBaseline = save
	if cut.Status != autoStatusStopped || cut.Aligned || soloDigest == "" || codebatch.SoloBuildDigest(root) != soloDigest {
		t.Fatalf("an incomplete Apply must preserve the hint for Recovery: %s aligned=%v", cut.Status, cut.Aligned)
	}
	applied := applyVolumeBatch(t, session, arguments)
	if applied.Status != autoStatusApplied || applied.Aligned {
		t.Fatalf("expected a successful partial solo Apply: %s aligned=%v", applied.Status, applied.Aligned)
	}
	if stillSolo := maintainVolumeBatch(t, connectMCPClient(t, root)); stillSolo.CodePlan == nil || stillSolo.CodePlan.LogicalPlan != "" {
		t.Fatalf("unfinished solo work must stay single across sessions: %#v", stillSolo.CodePlan)
	}

	// Finish the original build through the write path used by update-entry.
	// Read Verify's shared facts so a final Maintain cannot clear the mark
	// for the writer.
	remaining := make([]AtomicUpdateItem, 0, second.CodePlan.Remaining)
	original := make([]string, 0, second.CodePlan.Remaining)
	for i := len(first.Candidates) + len(second.Candidates); i < files; i++ {
		path := fmt.Sprintf("pkg/mod%03d/file%03d.go", i/10, i)
		remaining = append(remaining, AtomicUpdateItem{Path: path, NewEntry: fixtureEntryLine(path),
			SourceSHA256: volumeSourceSHA(t, root, path)})
		original = append(original, volumeFileText(t, root, path))
	}
	humanApply := func() *AtomicBatchOutcome {
		outcome, fail := ApplyUpdateEntriesAtomic(root, remaining, ledger.SourceHuman, false)
		if fail != nil || outcome == nil || !outcome.BaselineComplete {
			t.Fatalf("human Apply did not complete: outcome=%#v fail=%#v", outcome, fail)
		}
		return outcome
	}
	humanApply()
	if facts, _ := verifyAligned(t, root); !facts.GovernanceAligned {
		t.Fatalf("human Apply did not align the build: %s", facts.NextRequiredAction)
	}

	// Accept a different source revision without running Maintain, then
	// restore the exact bytes the old mark originally named.
	for i := range remaining {
		path := remaining[i].Path
		writeVolumeTestFile(t, root, path, original[i]+"\n// intermediate revision\n")
		remaining[i].SourceSHA256 = volumeSourceSHA(t, root, path)
	}
	if rebinding := humanApply(); !rebinding.AlreadyApplied {
		t.Fatal("unchanged Entries should only rebind the Baseline")
	}
	for i, item := range remaining {
		writeVolumeTestFile(t, root, item.Path, original[i])
	}
	wave := maintainVolumeBatch(t, connectMCPClient(t, root))
	if wave.CodePlan == nil || wave.CodePlan.LogicalPlan == "" || len(wave.CodePlan.Siblings) == 0 {
		t.Fatalf("same source bytes in a new wave must issue siblings: %#v", wave.CodePlan)
	}
	t.Logf("human Apply reached alignment, then %d restored targets issued %d siblings", len(remaining), len(wave.CodePlan.Siblings)+1)
}

func TestSoloWorkDoesNotReviveTargetsSeenAbsentOrReverted(t *testing.T) {
	for _, removed := range []bool{false, true} {
		name := "reverted"
		if removed {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			const files = 100
			root := buildManyFileVolumesRepo(t, files)
			session := connectMCPClient(t, root)
			buildToAligned(t, session)
			path := func(i int) string { return fmt.Sprintf("pkg/mod%03d/file%03d.go", i/10, i) }
			original := make([]string, files)
			for i := range files {
				original[i] = volumeFileText(t, root, path(i))
				writeVolumeTestFile(t, root, path(i), original[i]+"\n// wave\n")
			}
			first := maintainVolumeBatch(t, session)
			if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
				t.Fatalf("expected a parallel update wave: %#v", first.CodePlan)
			}
			if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
				t.Fatalf("the first sibling did not apply: %s", applied.Status)
			}
			solo := maintainVolumeBatch(t, session)
			if solo.CodePlan == nil || solo.CodePlan.LogicalPlan != "" {
				t.Fatalf("unfetched siblings did not leave a solo wave: %#v", solo.CodePlan)
			}

			for i := len(first.Candidates) + 1; i < files; i++ {
				if !removed {
					writeVolumeTestFile(t, root, path(i), original[i])
				} else if err := os.Remove(filepath.Join(root, filepath.FromSlash(path(i)))); err != nil {
					t.Fatal(err)
				}
			}
			reduced := maintainVolumeBatch(t, session)
			if len(reduced.Candidates) != 1 || reduced.CodePlan == nil || reduced.CodePlan.LogicalPlan != "" {
				t.Fatalf("reduced work must fit one batch: %d, %#v", len(reduced.Candidates), reduced.CodePlan)
			}
			for i := len(first.Candidates) + 1; i < files; i++ {
				writeVolumeTestFile(t, root, path(i), original[i]+"\n// wave\n")
			}
			wave := maintainVolumeBatch(t, session)
			if wave.CodePlan == nil || wave.CodePlan.LogicalPlan == "" || len(wave.CodePlan.Siblings) == 0 {
				t.Fatalf("targets that returned after being seen %s must issue siblings: %#v", name, wave.CodePlan)
			}
		})
	}
}

func TestFinalApplyClearsSoloWorkThatRevertedDuringTheBatch(t *testing.T) {
	const files = 100
	root := buildManyFileVolumesRepo(t, files)
	session := connectMCPClient(t, root)
	buildToAligned(t, session)
	original := map[string]string{}
	for i := range files {
		path := fmt.Sprintf("pkg/mod%03d/file%03d.go", i/10, i)
		original[path] = volumeFileText(t, root, path)
		writeVolumeTestFile(t, root, path, original[path]+"\n// wave\n")
	}
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("expected to start a parallel wave: %#v", first.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("sibling Apply failed: %s", applied.Status)
	}
	solo := maintainVolumeBatch(t, session)
	if solo.CodePlan == nil || solo.CodePlan.LogicalPlan != "" {
		t.Fatalf("expected a solo batch: %#v", solo.CodePlan)
	}
	for _, batch := range []volumeMaintainResult{first, solo} {
		for _, candidate := range batch.Candidates {
			delete(original, candidate.Path)
		}
	}
	if len(original) == 0 {
		t.Fatal("fixture must leave targets outside the issued batch")
	}
	for path, source := range original {
		writeVolumeTestFile(t, root, path, source)
	}
	applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(solo)))
	if applied.TerminalProof == nil || !applied.TerminalProof.Aligned {
		t.Fatalf("the last Apply did not prove alignment: %s, %#v", applied.Status, applied.TerminalProof)
	}
	if _, err := os.Lstat(filepath.Join(root, ".aoci", "drafts", "code-cognition", "solo-build")); !os.IsNotExist(err) {
		t.Fatalf("the aligned proof must retire the reverted remainder: %v", err)
	}
}

// An update wave whose Entries all stand as they were rebinds its sources
// and writes no Volume. Each sibling of such a wave must still be recorded
// as applied, or the plan holds it outstanding and one session is handed the
// same batch for ever (found on excalidraw, 2026-11-20).
func TestARebindOnlyWaveBeyondOneBatchConverges(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	buildToAligned(t, session)
	for index := 0; index < 150; index++ {
		path := filepath.Join(root, "pkg", fmt.Sprintf("mod%03d", index/10), fmt.Sprintf("file%03d.go", index))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n// wave\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wave := connectMCPClient(t, root)
	for round := 1; ; round++ {
		if round > 40 {
			t.Fatal("a rebind-only wave did not converge")
		}
		maintain := maintainVolumeBatch(t, wave)
		if maintain.Aligned {
			break
		}
		if len(maintain.Candidates) == 0 {
			t.Fatalf("round %d: no batch and not aligned: %s", round, maintain.Status)
		}
		entries := []map[string]any{}
		for _, candidate := range maintain.Candidates {
			if candidate.ExistingEntry == "" {
				t.Fatalf("the wave issued a create: %s", candidate.Path)
			}
			entries = append(entries, map[string]any{"path": candidate.Path, "new_entry": candidate.ExistingEntry})
		}
		applied := applyVolumeBatch(t, wave, map[string]any{"code_batch_id": maintain.CodePlan.BatchID, "entries": entries})
		if applied.Status != autoStatusApplied {
			t.Fatalf("round %d: a rebind batch did not apply: %#v", round, applied)
		}
		if applied.Aligned {
			break
		}
	}
}

// Work that fits one batch takes no lock, as Maintain never did: a lock held
// elsewhere (a killed writer's, until it is reclaimed) does not hold up an
// ordinary Maintain (scenario C2 on CI, 2026-11-20).
func TestSingleBatchMaintainTakesNoLock(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 3)
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	done := make(chan volumeMaintainResult, 1)
	go func() { done <- maintainVolumeBatch(t, connectMCPClient(t, root)) }()
	select {
	case result := <-done:
		if len(result.Candidates) == 0 {
			t.Fatalf("a single-batch Maintain issued nothing while the lock was held: %s", result.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a single-batch Maintain waited for the index lock")
	}
}

// An unlocked single-batch Maintain must never drop a plan another session
// recorded after this one looked: plan records are only removed under the lock.
func TestUnlockedMaintainKeepsPlanRecordedMeanwhile(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 3)
	record := filepath.Join(root, ".aoci", "drafts", "code-cognition", "parallel-other-session.json")
	afterPlanRecordsCheck = func(string) {
		if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(record, []byte("{}"), 0o644); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { afterPlanRecordsCheck = func(string) {} })
	result := maintainVolumeBatch(t, connectMCPClient(t, root))
	if len(result.Candidates) == 0 {
		t.Fatalf("the single-batch Maintain issued nothing: %s", result.Status)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("an unlocked Maintain removed a plan record another session wrote: %v", err)
	}
}

func TestMaintainLockFailureKeepsItsKind(t *testing.T) {
	timeout := maintainLockFailure(fmt.Errorf("held: %w", afs.ErrLockTimeout))
	if timeout.Code != errWriteConflict {
		t.Fatalf("a held lock must answer write_conflict, got %s", timeout.Code)
	}
	other := maintainLockFailure(errors.New("permission denied"))
	if other.Code == errWriteConflict || other.Hint == timeout.Hint {
		t.Fatalf("a lock that cannot be created must not read as another writer: %+v", other)
	}
}

// A plan another session recorded after this Maintain assessed the tree came
// from a newer view of it: the call must start over rather than judge (and
// drop) that plan from its older snapshot.
func TestMaintainRestartsOnPlanRecordedSinceItsAssessment(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 3)
	fired := 0
	beforePlanRecordsCheck = func(string) {
		fired++
		if fired > 1 {
			return
		}
		writeVolumeTestFile(t, root, "pkg/late/added_since.go", "package late\n\nfunc AddedSince() int { return 1 }\n")
		writeVolumeTestFile(t, root, ".aoci/drafts/code-cognition/parallel-other-session.json", "{}")
	}
	t.Cleanup(func() { beforePlanRecordsCheck = func(string) {} })
	result := maintainVolumeBatch(t, connectMCPClient(t, root))
	if fired < 2 {
		t.Fatalf("Maintain judged a plan recorded after its assessment instead of starting over (assessed %d time(s))", fired)
	}
	found := false
	for _, candidate := range result.Candidates {
		if candidate.Path == "pkg/late/added_since.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the restarted Maintain did not see the newer tree: %s", result.Status)
	}
}
