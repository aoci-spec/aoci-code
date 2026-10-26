// 条目删除管线与 aoci_remove_entry 工具(v2.8 P1,D75下半句——人工裁决的执行工具)。
// 索引条目: tools_remove.go[MWR8AM]
//
// 定位: 与 tools_write.go 的 ApplyUpdateEntry 同族的 plan/commit 分段管线,
// MCP 与 CLI 共用唯一实现(防隐性孪生)。独立成文件不并入 tools_write.go
// (分文件先例,避免整文件覆盖既有大文件)。
//
// 分段形态(阶段 D 重构): 原内联 plan/commit 重构为 planRemoveEntry/commitRemove
// 显式分段,与 update 管线同构 —— 动机: 接入并发防线后两管线临界区形态必须
// 一致(刻意同构防隐性分叉);且分段后 CAS 冲突路径才可测试(测试可在 plan 与
// commit 之间篡改索引,内联形态无此观测窗口)。removePlan 非导出,包外无法
// 伪造计划调用 commit(R18 结构保证,与 planResult 同族)。
//
// 并发防线(阶段 D,与 tools_write.go 同款两防线,详见其包注释):
// 防线一 = fs.AcquireIndexLock 跨进程写锁只包 commit;
// 防线二 = index hash CAS,plan 记录所读索引 sha256,commit 取锁后重读比对,
// 不符即拒 write_conflict。锁超时映射 write_conflict,锁 IO 故障映射 internal,
// 释锁失败仅 stderr 警告不污染业务结果。
//
// 差异化护栏(orphanOnly,本批核心裁决):
//   - MCP 侧(agent)调用恒传 orphanOnly=true —— 仅允许删除孤儿条目
//     (目标文件已不在磁盘),活文件条目一律拒绝并引导 aoci_report。
//     理由: S字段是组织伤疤,agent误删活文件伤疤的代价远高于留一条孤儿;
//     agent的真实删除场景恰是清理 index update 报出的 deleted 孤儿。
//   - CLI 侧(人)传 orphanOnly=false 全权裁决 —— 删除活文件条目合法
//     (随后该文件按 Missing 态浮出属正确语义:磁盘有索引无待重建条目)。
//
// 基线纪律: 删除成功后索引自身指纹恒前移(D51同族,否则每次删除自造
// aoci.txt 假 Stale);baseline.Load/Save 第一参是仓库根而非基线文件路径
// (2026-07-12 修正: 初版误传 BaselinePath,string 对 string 编译放行但 Load
// 恒找不到基线→前移静默跳过→每次删除自造假 Stale;测试曾绿因夹具无基线
// 前移分支零覆盖 —— 亲见样板 server.go/index_header.go 均传 root 为裁决依据);
// 目标文件在基线中的残键刻意不清理 —— Detect 四态以磁盘快照与索引为准,
// 残键无害,且 baseline 包刻意不提供删键 API(基线只增改不删,语义单纯)。
package mcptools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// RemoveOutcome 删除结果(供 MCP 与 CLI 双端渲染)
type RemoveOutcome struct {
	// Rel 归一后的目标相对路径
	Rel string
	// RemovedLine 被删除的条目整行(回显供确认/审计)
	RemovedLine string
	// DryRun 干跑标记(true=未落盘)
	DryRun bool
	// OwnershipRepair marks the machine-proven ownership-conflict path. It
	// changes only the success rendering; the write still uses this remove
	// pipeline and its existing transaction authorities.
	OwnershipRepair bool
	// PreservedOwner is the deterministic owner whose formal asset is guarded
	// and left untouched while the misplaced Domain Entry is removed.
	PreservedOwner string
	// Superseded reports that a receipt the Volume had moved past was closed
	// because its object was already absent; no index or Baseline bytes moved.
	Superseded bool
	// StaleReceiptDiscarded reports that a receipt bound to a Volume version
	// that no longer existed was discarded before this fresh removal.
	StaleReceiptDiscarded bool
}

