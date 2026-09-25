package mcptools

import (
	"fmt"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// pendingCognitionDeliveryFail prevents Overview and ordinary local cognition
// reads from presenting a potentially partial image while recovery evidence or
// an AtomicWrite intent is active. check_only remains available so callers can
// inspect checkpoint facts without claiming a coherent cognition delivery.
func pendingCognitionDeliveryFail(root string, set *cognition.Set) *Fail {
	// One detection for every consumer: the cognitiontxn.Pending that Verify,
	// Guide, Maintain, the CLI gate, and every transaction start also read.
	// Until v0.1.0-rc15 this guard scanned the directory itself and stopped on
	// any receipt file while Verify recognised five kinds and reported aligned,
	// so a stale remove receipt blocked full delivery with nothing naming it (#74).
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage(
			"overview.delivery.recovery_inspection_failed",
			localeSafeMCPDetail(err.Error()),
		)}
	}
	if len(pending) > 0 {
		return &Fail{
			Code: errCognitionSnapshotUnavailable,
			Msg:  mcpMessage("overview.delivery.pending_recovery", pending[0].Filename),
			Hint: mcpMessage("overview.delivery.pending_recovery_hint"),
		}
	}

	for _, rel := range cognitionFormalAssetPaths(set) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		for _, suffix := range []string{".aoci-cas.intent", ".aoci-cas.swap"} {
			if _, statErr := os.Lstat(path + suffix); statErr == nil {
				return &Fail{
					Code: errCognitionSnapshotUnavailable,
					Msg:  mcpMessage("overview.delivery.pending_atomic_write", rel),
					Hint: mcpMessage("overview.delivery.pending_recovery_hint"),
				}
			} else if !os.IsNotExist(statErr) {
				return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage(
					"overview.delivery.recovery_inspection_failed",
					localeSafeMCPDetail(fmt.Sprintf("%s: %v", rel, statErr)),
				)}
			}
		}
	}
	return nil
}

// pendingHeaderTransactionFail在索引写锁内阻止其他治理写入越过未完成Header。
// Header恢复收据绑定的是完整索引pre/postimage；允许Entries或删除继续会让
// 同一Header run永久失去可证明恢复点。
func pendingHeaderTransactionFail(root string) *Fail {
	directory := filepath.Join(root, ".aoci", "transactions")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"entry.transaction.read_failed", localeSafeWriteDetail(err.Error()),
		)}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "header-") ||
			!strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		return &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.transaction.pending_header"),
			Hint: writeMessage("entry.transaction.hint.recover_header"),
		}
	}
	return nil
}
