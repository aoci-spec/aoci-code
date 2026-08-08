package mcptools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

func TestSingleUpdateRejectsCorruptBaselineBeforeIndexWrite(t *testing.T) {
	root := buildRepo(t)
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, fail := ApplyUpdateEntry(root, "src/a.go", "a.go[X.Y.5.T]: F:预检 | R:- | A:- | S:-", "human", false)
	if fail == nil || fail.Code != errInternal || !strings.Contains(fail.Msg, "尚未写入") {
		t.Fatalf("损坏Baseline必须写前停止: %+v", fail)
	}
	after, _ := os.ReadFile(indexPath)
	if string(after) != string(before) {
		t.Fatal("Baseline预检失败不得修改索引")
	}
}

func TestSingleUpdateDoesNotBaselineUnexpectedPostimage(t *testing.T) {
	root := buildRepo(t)
	previousWrite := writeSingleIndex
	writeSingleIndex = func(path string, data []byte, expected string) error {
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return os.WriteFile(path, append(data, []byte("#external\n")...), 0o644)
	}
	t.Cleanup(func() { writeSingleIndex = previousWrite })
	_, fail := ApplyUpdateEntry(root, "src/a.go", "a.go[X.Y.5.T]: F:竞态 | R:- | A:- | S:-", "human", false)
	if fail == nil || fail.Code != errWriteConflict || !strings.Contains(fail.Msg, "postimage") {
		t.Fatalf("意外postimage必须报告冲突: %+v", fail)
	}
	state, exists, loadErr := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, ".aoci", "index.txt"))
	if loadErr != nil || hashErr != nil || !exists || state.Files[".aoci/index.txt"] == current {
		t.Fatalf("意外postimage必须保持Stale: exists=%v load=%v hash=%v state=%+v current=%+v", exists, loadErr, hashErr, state, current)
	}
}

func TestSingleUpdateBaselineSaveFailureReturnsErrorAndCanRetry(t *testing.T) {
	root := buildRepo(t)
	previousSave := saveSingleBaseline
	saveSingleBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected baseline failure")
	}
	t.Cleanup(func() { saveSingleBaseline = previousSave })
	entry := "a.go[X.Y.5.T]: F:恢复 | R:- | A:- | S:-"
	_, fail := ApplyUpdateEntry(root, "src/a.go", entry, "human", false)
	if fail == nil || fail.Code != errInternal || !strings.Contains(fail.Msg, "已写入") {
		t.Fatalf("Baseline失败不得报告成功: %+v", fail)
	}
	saveSingleBaseline = previousSave
	if _, retryFail := ApplyUpdateEntry(root, "src/a.go", entry, "human", false); retryFail != nil {
		t.Fatalf("同一候选重试应补齐Baseline: %+v", retryFail)
	}
}
