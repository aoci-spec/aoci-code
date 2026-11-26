package mcptools

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
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
