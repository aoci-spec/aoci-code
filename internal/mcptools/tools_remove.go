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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
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

var saveRemoveBaseline = baseline.SaveUnderIndexLock
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
				localeSafeWriteDetail(err.Error()),
			),
			Hint: writeMessage("remove.hint.relative_path"),
		}
	}

	rc, fail := loadRepoCtx(root)
	if fail != nil {
		return nil, fail
	}

	recovery, recoveryErr := loadRemoveRecovery(root, rel)
	if recoveryErr != nil && !os.IsNotExist(recoveryErr) {
		return nil, &Fail{Code: errInternal, Msg: writeMessage(
			"remove.recovery_read_failed",
			localeSafeWriteDetail(recoveryErr.Error()),
		)}
	}
	if os.IsNotExist(recoveryErr) {
		recovery = nil
	}

	entry := index.FindEntry(rc.doc, rel)
	if entry != nil && recovery != nil {
		currentHash := indexTextHash(rc.text)
		if !recovery.Completed && currentHash == recovery.PreIndexSHA256 &&
			entry.FullLine == recovery.RemovedLine {
			resumedText, removeErr := index.RemoveEntry(rc.text, entry.FullLine)
			if removeErr == nil && indexTextHash(resumedText) == recovery.PostIndexSHA256 {
				return &removePlan{
					out:     &RemoveOutcome{Rel: rel, RemovedLine: entry.FullLine},
					newText: resumedText, rc: rc, indexHash: currentHash,
					orphanOnly: orphanOnly,
				}, nil
			}
		}
		return nil, &Fail{Code: errWriteConflict,
			Msg:  writeMessage("remove.recovery_entry_reappeared"),
			Hint: writeMessage("remove.hint.new_decision")}
	}
	if entry == nil {
		if recovery != nil {
			currentHash := indexTextHash(rc.text)
			if currentHash != recovery.PostIndexSHA256 {
				if recovery.Completed {
					return nil, &Fail{Code: errBadArgs,
						Msg:  writeMessage("remove.already_completed", rel),
						Hint: writeMessage("remove.hint.no_repeat")}
				}
				return nil, &Fail{Code: errWriteConflict,
					Msg:  writeMessage("remove.recovery_postimage_drift"),
					Hint: writeMessage("remove.hint.inspect_recovery")}
			}
			return &removePlan{
				out:     &RemoveOutcome{Rel: rel, RemovedLine: recovery.RemovedLine},
				newText: rc.text, rc: rc, indexHash: currentHash, alreadyApplied: true,
				orphanOnly: orphanOnly,
			}, nil
		}
		return nil, &Fail{
			Code: "bad_args",
			Msg:  writeMessage("remove.entry_missing", rel),
			Hint: writeMessage("remove.hint.confirm_entry"),
		}
	}

	if orphanOnly {
		if orphanFail := requireRemoveTargetAbsent(root, rel); orphanFail != nil {
			return nil, orphanFail
		}
	}

	newText, rmErr := index.RemoveEntry(rc.text, entry.FullLine)
	if rmErr != nil {
		return nil, &Fail{
			Code: "write_conflict",
			Msg: writeMessage(
				"remove.transform_failed",
				localeSafeWriteDetail(rmErr.Error()),
			),
			Hint: writeMessage("remove.hint.refresh"),
		}
	}

	return &removePlan{
		out:        &RemoveOutcome{Rel: rel, RemovedLine: entry.FullLine},
		newText:    newText,
		rc:         rc,
		indexHash:  indexTextHash(rc.text),
		orphanOnly: orphanOnly,
	}, nil
}

func requireRemoveTargetAbsent(root, rel string) *Fail {
	_, statErr := lstatRemoveTarget(filepath.Join(root, filepath.FromSlash(rel)))
	switch {
	case statErr == nil:
		return &Fail{
			Code: errBadArgs,
			Msg:  writeMessage("remove.live_target", rel),
			Hint: writeMessage("remove.hint.live_target"),
		}
	case os.IsNotExist(statErr):
		return nil
	default:
		return &Fail{
			Code: errInternal,
			Msg: writeMessage(
				"remove.orphan_check_failed",
				rel,
				localeSafeWriteDetail(statErr.Error()),
			),
			Hint: writeMessage("remove.hint.orphan_check"),
		}
	}
}