// removePlan 删除计划(非导出): plan 产出、commit 消费,包外无法伪造(R18)。
type removePlan struct {
	// out 面向渲染的结果骨架
	out *RemoveOutcome
	// newText 应用删除后的索引全文(落盘材料)
	newText string
	// rc plan 阶段已读取的仓库上下文
	rc *repoCtx
	// indexHash plan 所读索引全文的 sha256(CAS 凭据,算法与 update 管线同源 indexTextHash)
	indexHash string
	// alreadyApplied表示索引已处于持久恢复意图绑定的删除postimage，本轮只补Baseline。
	alreadyApplied bool
	// superseded marks a receipt the index or Volume has moved past while its
	// object is already absent: the commit closes the receipt and writes
	// nothing else.
	superseded bool
	// staleReceipt is the receipt the plan judged stale (superseded or to be
	// discarded); the commit re-reads it under the lock and acts only if it is
	// still that receipt and the target still matches neither of its images.
	staleReceipt *removeRecovery
	// orphanOnly要求计划与持锁提交两个时点都证明目标路径确实不存在。
	orphanOnly bool
	// volumeMode reuses this established explicit-remove pipeline for one
	// proven Code or Database Cognition orphan. Root and Meta remain guards.
	volumeMode         bool
	objectRef          string
	volumeID           string
	guardSHA256        map[string]string
	evidenceIdentity   string
	baselinePreSHA256  string
	baselinePostSHA256 string
	baselinePostimage  string
}

var writeRemoveIndex = fs.AtomicWriteCAS
var removeRecoveryFile = os.Remove
var lstatRemoveTarget = os.Lstat

type removeRecovery struct {
	Version            int               `json:"version"`
	Rel                string            `json:"rel"`
	RemovedLine        string            `json:"removed_line"`
	PreIndexSHA256     string            `json:"pre_index_sha256"`
	PostIndexSHA256    string            `json:"post_index_sha256"`
	Completed          bool              `json:"completed,omitempty"`
	VolumeMode         bool              `json:"volume_mode,omitempty"`
	ObjectRef          string            `json:"object_ref,omitempty"`
	VolumeID           string            `json:"volume_id,omitempty"`
	VolumePath         string            `json:"volume_path,omitempty"`
	VolumePostimage    string            `json:"volume_postimage,omitempty"`
	GuardSHA256        map[string]string `json:"guard_sha256,omitempty"`
	EvidenceIdentity   string            `json:"evidence_identity,omitempty"`
	BaselinePreSHA256  string            `json:"baseline_pre_sha256,omitempty"`
	BaselinePostSHA256 string            `json:"baseline_post_sha256,omitempty"`
	BaselinePostimage  string            `json:"baseline_postimage,omitempty"`
	OwnershipRepair    bool              `json:"ownership_repair,omitempty"`
	PreservedOwner     string            `json:"preserved_owner,omitempty"`
}

// removeEntryArgs aoci_remove_entry 的入参(In 结构供 go-sdk 自动推导 schema 并前置校验)
type removeEntryArgs struct {
	Path string `json:"path"`
}

// registerRemoveTool 注册 aoci_remove_entry(挂载点在 server.go 的 RunStdio)。
// agent 侧恒 orphanOnly=true(护栏见包注释);handler 经 guard 包裹恢复 panic。
func registerRemoveTool(
	srv *mcp.Server,
	root string,
	descriptions mcpToolDescriptions,
	inputSchemas mcpInputSchemas,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "aoci_remove_entry",
		Description: descriptions[textassets.ContractMCPRemoveDescription],
		InputSchema: inputSchemas["aoci_remove_entry"],
	}, func(ctx context.Context, req *mcp.CallToolRequest, in removeEntryArgs) (*mcp.CallToolResult, any, error) {
		res := guard(func() *mcp.CallToolResult {
			out, fail := ApplyRemoveEntry(root, in.Path, "agent", true, false)
			if fail != nil {
				return failResult(fail)
			}
			return textResult(RenderRemoveOutcome(out))
		})
		return res, nil, nil
	})
}

