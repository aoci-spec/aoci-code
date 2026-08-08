// Entries Auto完成标记与恢复收据的反例测试。
package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

func TestEntriesAutoCleanupFailureRetryOnlyCompletesRecovery(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, firstErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	); firstErr != nil {
		t.Fatal(firstErr)
	}
	beforeRetry, err := draft.LoadManifest(root, runID)
	if err != nil || beforeRetry.AppliedAt == "" || len(beforeRetry.Applications) != 1 {
		t.Fatalf("首次调用必须已持久化唯一成功Application: err=%v manifest=%+v", err, beforeRetry)
	}

	previousPending := entriesAutoRecoveryPending
	previousComplete := completeEntriesAutoRecovery
	t.Cleanup(func() {
		entriesAutoRecoveryPending = previousPending
		completeEntriesAutoRecovery = previousComplete
	})
	entriesAutoRecoveryPending = func(string, []mcptools.AtomicUpdateItem) (bool, error) {
		return true, nil
	}
	completeEntriesAutoRecovery = func(string, []mcptools.AtomicUpdateItem) error {
		return errors.New("injected recovery cleanup failure")
	}
	stopped, stoppedErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	)
	if stoppedErr == nil || stopped.Status != entriesAutoStatusStopped ||
		stopped.FailedStep != entriesAutoStepAudit ||
		!strings.Contains(stopped.Recovery, "重复同一run只执行清理") {
		t.Fatalf("清理失败必须保留可恢复终态: err=%v result=%+v", stoppedErr, stopped)
	}
	completeEntriesAutoRecovery = func(string, []mcptools.AtomicUpdateItem) error { return nil }
	retry, retryErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	)
	if retryErr != nil || retry.Status != entriesAutoStatusApplied || retry.Applied != 0 ||
		retry.Recovered != len(manifest.Entries) || !retry.AuditRecorded ||
		!strings.Contains(retry.Recovery, "仅清理恢复收据") {
		t.Fatalf("同run重试必须只收口恢复副作用: err=%v result=%+v", retryErr, retry)
	}
	afterRetry, err := draft.LoadManifest(root, runID)
	if err != nil || len(afterRetry.Applications) != 1 {
		t.Fatalf("恢复重试不得追加第二个Application: err=%v manifest=%+v", err, afterRetry)
	}
}

func TestEntriesAutoRejectsUnprovenAppliedAt(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.MarkApplied(root, runID); err != nil {
		t.Fatal(err)
	}

	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	)
	if runErr == nil || result.Status != entriesAutoStatusStopped ||
		result.FailedStep != entriesAutoStepAudit || result.AuditRecorded ||
		!strings.Contains(runErr.Error(), "AppliedAt未绑定完整成功Application") {
		t.Fatalf("孤立AppliedAt不得伪装完成证据: err=%v result=%+v", runErr, result)
	}
}