// commitRemove 删除管线后半段: 取锁 → CAS 校验 → 原子落盘 → 索引自身基线前移(D51) → 落账 → 释锁。
func commitRemove(root, source string, p *removePlan) *Fail {
	// —— 防线一: 跨进程写锁(与 update 管线同款,见 tools_write.go 包注释) ——
	lock, lerr := fs.AcquireIndexLock(root)
	if lerr != nil {
		if errors.Is(lerr, fs.ErrLockTimeout) {
			return &Fail{Code: "write_conflict",
				Msg: writeMessage(
					"remove.lock_timeout",
					localeSafeWriteDetail(lerr.Error()),
				),
				Hint: writeMessage("remove.hint.lock_timeout")}
		}
		return &Fail{Code: "internal", Msg: writeMessage(
			"remove.lock_failed",
			localeSafeWriteDetail(lerr.Error()),
		), Hint: writeMessage("remove.hint.lock_failed")}
	}
	defer func() {
		if rerr := lock.Release(); rerr != nil {
			fmt.Fprintln(os.Stderr, writeMessage(
				"remove.lock_release_warning",
				localeSafeWriteDetail(rerr.Error()),
			))
		}
	}()
	if transactionFail := pendingHeaderTransactionFail(root); transactionFail != nil {
		return transactionFail
	}
	if p.orphanOnly {
		if orphanFail := requireRemoveTargetAbsent(root, p.out.Rel); orphanFail != nil {
			return orphanFail
		}
	}

	// —— 防线二: CAS 新鲜度校验(防非协作方修改) ——
	curText, rerr := os.ReadFile(p.rc.paths.IndexPath)
	if rerr != nil {
		return &Fail{Code: "internal", Msg: writeMessage(
			"remove.cas_read_failed",
			localeSafeWriteDetail(rerr.Error()),
		), Hint: writeMessage("remove.hint.check_index")}
	}
	if indexTextHash(string(curText)) != p.indexHash {
		return &Fail{Code: "write_conflict",
			Msg:  writeMessage("remove.cas_stale"),
			Hint: writeMessage("remove.hint.replan")}
	}
	state, exists, loadErr := baseline.Load(root)
	if loadErr != nil {
		return &Fail{Code: errInternal,
			Msg: writeMessage(
				"remove.baseline_read_failed",
				localeSafeWriteDetail(loadErr.Error()),
			),
			Hint: writeMessage("remove.hint.baseline_read")}
	}
	if !exists || state == nil {
		state = baseline.NewBaseline(nil)
	}

	expectedPostimage := indexTextHash(p.newText)
	if !p.alreadyApplied {
		intent := removeRecovery{
			Version: 1, Rel: p.out.Rel, RemovedLine: p.out.RemovedLine,
			PreIndexSHA256: p.indexHash, PostIndexSHA256: expectedPostimage,
		}
		if intentErr := saveRemoveRecovery(root, intent); intentErr != nil {
			return &Fail{Code: errInternal, Msg: writeMessage(
				"remove.recovery_save_failed",
				localeSafeWriteDetail(intentErr.Error()),
			), Hint: writeMessage("remove.hint.index_unchanged")}
		}
		if wErr := writeRemoveIndex(p.rc.paths.IndexPath, []byte(p.newText), p.indexHash); wErr != nil {
			code := errInternal
			if errors.Is(wErr, fs.ErrAtomicCASConflict) {
				code = errWriteConflict
			}
			current, hashErr := baseline.HashFile(p.rc.paths.IndexPath)
			postimageWritten := hashErr == nil && current.SHA256 == expectedPostimage
			preimagePreserved := hashErr == nil && current.SHA256 == p.indexHash
			if preimagePreserved {
				if cleanupErr := removeRecoveryFile(removeRecoveryPath(root, p.out.Rel)); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
					return &Fail{Code: errInternal,
						Msg: writeMessage(
							"remove.recovery_cleanup_unapplied_failed",
							localeSafeWriteDetail(cleanupErr.Error()),
						),
						Hint: writeMessage("remove.hint.transaction_permissions")}
				}
			}
			if postimageWritten {
				return &Fail{Code: errInternal,
					Msg: writeMessage(
						"remove.postimage_recovery_incomplete",
						localeSafeWriteDetail(wErr.Error()),
					),
					Hint: writeMessage("remove.hint.resume_baseline")}
			}
			return &Fail{Code: code, Msg: writeMessage(
				"remove.index_write_failed",
				localeSafeWriteDetail(wErr.Error()),
			), Hint: writeMessage("remove.hint.disk_permissions")}
		}
	}
	// 索引自身基线前移。正式索引已写后任一失败都必须明确返回stopped事实，
	// 不能继续渲染“基线已前移”。
	var baselineFail *Fail
	fingerprint, hashErr := baseline.HashFile(p.rc.paths.IndexPath)
	if hashErr != nil {
		baselineFail = &Fail{Code: errInternal,
			Msg: writeMessage(
				"remove.index_hash_failed",
				localeSafeWriteDetail(hashErr.Error()),
			),
			Hint: writeMessage("remove.hint.no_repeat_repair")}
	} else if fingerprint.SHA256 != expectedPostimage {
		baselineFail = &Fail{Code: errWriteConflict,
			Msg:  writeMessage("remove.postimage_changed"),
			Hint: writeMessage("remove.hint.preserve_recovery")}
	} else {
		baseline.UpdateOne(state, p.rc.cfg.IndexPath, fingerprint)
		if saveErr := saveRemoveBaseline(root, state); saveErr != nil {
			baselineFail = &Fail{Code: errInternal,
				Msg: writeMessage(
					"remove.baseline_save_failed",
					localeSafeWriteDetail(saveErr.Error()),
				),
				Hint: writeMessage("remove.hint.retry_baseline")}
		}
	}
	if baselineFail == nil {
		confirmed, confirmErr := baseline.HashFile(p.rc.paths.IndexPath)
		if confirmErr != nil || confirmed.SHA256 != expectedPostimage {
			baselineFail = &Fail{Code: errWriteConflict,
				Msg:  writeMessage("remove.baseline_postimage_changed"),
				Hint: writeMessage("remove.hint.inspect_external")}
		}
	}
	if baselineFail == nil {
		completedIntent := removeRecovery{
			Version: 1, Rel: p.out.Rel, RemovedLine: p.out.RemovedLine,
			PreIndexSHA256: p.indexHash, PostIndexSHA256: expectedPostimage,
			Completed: true,
		}
		if intentErr := saveRemoveRecovery(root, completedIntent); intentErr != nil {
			baselineFail = &Fail{Code: errInternal,
				Msg: writeMessage(
					"remove.completion_marker_failed",
					localeSafeWriteDetail(intentErr.Error()),
				),
				Hint: writeMessage("remove.hint.retry_recovery")}
		} else if cleanupErr := removeRecoveryFile(removeRecoveryPath(root, p.out.Rel)); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			baselineFail = &Fail{Code: errInternal,
				Msg: writeMessage(
					"remove.recovery_cleanup_failed",
					localeSafeWriteDetail(cleanupErr.Error()),
				),
				Hint: writeMessage("remove.hint.retry_cleanup")}
		}
	}
	result := ledger.ResultOK
	if baselineFail != nil {
		result = ledger.ResultError
	}
	appliedCount := 1
	recoveredCount := 0
	duplicateApplies := 0
	if p.alreadyApplied {
		appliedCount = 0
		recoveredCount = 1
		duplicateApplies = 1
	}
	ledger.Append(root, p.rc.cfg.LedgerEnabled, ledger.Event{
		Op: "remove_entry", PathsCount: 1, AppliedCount: appliedCount,
		RecoveredCount: recoveredCount, DuplicateApplies: duplicateApplies,
		Source: source, Result: result,
	})
	return baselineFail
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
	if p.volumeMode {
		if fail := commitVolumeRemove(root, source, p); fail != nil {
			return nil, fail
		}
		return p.out, nil
	}
	if fail := commitRemove(root, source, p); fail != nil {
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
	if o.DryRun {
		return writeMessage("remove.preview", o.Rel, o.RemovedLine)
	}
	if o.OwnershipRepair {
		return writeMessage("remove.ownership_repair_applied", o.Rel, o.PreservedOwner, true, true, o.RemovedLine)
	}
	return writeMessage("remove.applied", o.Rel, o.RemovedLine)
}