// planRemoveEntry 删除管线前半段: 路径归一 → 加载 → 定位 → 护栏 → 文本变换(内存),纯读零副作用。
func planRemoveEntry(root, rawPath string, orphanOnly bool) (*removePlan, *Fail) {
	trimmed := strings.TrimSpace(rawPath)
	if strings.HasPrefix(trimmed, "code:") || cognition.IsCanonicalDatabaseRef(trimmed) {
		return planVolumeRemoveEntry(root, trimmed, orphanOnly)
	}
	rel, err := fs.NormalizeRelPath(rawPath)
	if err != nil {
		return nil, &Fail{
			Code: "path_unsafe",
			Msg: writeMessage(
				"remove.path_invalid",
				localizeRelPathError(err),
			),
			Hint: writeMessage("remove.hint.relative_path"),
		}
	}

	// A plain repository-relative path under a Volumes v1 layout names the
	// Code object code:<path>; route it to the Volume pipeline instead of the
	// Legacy loader, whose volume_read_only refusal never named that spelling
	// (#112).
	if ref, isVolume := volumeCodeRef(root, rel); isVolume {
		return planVolumeRemoveEntry(root, ref, orphanOnly)
	}

	// Not a Volumes object: a Legacy single-file layout, read-only since
	// 0.2.0, or no layout at all. Let the Legacy loader name an absent or
	// Volumes index as before, and refuse the Legacy write itself.
	if _, fail := loadRepoCtx(root); fail != nil {
		return nil, fail
	}
	return nil, legacyReadOnlyFail()
}

// volumeCodeRef maps a plain repository-relative path onto its canonical Code
// identity when the repository is a Volumes v1 layout, so `--path dir/file.go`
// and `path: "dir/file.go"` reach the same guarded Volume pipeline as
// `code:dir/file.go`. Any load failure answers false and the Legacy path keeps
// its own diagnostics.
func volumeCodeRef(root, rel string) (string, bool) {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return "", false
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return "", false
	}
	return "code:" + rel, true
}

func removeRecoveryPath(root, rel string) string {
	digest := sha256.Sum256([]byte(rel))
	return filepath.Join(root, ".aoci", "transactions", "remove-"+hex.EncodeToString(digest[:])+".json")
}

func saveRemoveRecovery(root string, recovery removeRecovery) error {
	path := removeRecoveryPath(root, recovery.Rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recovery, "", "  ")
	if err != nil {
		return err
	}
	return fs.AtomicWrite(path, append(data, '\n'))
}

