package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func writeMaintainRebindWave(t *testing.T, root string, count int) []map[string]any {
	t.Helper()
	args := make([]map[string]any, 0, count)
	for i := range count {
		path := fmt.Sprintf("pkg/mod%03d/file%03d.go", i/10, i)
		writeVolumeTestFile(t, root, path, volumeFileText(t, root, path)+"\n// Fresh source binding.\n")
		args = append(args, map[string]any{"path": path, "source_sha256": volumeSourceSHA(t, root, path),
			"new_entry": filepath.Base(path) + "[CD5S]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic"})
	}
	return args
}

func TestMaintainByIDReloadsTeamBudgetUnderIndexLock(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 160)
	setMaintainBudget(t, root, 64<<10)
	leader := connectMCPClient(t, root)
	buildToAligned(t, leader)
	writeMaintainRebindWave(t, root, 160)
	first := maintainVolumeBatch(t, leader)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 || len(first.Candidates) != 50 {
		t.Fatalf("fixture needs three 50-entry siblings, got %+v", first.CodePlan)
	}
	unclaimed := first.CodePlan.Siblings[1]
	beforeCode := volumeFileText(t, root, "aoci.code.txt")
	beforeBaseline := volumeFileText(t, root, ".aoci/baseline.json")
	follower := connectMCPClient(t, root)
	waiting, resume := make(chan struct{}), make(chan struct{})
	previousHook := beforeMaintainIndexLock
	beforeMaintainIndexLock = func(string) { close(waiting); <-resume }
	answer := make(chan string, 1)
	go func() {
		answer <- callVolumeToolQuiet(follower, "aoci_maintain", map[string]any{"batch_id": unclaimed.BatchID})
	}()
	defer func() {
		if resume != nil {
			close(resume)
			<-answer
		}
		beforeMaintainIndexLock = previousHook
	}()
	select {
	case <-waiting:
	case <-time.After(15 * time.Second):
		t.Fatal("sibling delivery never reached the lock")
	}
	applied := applyVolumeBatch(t, leader, codeBatchArguments(wireCodePlan(first)))
	if applied.Status != autoStatusApplied || volumeFileText(t, root, "aoci.code.txt") != beforeCode ||
		volumeFileText(t, root, ".aoci/baseline.json") == beforeBaseline {
		t.Fatalf("competing Apply must move only the Baseline, got %+v", applied)
	}
	setMaintainBudget(t, root, 16<<10)
	close(resume)
	raw := <-answer
	resume = nil
	var delivered volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.CodePlan == nil || delivered.CodePlan.BatchID == unclaimed.BatchID ||
		delivered.Batch.TransportBudgetBytes != 16<<10 || len(raw) > 16<<10 ||
		len(delivered.Candidates) == 0 || len(delivered.Candidates) >= unclaimed.Included {
		t.Fatalf("first delivery should recut under the new budget: %d bytes, %d of %d candidates, budget=%d", len(raw), len(delivered.Candidates), unclaimed.Included, delivered.Batch.TransportBudgetBytes)
	}
	t.Logf("first delivery recut %d to %d entries using %d/%d bytes", unclaimed.Included, len(delivered.Candidates), len(raw), delivered.Batch.TransportBudgetBytes)
	if applied := applyVolumeBatch(t, follower, codeBatchArguments(wireCodePlan(delivered))); applied.Status != autoStatusApplied {
		t.Fatalf("the recut binding must Apply: %+v", applied)
	}
}

func TestMaintainRechecksMalformedBaselineBeforeOfferingRestore(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	refreshVolumeBaseline(t, root)
	const path = ".aoci/baseline.json"
	original := volumeFileText(t, root, path)
	writeVolumeTestFile(t, root, path, "<<<<<<< HEAD\n{}\n=======\n{}\n>>>>>>> other\n")
	previousHook := beforeMaintainIndexLock
	defer func() { beforeMaintainIndexLock = previousHook }()
	calls := 0
	beforeMaintainIndexLock = func(string) {
		if calls == 0 {
			if err := os.WriteFile(filepath.Join(root, path), []byte(original), 0o644); err != nil {
				t.Error(err)
			}
		}
		calls++
	}
	result := maintainVolumeBatch(t, connectMCPClient(t, root))
	if !result.Aligned || result.NextAction != "none" || calls != 1 || volumeFileText(t, root, path) != original {
		t.Fatalf("replaced malformed Baseline must be reassessed, got status=%s next=%s lock checks=%d", result.Status, result.NextAction, calls)
	}
}

