// D69-2A 原子批量回写测试。
package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func writeBatchSource(
	t *testing.T,
	root,
	rel string,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		absolutePath,
		[]byte("package demo\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func readBatchIndex(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(root, ".aoci", "index.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAtomicBatchRecoveryRejectsMissingPreimage(t *testing.T) {
	root := buildRepo(t)
	items := []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:非法恢复 | R:- | A:- | S:-",
	}}
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		t.Fatal(err)
	}
	key := atomicBatchKey(normalized)
	if err := saveAtomicBatchRecovery(root, atomicBatchRecovery{
		Version: 1, BatchKey: key,
		PostIndexSHA256: indexTextHash(readBatchIndex(t, root)),
	}); err != nil {
		t.Fatal(err)
	}

	pending, pendingErr := UpdateEntriesAtomicRecoveryPending(root, items)
	if pending || pendingErr == nil || !strings.Contains(pendingErr.Error(), "entries_batch_recovery_receipt_invalid") {
		t.Fatalf("缺少preimage的批次收据不得成为恢复证据: pending=%v err=%v", pending, pendingErr)
	}
}

func TestAtomicBatchRejectsDuplicatePath(t *testing.T) {
	root := buildRepo(t)

	_, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path:     "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:一 | R:- | A:- | S:-",
			},
			{
				Path:     "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:二 | R:- | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail == nil || fail.Code != errBadArgs {
		t.Fatalf("重复路径应 bad_args: %+v", fail)
	}
	if !strings.Contains(fail.Msg, "重复路径") {
		t.Fatalf("拒绝文案应点明重复路径: %+v", fail)
	}
}