func loadRemoveRecovery(root, rel string) (*removeRecovery, error) {
	data, err := os.ReadFile(removeRecoveryPath(root, rel))
	if err != nil {
		return nil, err
	}
	var recovery removeRecovery
	if err := json.Unmarshal(data, &recovery); err != nil {
		return nil, err
	}
	if recovery.Version != 1 || recovery.Rel != rel || recovery.RemovedLine == "" ||
		!validRecoverySHA256(recovery.PreIndexSHA256) ||
		!validRecoverySHA256(recovery.PostIndexSHA256) {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	if recovery.VolumeMode && (recovery.ObjectRef != recovery.Rel || recovery.VolumeID == "" || recovery.VolumePath == "" ||
		recovery.VolumePostimage == "" || len(recovery.GuardSHA256) == 0 || recovery.EvidenceIdentity == "" ||
		!validRecoverySHA256(recovery.BaselinePreSHA256) || !validRecoverySHA256(recovery.BaselinePostSHA256) ||
		recovery.BaselinePostimage == "") {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	if recovery.OwnershipRepair && (recovery.PreservedOwner == "" || recovery.PreservedOwner == recovery.VolumeID) {
		return nil, errors.New(writeMessage("remove.recovery_invalid"))
	}
	return &recovery, nil
}

// ApplyRemoveEntry 删除指定文件的索引条目(plan/commit 薄组合,双端唯一实现)。
// orphanOnly=true 时目标文件仍在磁盘即拒绝(MCP/agent 护栏);
// dryRun=true 时走完 plan 即返回不落盘不前移不记账。
func ApplyRemoveEntry(root, rawPath, source string, orphanOnly, dryRun bool) (*RemoveOutcome, *Fail) {
	if fail := validateWriteMessages(requiredRemoveMessages); fail != nil {
		return nil, fail
	}
	p, fail := planRemoveEntry(root, rawPath, orphanOnly)
	if fail != nil {
		if !dryRun {
			if cleanupFail := cleanupCompletedRemoveRecovery(root, rawPath); cleanupFail != nil {
				return nil, cleanupFail
			}
		}
		return nil, fail
	}
	p.out.DryRun = dryRun
	if dryRun {
		return p.out, nil
	}
	// planRemoveEntry answers a Volumes plan or a refusal: the Legacy commit
	// left with the Legacy authoring surfaces (0.2.0).
	if fail := commitVolumeRemove(root, source, p); fail != nil {
		return nil, fail
	}
	return p.out, nil
}

func cleanupCompletedRemoveRecovery(root, rawPath string) *Fail {
	rel, err := fs.NormalizeRelPath(rawPath)
	if err != nil {
		return nil
	}
	recovery, err := loadRemoveRecovery(root, rel)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"remove.completed_recovery_read_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	if recovery == nil || !recovery.Completed {
		return nil
	}
	if err := removeRecoveryFile(removeRecoveryPath(root, rel)); err != nil && !os.IsNotExist(err) {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"remove.completed_recovery_cleanup_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	return nil
}

// RenderRemoveOutcome 渲染删除结果文案(双端共用)。
// preview 分支不打"基线已前移"(2026-07-12 CLI冒烟证实的文案缺陷: 干跑未动基线却宣称前移)。
func RenderRemoveOutcome(o *RemoveOutcome) string {
	if o.DryRun && o.Superseded {
		return writeMessage("remove.preview_superseded", o.Rel, o.RemovedLine)
	}
	if o.DryRun {
		return writeMessage("remove.preview", o.Rel, o.RemovedLine)
	}
	if o.Superseded {
		return writeMessage("remove.recovery_superseded", o.Rel, o.RemovedLine)
	}
	prefix := ""
	if o.StaleReceiptDiscarded {
		prefix = writeMessage("remove.stale_receipt_discarded", o.Rel) + "\n"
	}
	if o.OwnershipRepair {
		return prefix + writeMessage("remove.ownership_repair_applied", o.Rel, o.PreservedOwner, true, true, o.RemovedLine)
	}
	return prefix + writeMessage("remove.applied", o.Rel, o.RemovedLine)
}

// receiptKey is the identity a removal's receipt is filed under: the canonical
// object reference in Volumes, the repository-relative path in Legacy.
func (p *removePlan) receiptKey() string {
	if p.volumeMode {
		return p.objectRef
	}
	return p.out.Rel
}

// rejectOtherRemovePending fails closed on every pending receipt except this
// removal's own: a foreign file, another object's receipt, or an unfinished
// batch means the state is not one this write can prove it is extending.
func rejectOtherRemovePending(root string, p *removePlan) *Fail {
	other, err := cognitiontxn.OtherPending(root, filepath.Base(removeRecoveryPath(root, p.receiptKey())))
	if err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_read_failed", localeSafeWriteDetail(err.Error()))}
	}
	if other != "" {
		return &Fail{Code: errWriteConflict, Msg: "remove_other_transaction_pending: " + other,
			Hint: writeMessage("remove.hint.inspect_recovery")}
	}
	return nil
}

// closeSupersededRemove finishes a receipt the index or Volume has moved past
// while its object is already absent: the Ledger line first, so a retry after
// a later failure meets an equal terminal event, then the completion marker,
// then the file. Nothing else is written; the transactions that moved past
// the receipt already governed the index and the Baseline, and a receipt's
// stale postimages must never be written over a newer state. The Ledger id
// carries the receipt's preimage so a later removal of a re-created object
// never collides with this one.
func closeSupersededRemove(root, source string, p *removePlan, objectAbsent func() *Fail) *Fail {
	if fail := objectAbsent(); fail != nil {
		return fail
	}
	recovery, fail := reloadStaleReceipt(root, p)
	if fail != nil {
		return fail
	}
	if recovery == nil {
		return nil
	}
	key := p.receiptKey()
	event := ledger.Event{Op: "remove_entry", Source: source, Result: ledger.ResultOK, RecoveredCount: 1, DuplicateApplies: 1,
		RecoveryTransactionID: removeTransactionID(root, key) + "-superseded-" + shortSHA(recovery.PreIndexSHA256),
		PreIndexSHA256:        recovery.PreIndexSHA256, PostIndexSHA256: recovery.PostIndexSHA256, FallbackReason: "receipt_superseded"}
	if p.volumeMode {
		// Volumes completion is a terminal Ledger event, as its ordinary commit
		// writes; Legacy never required the Ledger and keeps the tolerant append.
		if err := cognitiontxn.EnsureLedger(root, p.rc.cfg.LedgerEnabled, event); err != nil {
			return &Fail{Code: errInternal, Msg: "remove_ledger_completion_failed: " + localeSafeWriteDetail(err.Error()),
				Hint: writeMessage("remove.hint.retry_recovery")}
		}
	} else {
		ledger.Append(root, p.rc.cfg.LedgerEnabled, event)
	}
	recovery.Completed = true
	if err := saveRemoveRecovery(root, *recovery); err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.completion_marker_failed", localeSafeWriteDetail(err.Error())),
			Hint: writeMessage("remove.hint.retry_recovery")}
	}
	if err := removeRecoveryFile(removeRecoveryPath(root, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "remove_recovery_cleanup_failed"}
	}
	return nil
}

