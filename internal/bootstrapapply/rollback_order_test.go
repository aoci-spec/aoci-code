package bootstrapapply

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A rollback cut after any of its steps leaves a state the next Rollback
// closes: it reverses the exact write order, Baseline first, so the Root is
// never restored ahead of a Baseline postimage (audit G1-03).
func TestBootstrapRollbackCutAtEveryStepStillCloses(t *testing.T) {
	for _, cut := range []string{"after_rollback_baseline", "after_rollback_root", "after_rollback_code", "after_rollback_meta"} {
		t.Run(cut, func(t *testing.T) {
			root := t.TempDir()
			writeBootstrapFile(t, root, "main.go", "package main\n")
			envelope, approval := preparedFixture(t, root, []string{"code"})
			previous := bootstrapFault
			t.Cleanup(func() { bootstrapFault = previous })
			bootstrapFault = func(step string) error {
				if step == "before_internal_verify" {
					return os.ErrPermission
				}
				return nil
			}
			if _, err := Apply(root, envelope, approval); err == nil {
				t.Fatal("injected failure was ignored")
			}
			transactionID := transactionIdentity(envelope, approval)
			cutErr := errors.New("test-only rollback interruption")
			bootstrapFault = func(step string) error {
				if step == cut {
					return cutErr
				}
				return nil
			}
			if _, err := Rollback(root, transactionID); !errors.Is(err, cutErr) {
				t.Fatalf("rollback was not cut at %s: %v", cut, err)
			}
			bootstrapFault = previous
			result, err := Rollback(root, transactionID)
			if err != nil || result.Status != StatusRolledBack {
				t.Fatalf("rollback after a cut at %s: %#v err=%v", cut, result, err)
			}
			for _, path := range envelope.WriteOrder {
				if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
					t.Fatalf("formal target remains after rollback: %s err=%v", path, err)
				}
			}
		})
	}
}
