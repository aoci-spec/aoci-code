package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// A file-size limit fails the actual marker staging write while leaving
// unlink available. The Code receipts and formal writes fit under the limit.
func limitTestFileSize(t *testing.T, bytes uint64, run func()) {
	t.Helper()
	var original unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	if original.Cur < bytes {
		t.Skipf("existing file-size limit is below %d bytes", bytes)
	}
	if !signal.Ignored(unix.SIGXFSZ) {
		signal.Ignore(unix.SIGXFSZ)
		defer signal.Reset(unix.SIGXFSZ)
	}
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: bytes, Max: original.Max}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &original); err != nil {
			t.Fatal(err)
		}
	}()
	run()
}

func TestSoloMarkSurvivesFailedShrink(t *testing.T) {
	for _, stage := range []string{"Maintain", "Apply", "Recovery"} {
		t.Run(stage, func(t *testing.T) {
			root, leader, solo, mark := beginSoloBuild(t, 160, 10)
			const relativeMark = ".aoci/drafts/code-cognition/solo-build"
			original := volumeFileText(t, root, relativeMark)
			assertMark := func() {
				t.Helper()
				data, err := os.ReadFile(mark)
				if err != nil || string(data) != original {
					t.Fatalf("failed %s rewrite did not preserve the old mark (%d bytes): %v", stage, len(original), err)
				}
			}
			arguments := codeBatchArguments(wireCodePlan(solo))
			if stage == "Maintain" {
				const departed = "pkg/mod015/file159.go"
				for _, candidate := range solo.Candidates {
					if candidate.Path == departed {
						t.Fatal("fixture must remove a path outside the in-flight batch")
					}
				}
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(departed))); err != nil {
					t.Fatal(err)
				}
				other := connectMCPClient(t, root)
				limitTestFileSize(t, 6<<10, func() {
					requireSoloBatch(t, maintainVolumeBatch(t, other))
				})
				assertMark()
			}
			if stage != "Maintain" {
				// Leave enough previously named work in the mark to exceed the
				// limit after retiring the applied prefix. This is an overwide
				// advisory file in a repository with a small formal write set.
				var padding strings.Builder
				for i := range 4096 {
					fmt.Fprintf(&padding, "%064x pkg/settled%04d.go\n", i, i)
				}
				original += padding.String()
				if err := os.WriteFile(mark, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "Recovery" {
				save := saveAtomicBaseline
				saveAtomicBaseline = func(string, *baseline.Baseline) error { return errors.New("injected baseline save failure") }
				t.Cleanup(func() { saveAtomicBaseline = save })
				cut := applyVolumeBatch(t, leader, arguments)
				saveAtomicBaseline = save
				pending, err := cognitiontxn.Pending(root)
				if cut.Status != autoStatusStopped || err != nil || len(pending) != 1 {
					t.Fatalf("expected an active Recovery, got status=%s pending=%d err=%v", cut.Status, len(pending), err)
				}
				assertMark()
			}
			apply := func() {
				result := applyVolumeBatch(t, leader, arguments)
				if result.Status != autoStatusApplied || result.Aligned {
					t.Fatalf("the in-flight batch must complete with work left, got status=%s aligned=%v", result.Status, result.Aligned)
				}
				t.Logf("%s fixture Apply status=%s applied=%d", stage, result.Status, result.Applied)
			}
			if stage == "Maintain" {
				apply()
			} else {
				limitTestFileSize(t, 128<<10, apply)
				assertMark()
			}
			if pending, err := cognitiontxn.Pending(root); err != nil || len(pending) != 0 {
				t.Fatalf("formal Apply left a pending transaction: %v %v", pending, err)
			}
			requireSoloBatch(t, maintainVolumeBatch(t, connectMCPClient(t, root)))
		})
	}
}