// discardStaleReceipt drops, under the lock, a receipt the plan judged stale
// because its object is still present: it binds an index or Volume version
// that no longer exists, and this call is the fresh removal decision. The
// Ledger records the discard with the receipt's images before the file goes,
// so the removed line it carried is not lost with it.
func discardStaleReceipt(root, source string, p *removePlan) *Fail {
	recovery, fail := reloadStaleReceipt(root, p)
	if fail != nil {
		return fail
	}
	if recovery == nil {
		return nil
	}
	raw, readErr := os.ReadFile(p.rc.paths.IndexPath)
	if readErr != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.cas_read_failed", localeSafeWriteDetail(readErr.Error()))}
	}
	if indexTextHash(string(raw)) != p.indexHash {
		// The object the plan judged present with its recorded line is only
		// proven by the target still being the plan's preimage.
		return &Fail{Code: errWriteConflict, Msg: writeMessage("remove.cas_stale"), Hint: writeMessage("remove.hint.replan")}
	}
	key := p.receiptKey()
	ledger.Append(root, p.rc.cfg.LedgerEnabled, ledger.Event{Op: "remove_entry", Source: source, Result: ledger.ResultOK,
		RecoveryTransactionID: removeTransactionID(root, key) + "-discarded-" + shortSHA(recovery.PreIndexSHA256),
		PreIndexSHA256:        recovery.PreIndexSHA256, PostIndexSHA256: recovery.PostIndexSHA256, FallbackReason: "stale_receipt_discarded"})
	if err := removeRecoveryFile(removeRecoveryPath(root, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "remove_stale_receipt_discard_failed"}
	}
	return nil
}

// reloadStaleReceipt re-reads the receipt the plan judged stale and proves,
// under the lock, that it is still that receipt and that the index or Volume
// still matches neither of its images. A receipt that now matches one is not
// stale: the ordinary resume arms own it on the next call.
func reloadStaleReceipt(root string, p *removePlan) (*removeRecovery, *Fail) {
	judged := p.staleReceipt
	current, err := loadRemoveRecovery(root, p.receiptKey())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_read_failed", localeSafeWriteDetail(err.Error()))}
	}
	if judged == nil || current.PreIndexSHA256 != judged.PreIndexSHA256 || current.PostIndexSHA256 != judged.PostIndexSHA256 {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_receipt_changed_since_plan", Hint: writeMessage("remove.hint.replan")}
	}
	raw, readErr := os.ReadFile(p.rc.paths.IndexPath)
	if readErr != nil {
		return nil, &Fail{Code: errInternal, Msg: writeMessage("remove.cas_read_failed", localeSafeWriteDetail(readErr.Error()))}
	}
	if hash := indexTextHash(string(raw)); hash == current.PreIndexSHA256 || hash == current.PostIndexSHA256 {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_receipt_no_longer_stale", Hint: writeMessage("remove.hint.replan")}
	}
	return current, nil
}

func removeTransactionID(root, key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(removeRecoveryPath(root, key)), "remove-"), ".json")
}

func shortSHA(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