func TestMaintainReloadExhaustionReturnsRetryAction(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 25)
	writer, waiting := connectMCPClient(t, root), connectMCPClient(t, root)
	buildToAligned(t, writer)
	setBatchEntries(t, root, 3)
	rebinds := writeMaintainRebindWave(t, root, 25)
	formal := map[string]string{}
	for _, asset := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		formal[asset] = volumeFileText(t, root, asset)
	}
	previousHook := beforePlanRecordsCheck
	defer func() { beforePlanRecordsCheck = previousHook }()
	attempts := 0
	lastBaseline := volumeFileText(t, root, ".aoci/baseline.json")
	beforePlanRecordsCheck = func(string) {
		if attempts >= 4 {
			return
		}
		raw := callVolumeToolQuiet(writer, "aoci_update_entry", rebinds[attempts])
		var applied autoResult
		if err := json.Unmarshal([]byte(raw), &applied); err != nil || applied.Status != autoStatusApplied {
			t.Errorf("competing Apply %d failed: %v %s", attempts+1, err, raw)
		}
		content, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
		if err != nil || string(content) == lastBaseline {
			t.Errorf("competing Apply %d did not advance the Baseline: %v", attempts+1, err)
		}
		lastBaseline = string(content)
		attempts++
	}
	// Capture the runtime state after the last competing write, before the
	// paused reader resumes. The advisory fingerprint cache can be refreshed.
	var finalRuntime map[string]string
	previousAfter := afterPlanRecordsCheck
	afterPlanRecordsCheck = func(string) {
		if attempts == 4 {
			finalRuntime = maintainRuntimeFiles(t, root)
		}
	}
	defer func() { afterPlanRecordsCheck = previousAfter }()
	answer, err := waiting.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_maintain"})
	if err != nil {
		t.Fatal(err)
	}
	if !answer.IsError || attempts != 4 || !strings.Contains(resText(t, answer), "[write_conflict]") {
		t.Fatalf("expected the bounded write conflict after 4 attempts, got %d: %+v", attempts, answer)
	}
	for path, expected := range formal {
		if volumeFileText(t, root, path) != expected {
			t.Fatalf("stopped Maintain changed %s", path)
		}
	}
	if volumeFileText(t, root, ".aoci/baseline.json") != lastBaseline || !reflect.DeepEqual(maintainRuntimeFiles(t, root), finalRuntime) {
		t.Fatal("stopped Maintain must leave formal assets, plan records and Ledger untouched")
	}
	structured, ok := answer.StructuredContent.(map[string]any)
	if !ok || structured["next_action"] != machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch ||
		structured["status"] != autoStatusStopped || structured["error_code"] != errWriteConflict ||
		structured["reason_code"] != machinecontract.MaintainReasonSnapshotChanged || structured["formal_writes_started"] != false {
		t.Fatalf("write conflict must carry a structured retry action and write status: %s, data=%v", resText(t, answer), answer.StructuredContent)
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	afterPlanRecordsCheck = previousAfter
	if batch := maintainVolumeBatch(t, waiting); len(batch.Candidates) == 0 {
		t.Fatalf("no-argument retry must progress, got %s", batch.NextAction)
	}
	t.Logf("stopped after %d Baseline-only changes, then retried using %s", attempts, structured["next_action"])
}

func maintainRuntimeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	base := filepath.Join(root, ".aoci")
	files := map[string]string{}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || !d.Type().IsRegular() || d.Name() == "fingerprints.json" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[path] = string(data)
		}
		return err
	})
	if err != nil {
		t.Error(err)
	}
	return files
}

func TestMaintainBudgetRetryPreservesOwnSoloAssessment(t *testing.T) {
	// Each call's first cut reserves scale evidence at the width the line
	// cache can vouch for, and the cache trusts only a hash and line count
	// taken more than two seconds after the file's mtime. A slow run (race,
	// few CPUs) crosses that window between the two calls and starts the
	// replay one candidate wider, so the cache is off here: both calls reserve
	// the widest width and the count below compares lock checks, not cache age.
	t.Setenv(baseline.FingerprintCacheEnv, "off")
	root, _, solo, markPath := beginSoloBuild(t, 160, 50)
	// Add recorded Entries for sources already gone. Long orphan paths occupy
	// mandatory response bytes, which the initial candidate cut cannot see.
	const orphans = 44
	setMaintainBudget(t, root, 16<<10)
	code := volumeFileText(t, root, "aoci.code.txt")
	dir := strings.Repeat("long"+strings.Repeat("p", 90)+"/", 6)
	code += "\n===Orphans" + filepath.ToSlash(root) + "/" + dir + "===\n"
	for i := range orphans {
		code += fmt.Sprintf("old%02d.go[CD5S]: F:former fixture source | R:- | A:- | S:-\n", i)
	}
	writeVolumeTestFile(t, root, "aoci.code.txt", code)
	bl, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bl.Files["aoci.code.txt"] = baseline.HashBytes("aoci.code.txt", []byte(code))
	if err := baseline.Save(root, bl); err != nil {
		t.Fatal(err)
	}
	departed := "pkg/mod015/file159.go"
	if err := os.Remove(filepath.Join(root, departed)); err != nil {
		t.Fatal(err)
	}
	oldMark, err := os.ReadFile(markPath)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := beforeMaintainIndexLock
	defer func() { beforeMaintainIndexLock = previousHook }()
	attempts := 0
	beforeMaintainIndexLock = func(string) { attempts++ }
	batch := maintainVolumeBatch(t, connectMCPClient(t, root))
	requireSoloBatch(t, batch)
	newMark, err := os.ReadFile(markPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.OrphanRemovals) != orphans || len(batch.Candidates) >= len(solo.Candidates) ||
		!strings.Contains(string(oldMark), departed) || strings.Contains(string(newMark), departed) || batch.CodePlan.BatchID == solo.CodePlan.BatchID {
		t.Fatalf("expected a smaller solo batch alongside %d orphans and a retired target, got %d candidates and %d orphans", orphans, len(batch.Candidates), len(batch.OrphanRemovals))
	}
	// Each lock check follows a budget cut. Rechecking the exact same cut
	// would mean this request counted its own mark update as outside drift.
	firstChecks := attempts
	attempts = 0
	replayed := maintainVolumeBatch(t, connectMCPClient(t, root))
	if attempts < 2 || firstChecks != attempts || replayed.CodePlan == nil || replayed.CodePlan.BatchID != batch.CodePlan.BatchID {
		t.Fatalf("own solo change must not add a lock check over the same budget cuts: first=%d replay=%d, batch=%d entries budget=%d first orphan bytes=%d",
			firstChecks, attempts, len(batch.Candidates), batch.Batch.TransportBudgetBytes, len(batch.OrphanRemovals[0]))
	}
	t.Logf("cut solo batch from %d to %d candidates beside %d removals, with %d lock checks", len(solo.Candidates), len(batch.Candidates), orphans, firstChecks)
}
