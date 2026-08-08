package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

func TestScanForceSharesBaselineWriteLock(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	if err := os.WriteFile(
		filepath.Join(root, "a.go"),
		[]byte("package demo\n\nvar ScanChanged = true\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- executeCLI(
			[]string{"--repo", root, "--quiet", "scan", "--force"},
			&stdout,
			&stderr,
		)
	}()
	select {
	case code := <-done:
		_ = lock.Release()
		t.Fatalf("scan --force不得绕过共享Baseline锁: code=%d", code)
	case <-time.After(150 * time.Millisecond):
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != ExitOK {
		t.Fatalf("释放共享锁后scan应成功: code=%d", code)
	}

	state, exists, err := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, "a.go"))
	if err != nil || hashErr != nil || !exists || state.Files["a.go"] != current {
		t.Fatalf("scan完成后的Baseline必须绑定当前快照: exists=%v load=%v hash=%v", exists, err, hashErr)
	}
}