func TestMaintainReloadsBaselineAfterSoloMarkRewriteFails(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		name := "marker_write_failure"
		if readOnly {
			name = "receipt_directory_unwritable"
		}
		t.Run(name, func(t *testing.T) {
			const files = 160
			root := buildManyFileVolumesRepo(t, files)
			leader := connectMCPClient(t, root)
			buildToAligned(t, leader)
			setBatchEntries(t, root, 10)
			for i := range files {
				path := fmt.Sprintf("pkg/mod%03d/file%03d.go", i/10, i)
				writeVolumeTestFile(t, root, path, volumeFileText(t, root, path)+"\n// Rebind wave.\n")
			}
			first := maintainVolumeBatch(t, leader)
			if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
				t.Fatalf("fixture needs a parallel lead: %#v", first.CodePlan)
			}
			if applied := applyVolumeBatch(t, leader, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
				t.Fatalf("lead Apply failed: %s", applied.Status)
			}
			solo := maintainVolumeBatch(t, leader)
			requireSoloBatch(t, solo)
			settled := solo.Candidates[0]
			if settled.ExistingEntry == "" {
				t.Fatal("fixture must rebind an existing Entry")
			}
			const mark = ".aoci/drafts/code-cognition/solo-build"
			const baselineFile = ".aoci/baseline.json"
			if !readOnly {
				// Keep the advisory mark above the limit even after retiring one
				// target, while the actual Baseline can still be saved.
				var padding strings.Builder
				padding.WriteString(volumeFileText(t, root, mark))
				for i := range 4096 {
					fmt.Fprintf(&padding, "%064x pkg/departed%04d.go\n", i, i)
				}
				writeVolumeTestFile(t, root, mark, padding.String())
			}

			markBefore := volumeFileText(t, root, mark)
			codeBefore := volumeFileText(t, root, "aoci.code.txt")
			baselineBefore := volumeFileText(t, root, baselineFile)
			follower := connectMCPClient(t, root)
			assessed, continueMaintain := make(chan struct{}), make(chan struct{})
			resume := sync.OnceFunc(func() { close(continueMaintain) })
			assessments := 0
			originalHook := beforePlanRecordsCheck
			beforePlanRecordsCheck = func(string) {
				assessments++
				if assessments == 1 {
					close(assessed)
					<-continueMaintain
				}
			}
			response, done := make(chan string, 1), make(chan struct{})
			go func() {
				defer close(done)
				response <- callVolumeToolQuiet(follower, "aoci_maintain", nil)
			}()
			defer func() {
				resume()
				<-done
				beforePlanRecordsCheck = originalHook
			}()
			select {
			case <-assessed:
			case <-time.After(10 * time.Second):
				t.Fatal("Maintain did not reach the pre-lock hook")
			}

			rebind := func() {
				t.Helper()
				outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
					Path: settled.Path, NewEntry: settled.ExistingEntry, SourceSHA256: settled.SourceSHA256,
				}}, ledger.SourceHuman, false)
				if fail != nil || outcome == nil || !outcome.AlreadyApplied || !outcome.BaselineComplete {
					t.Fatalf("Baseline-only rebind failed: outcome=%#v fail=%#v", outcome, fail)
				}
			}
			if readOnly {
				dir := filepath.Dir(filepath.Join(root, mark))
				if err := os.Chmod(dir, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
				if probe, err := os.CreateTemp(dir, "writability-*"); err == nil {
					_ = probe.Close()
					_ = os.Remove(probe.Name())
					t.Skip("filesystem allows writes to a read-only directory")
				}
				rebind()
			} else {
				limitTestFileSize(t, 128<<10, rebind)
			}
			if volumeFileText(t, root, baselineFile) == baselineBefore || volumeFileText(t, root, "aoci.code.txt") != codeBefore || volumeFileText(t, root, mark) != markBefore {
				t.Fatal("the fixture must change only the Baseline and preserve the mark after the failed write")
			}
			resume()
			raw := <-response
			if readOnly {
				if !strings.Contains(raw, "code_candidate_receipt_write_failed") {
					t.Fatalf("expected the existing receipt write error, got %.350s", raw)
				}
			} else {
				var batch volumeMaintainResult
				if err := json.Unmarshal([]byte(raw), &batch); err != nil {
					t.Fatalf("decode Maintain: %v, %.350s", err, raw)
				}
				for _, candidate := range batch.Candidates {
					if candidate.Path == settled.Path {
						t.Fatalf("Maintain reissued settled target %s after Baseline advanced", settled.Path)
					}
				}
				requireSoloBatch(t, batch)
				if batch.Batch.TotalTargets != solo.Batch.TotalTargets-1 || batch.CodePlan.BatchID == solo.CodePlan.BatchID {
					t.Fatalf("expected a fresh batch over %d targets, got %d", solo.Batch.TotalTargets-1, batch.Batch.TotalTargets)
				}
				t.Logf("reloaded work %d -> %d targets, settled target omitted", solo.Batch.TotalTargets, batch.Batch.TotalTargets)
			}
			if assessments != 2 {
				t.Fatalf("expected exactly one reload, got %d assessments", assessments)
			}
			t.Logf("Baseline advanced, Code and solo mark stayed byte-identical, assessments=%d, settled=%s", assessments, settled.Path)
		})
	}
}
