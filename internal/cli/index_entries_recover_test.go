// Entries已写入后失败的显式恢复与后续治理取代回归测试。
package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

const supersedingEntryA = "a.go[XUT5T]: F:后续合法职责甲 | R:b.go | A:- | S:-"

func buildEntriesRecoveryReadyRepo(t *testing.T) (string, string) {
	t.Helper()
	root, runID := buildManualAtomicEntriesRepo(t)
	indexText := readManualAtomicIndex(t, root)
	indexText = strings.Replace(
		indexText,
		manualAtomicOldA+"\n",
		"aoci.txt[XUT9T]: F:测试索引本体 | R:- | A:- | S:-\n"+manualAtomicOldA+"\n",
		1,
	)
	manualAtomicWriteFile(t, root, "aoci.txt", indexText)
	fixtureCfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fixtureSnapshot, _, err := baseline.Snapshot(root, fixtureCfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(fixtureSnapshot)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".aoci", "baseline.json.bak")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return root, runID
}

func buildEntriesBaselineIncompleteRun(t *testing.T) (string, string) {
	t.Helper()
	root, runID := buildEntriesRecoveryReadyRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	unblock := blockBaselineBackupReplacement(t, root)
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	unblock()
	var exitErr *ExitError
	if !errors.As(runErr, &exitErr) || exitErr.Code != ExitInternal || result == nil ||
		result.Applied != len(manifest.Entries) || result.RejectKinds != "baseline_incomplete" {
		t.Fatalf("夹具必须停在Entries已写、Baseline未完成: err=%v result=%+v\n%s", runErr, result, output.String())
	}
	return root, runID
}

func buildEntriesApplicationAuditIncompleteRun(t *testing.T) (string, string) {
	t.Helper()
	root, runID := buildEntriesRecoveryReadyRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	previous := appendEntriesAutoApplication
	appendEntriesAutoApplication = func(string, string, draft.ApplicationRecord, bool) error {
		return errors.New("injected application audit failure")
	}
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	appendEntriesAutoApplication = previous
	var exitErr *ExitError
	if !errors.As(runErr, &exitErr) || exitErr.Code != ExitInternal || result == nil ||
		result.Applied != len(manifest.Entries) || result.RejectKinds != "" {
		t.Fatalf("fixture must stop after Application audit failed post-write: err=%v result=%+v\n%s", runErr, result, output.String())
	}
	return root, runID
}

func applySupersedingGovernance(t *testing.T, root string, samePath bool) {
	t.Helper()
	path := "c.go"
	entry := "c.go[XUT5T]: F:后续跨路径职责 | R:a.go | A:- | S:-"
	if samePath {
		path = "a.go"
		entry = supersedingEntryA
	}
	manualAtomicWriteFile(t, root, path, "package demo\n\nvar LaterGovernance = true\n")
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	outcome, fail := mcptools.ApplyUpdateEntriesAtomic(
		root,
		[]mcptools.AtomicUpdateItem{{
			Path: path, NewEntry: entry, SourceSHA256: fingerprint.SHA256,
		}},
		ledger.SourceAgent,
		false,
	)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != 1 {
		t.Fatalf("后续合法治理失败: fail=%+v outcome=%+v", fail, outcome)
	}
}

func buildStageOnlyEntriesRun(t *testing.T) (string, string) {
	t.Helper()
	root := buildAgentPlanMixedRepo(t, true, true)
	r65ConfigureHostAgentMode(t, root, config.AutomationModeAuto)
	manualAtomicWriteFile(t, root, "orphan.go", "package main\n")
	fixtureCfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fixtureSnapshot, warnings, err := baseline.Snapshot(root, fixtureCfg.WalkOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("stage-only fixture snapshot failed: err=%v warnings=%v", err, warnings)
	}
	if err := baseline.Save(root, baseline.NewBaseline(fixtureSnapshot)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".aoci", "baseline.json.bak")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	request := r65HostAgentRequest(
		t, root, "new.go", "new.go[XAP7T]: F:待关闭旧草稿 | R:- | A:- | S:-",
	)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	staged, err := stageAgentEntries(root, cfg, doc, indexPath, request)
	if err != nil {
		t.Fatal(err)
	}
	return root, staged.RunID
}

func applyStageOnlySupersedingGovernance(t *testing.T, root string) {
	t.Helper()
	const path = "c.go"
	manualAtomicWriteFile(t, root, path, "package main\n\nvar LaterGovernance = true\n")
	fingerprint, err := baseline.HashFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	outcome, fail := mcptools.ApplyUpdateEntriesAtomic(
		root,
		[]mcptools.AtomicUpdateItem{{
			Path: path, NewEntry: "c.go[XAP3T]: F:后续合法职责 | R:- | A:- | S:-",
			SourceSHA256: fingerprint.SHA256,
		}},
		ledger.SourceAgent,
		false,
	)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != 1 {
		t.Fatalf("stage-only superseding governance failed: fail=%+v outcome=%+v", fail, outcome)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return string(digest[:])
}

func activeEntriesTransaction(t *testing.T, root string) (string, []byte) {
	t.Helper()
	directory := filepath.Join(root, ".aoci", "transactions")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "entries-") ||
			!strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return path, data
	}
	t.Fatal("fixture has no active Entries transaction")
	return "", nil
}

func TestEntriesRecoverCompletesOriginalPostimageWithoutReapply(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	indexPath := filepath.Join(root, "aoci.txt")
	indexBefore := fileDigest(t, indexPath)

	result, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || result == nil || result.Status != draft.RunResolutionRecovered ||
		result.Applied != 0 || result.Recovered != 2 {
		t.Fatalf("原postimage恢复必须只补齐原事务收尾: err=%v result=%+v", err, result)
	}
	if fileDigest(t, indexPath) != indexBefore {
		t.Fatal("恢复不得重复写正式索引")
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || manifest.AppliedAt == "" || len(manifest.Applications) != 2 ||
		manifest.Applications[0].RejectKinds != "baseline_incomplete" ||
		len(manifest.Resolutions) != 1 || manifest.Resolutions[0].Status != draft.RunResolutionRecovered {
		t.Fatalf("原失败与恢复关联必须同时保留: err=%v manifest=%+v", err, manifest)
	}
	repeated, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || repeated == nil || !repeated.AlreadyResolved || repeated.Recovered != 2 {
		t.Fatalf("原事务恢复的幂等结果必须保留防重数量: err=%v result=%+v", err, repeated)
	}
}

func TestEntriesRecoverClosesPreApplyRunWithoutReplayingCandidates(t *testing.T) {
	root := buildAgentPlanMixedRepo(t, true, true)
	r65ConfigureHostAgentMode(t, root, config.AutomationModeAuto)
	request := r65HostAgentRequest(
		t, root, "new.go", "new.go[XAP7T]: F:零写恢复 | R:- | A:- | S:-",
	)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	staged, err := stageAgentEntries(root, cfg, doc, indexPath, request)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore := fileDigest(t, indexPath)

	result, err := recoverEntriesRun(root, staged.RunID, ledger.SourceHuman)
	if err != nil || result == nil || result.Status != draft.RunResolutionZeroWrite ||
		result.Applied != 0 || result.Recovered != 0 || result.AlreadyResolved {
		t.Fatalf("pre-Apply recovery must close with zero writes: err=%v result=%+v", err, result)
	}
	if fileDigest(t, indexPath) != indexBefore {
		t.Fatal("zero-write closure must not replay the staged candidate")
	}
	manifest, err := draft.LoadManifest(root, staged.RunID)
	if err != nil || len(manifest.ZeroWriteClosures) != 1 || len(manifest.Reviews) != 0 ||
		len(manifest.Applications) != 0 || manifest.AppliedAt != "" {
		t.Fatalf("original draft and zero-write proof must remain append-only: err=%v manifest=%+v", err, manifest)
	}
	repeated, err := recoverEntriesRun(root, staged.RunID, ledger.SourceHuman)
	if err != nil || repeated == nil || !repeated.AlreadyResolved ||
		repeated.Status != draft.RunResolutionZeroWrite {
		t.Fatalf("zero-write recovery must be idempotent: err=%v result=%+v", err, repeated)
	}
}

func TestEntriesRecoverPreApplyRunFailsClosedAfterIndexDrift(t *testing.T) {
	root := buildAgentPlanMixedRepo(t, true, true)
	r65ConfigureHostAgentMode(t, root, config.AutomationModeAuto)
	request := r65HostAgentRequest(
		t, root, "new.go", "new.go[XAP7T]: F:拒绝重放 | R:- | A:- | S:-",
	)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	staged, err := stageAgentEntries(root, cfg, doc, indexPath, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	manualAtomicWriteFile(t, root, "aoci.txt", string(data)+"\n")

	result, recoverErr := recoverEntriesRun(root, staged.RunID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending ||
		!strings.Contains(recoverErr.Error(), "pre_apply_zero_write_unproven") {
		t.Fatalf("pre-Apply closure must fail closed after Index drift: err=%v result=%+v", recoverErr, result)
	}
	manifest, err := draft.LoadManifest(root, staged.RunID)
	if err != nil || len(manifest.ZeroWriteClosures) != 0 {
		t.Fatalf("failed proof must not create a terminal claim: err=%v manifest=%+v", err, manifest)
	}
}

func TestEntriesRecoverClosesStageOnlyRunAfterProvenLaterGovernance(t *testing.T) {
	root, runID := buildStageOnlyEntriesRun(t)
	applyStageOnlySupersedingGovernance(t, root)
	indexPath := filepath.Join(root, "aoci.txt")
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	indexBefore := fileDigest(t, indexPath)
	baselineBefore := fileDigest(t, baselinePath)
	cfg, doc, _ := agentPlanLoadDocument(t, root)
	score, err := indexgen.BuildScore(root, cfg, doc)
	if err != nil || score.Drift.ActionableMissing == 0 {
		t.Fatalf("fixture must retain actionable Missing: err=%v drift=%+v", err, score.Drift)
	}

	result, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || result == nil || result.Status != draft.RunResolutionZeroWrite ||
		result.Applied != 0 || result.Recovered != 0 || result.AlreadyResolved ||
		result.CurrentIndexSHA256 == result.PreIndexSHA256 ||
		result.CurrentBaselineSHA256 == "" || result.RepositorySHA256 == "" ||
		len(result.GovernanceReceipts) == 0 {
		t.Fatalf("proven later governance must close stage-only run: err=%v result=%+v", err, result)
	}
	if fileDigest(t, indexPath) != indexBefore || fileDigest(t, baselinePath) != baselineBefore {
		t.Fatal("stage-only recovery must not rewrite Index or Baseline")
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifest.ZeroWriteClosures) != 1 {
		t.Fatalf("v2 closure missing: err=%v manifest=%+v", err, manifest)
	}
	closure := manifest.ZeroWriteClosures[0]
	if closure.Version != 2 || closure.StagedTransactionID == "" ||
		closure.CurrentIndexSHA256 != result.CurrentIndexSHA256 ||
		!reflect.DeepEqual(closure.GovernanceReceipts, result.GovernanceReceipts) {
		t.Fatalf("v2 closure proof incomplete: %+v", closure)
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("v2 recovery Ledger is corrupt: %d", corrupt)
	}
	ledgerBound := false
	for _, event := range events {
		if matchesEntriesZeroWriteClosureEvent(event, runID, closure) {
			ledgerBound = true
			break
		}
	}
	if !ledgerBound {
		t.Fatalf("v2 recovery Ledger is not bound to the closure: %+v", events)
	}
	repeated, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || repeated == nil || !repeated.AlreadyResolved {
		t.Fatalf("v2 recovery must be idempotent: err=%v result=%+v", err, repeated)
	}
	receiptPath := filepath.Join(
		root, ".aoci", "governance", "entries-"+closure.GovernanceReceipts[0]+".json",
	)
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != runID {
		t.Fatalf("missing stored receipt must restore pending state: run=%q err=%v", pending, err)
	}
}

func TestEntriesRecoverRejectsStageOnlyReceiptOwnedByStagedBatch(t *testing.T) {
	root, runID := buildStageOnlyEntriesRun(t)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadEntryDraftSnapshot(root, runID, manifest)
	if err != nil {
		t.Fatal(err)
	}
	items, err := atomicItemsFromReviewedSnapshot(&entriesCheckResult{
		Manifest: manifest, Snapshot: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, fail := mcptools.ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != len(items) {
		t.Fatalf("fixture staged transaction apply failed: fail=%+v outcome=%+v", fail, outcome)
	}

	result, recoverErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending ||
		!strings.Contains(recoverErr.Error(), "staged_transaction_ambiguous") {
		t.Fatalf("receipt owned by staged batch must fail closed: err=%v result=%+v", recoverErr, result)
	}
	manifest, err = draft.LoadManifest(root, runID)
	if err != nil || len(manifest.ZeroWriteClosures) != 0 {
		t.Fatalf("ambiguous staged Apply must not create closure: err=%v manifest=%+v", err, manifest)
	}
}

func TestEntriesRecoverStageOnlyTamperedReceiptRestoresPending(t *testing.T) {
	root, runID := buildStageOnlyEntriesRun(t)
	applyStageOnlySupersedingGovernance(t, root)
	if _, err := recoverEntriesRun(root, runID, ledger.SourceHuman); err != nil {
		t.Fatal(err)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifest.ZeroWriteClosures) != 1 ||
		len(manifest.ZeroWriteClosures[0].GovernanceReceipts) == 0 {
		t.Fatalf("fixture has no v2 governance proof: err=%v manifest=%+v", err, manifest)
	}
	receiptPath := filepath.Join(
		root, ".aoci", "governance",
		"entries-"+manifest.ZeroWriteClosures[0].GovernanceReceipts[0]+".json",
	)
	receiptData, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(receiptData, []byte(`"kind": "entries"`), []byte(`"kind": "tampered"`), 1)
	if bytes.Equal(tampered, receiptData) {
		t.Fatal("fixture receipt did not contain expected kind")
	}
	if err := os.WriteFile(receiptPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != runID {
		t.Fatalf("tampered stored receipt must restore pending state: run=%q err=%v", pending, err)
	}
}

func TestEntriesRecoverRejectsStageOnlyRunWithApplyLedgerEvidence(t *testing.T) {
	root, runID := buildStageOnlyEntriesRun(t)
	applyStageOnlySupersedingGovernance(t, root)
	ledger.Append(root, true, ledger.Event{
		Op: "entries_apply", Source: ledger.SourceAgent, Result: ledger.ResultError,
		DraftRunID: runID,
	})

	result, recoverErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending ||
		!strings.Contains(recoverErr.Error(), "pre_apply_ledger_conflict") {
		t.Fatalf("Apply ledger evidence must reject zero-write closure: err=%v result=%+v", recoverErr, result)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifest.ZeroWriteClosures) != 0 {
		t.Fatalf("Apply ledger conflict must remain pending: err=%v manifest=%+v", err, manifest)
	}
}

func TestEntriesRecoverCompletesApplicationAuditFailureWithoutReapply(t *testing.T) {
	root, runID := buildEntriesApplicationAuditIncompleteRun(t)
	indexPath := filepath.Join(root, "aoci.txt")
	indexBefore := fileDigest(t, indexPath)
	result, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || result == nil || result.Status != draft.RunResolutionRecovered ||
		result.FailureKinds != "application_audit" || result.Applied != 0 || result.Recovered != 2 {
		t.Fatalf("Application audit recovery must close out without replay: err=%v result=%+v", err, result)
	}
	if fileDigest(t, indexPath) != indexBefore {
		t.Fatal("Application audit recovery must not rewrite the formal index")
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifest.Applications) != 1 ||
		manifest.Applications[0].Applied != 0 || manifest.Applications[0].Recovered != 2 ||
		len(manifest.Resolutions) != 1 ||
		manifest.Resolutions[0].FailureKinds != "application_audit" {
		t.Fatalf("the Ledger failure and recovered Application must remain reconstructible: err=%v manifest=%+v", err, manifest)
	}
}

func TestEntriesRecoverSafelyTerminatesSupersededSameAndCrossPath(t *testing.T) {
	for _, samePath := range []bool{true, false} {
		name := "cross_path"
		if samePath {
			name = "same_path"
		}
		t.Run(name, func(t *testing.T) {
			root, runID := buildEntriesBaselineIncompleteRun(t)
			applySupersedingGovernance(t, root, samePath)
			indexBefore := fileDigest(t, filepath.Join(root, "aoci.txt"))
			baselineBefore := fileDigest(t, filepath.Join(root, ".aoci", "baseline.json"))

			result, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
			if err != nil || result == nil || result.Status != draft.RunResolutionSuperseded {
				t.Fatalf("后续合法治理应安全终结旧run: err=%v result=%+v", err, result)
			}
			if result.Applied != 0 || result.Recovered != 0 || len(result.GovernanceReceipts) == 0 {
				t.Fatalf("取代终结不得伪装Apply或Baseline重放: %+v", result)
			}
			manifest, loadErr := draft.LoadManifest(root, runID)
			if loadErr != nil || manifest.AppliedAt != "" || len(manifest.Applications) != 1 ||
				manifest.Applications[0].RejectKinds != "baseline_incomplete" ||
				len(manifest.Resolutions) != 1 ||
				manifest.Resolutions[0].FailureKinds != "baseline_incomplete" ||
				manifest.Resolutions[0].PreIndexSHA256 == "" ||
				manifest.Resolutions[0].PostIndexSHA256 == "" ||
				manifest.Resolutions[0].CurrentIndexSHA256 == "" {
				t.Fatalf("取代终态必须保留原Manifest失败和完整证明: err=%v manifest=%+v", loadErr, manifest)
			}
			if fileDigest(t, filepath.Join(root, "aoci.txt")) != indexBefore ||
				fileDigest(t, filepath.Join(root, ".aoci", "baseline.json")) != baselineBefore {
				t.Fatal("取代终结不得写索引或前移Baseline")
			}
		})
	}
}

func TestEntriesRecoverRejectsUnknownExternalWriteEvenWhenBaselineMatches(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	indexPath := filepath.Join(root, "aoci.txt")
	external := readManualAtomicIndex(t, root) + "# unknown external write\n"
	if err := os.WriteFile(indexPath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("读取Baseline失败: exists=%v err=%v", exists, err)
	}
	fingerprint, err := baseline.HashFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	baseline.UpdateOne(state, "aoci.txt", fingerprint)
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	result, recoverErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending ||
		!strings.Contains(recoverErr.Error(), "合法治理证明链") {
		t.Fatalf("未知外部写入必须fail-closed: err=%v result=%+v", recoverErr, result)
	}
	if pending, _ := draft.LatestPendingRun(root, draft.KindEntries); pending != runID {
		t.Fatalf("拒绝后旧run必须继续pending: %q", pending)
	}
}

func TestEntriesRecoverRejectsCurrentDriftAndNeverReappliesStaleCandidate(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	manualAtomicWriteFile(t, root, "a.go", "package demo\n\nvar UnknownDrift = true\n")
	indexBefore := fileDigest(t, filepath.Join(root, "aoci.txt"))

	result, recoverErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending {
		t.Fatalf("当前源码/Baseline漂移必须拒绝终结: err=%v result=%+v", recoverErr, result)
	}
	if fileDigest(t, filepath.Join(root, "aoci.txt")) != indexBefore {
		t.Fatal("旧source_sha256过期时恢复绝不得重新Apply候选")
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || manifest.AppliedAt != "" || len(manifest.Resolutions) != 0 {
		t.Fatalf("拒绝恢复不得伪造终态: err=%v manifest=%+v", err, manifest)
	}
}

func TestEntriesRecoverRejectsSupersededStateWithBaselineDrift(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, false)
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state == nil {
		t.Fatalf("fixture Baseline unavailable: exists=%v err=%v", exists, err)
	}
	state.Files["a.go"] = baseline.Fingerprint{SHA256: strings.Repeat("0", 64)}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	indexBefore := fileDigest(t, filepath.Join(root, "aoci.txt"))
	result, recoverErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if recoverErr == nil || result == nil || result.Status != draft.RunResolutionPending {
		t.Fatalf("a superseded state with Baseline drift must fail closed: err=%v result=%+v", recoverErr, result)
	}
	if fileDigest(t, filepath.Join(root, "aoci.txt")) != indexBefore {
		t.Fatal("Baseline drift rejection must not replay the old candidate")
	}
}

func TestEntriesRecoverIsIdempotentAndPreservesAuditEvidence(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, true)
	manifestBefore, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifestBefore.Applications) != 1 {
		t.Fatalf("夹具原失败Application不完整: err=%v manifest=%+v", err, manifestBefore)
	}
	activePath, transactionBefore := activeEntriesTransaction(t, root)
	eventsBefore, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("夹具Ledger损坏: %d", corrupt)
	}

	first, firstErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if firstErr != nil {
		t.Fatalf("首次恢复失败: %v result=%+v", firstErr, first)
	}
	indexAfterFirst := fileDigest(t, filepath.Join(root, "aoci.txt"))
	baselineAfterFirst := fileDigest(t, filepath.Join(root, ".aoci", "baseline.json"))
	second, secondErr := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if secondErr != nil || second == nil || second.Status != first.Status || !second.AlreadyResolved {
		t.Fatalf("重复恢复应返回同一机器终态: err=%v first=%+v second=%+v", secondErr, first, second)
	}
	if fileDigest(t, filepath.Join(root, "aoci.txt")) != indexAfterFirst ||
		fileDigest(t, filepath.Join(root, ".aoci", "baseline.json")) != baselineAfterFirst {
		t.Fatal("重复恢复不得写正式索引或Baseline")
	}
	manifestAfter, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifestAfter.Applications) != len(manifestBefore.Applications) ||
		len(manifestAfter.Resolutions) != 1 {
		t.Fatalf("重复恢复不得追加成功Application或重复终态: err=%v manifest=%+v", err, manifestAfter)
	}
	if !reflect.DeepEqual(manifestAfter.Applications, manifestBefore.Applications) {
		t.Fatalf("恢复不得篡改原Application失败证据: before=%+v after=%+v",
			manifestBefore.Applications, manifestAfter.Applications)
	}
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Fatalf("活跃事务应已转入history: %v", err)
	}
	archiveData, err := os.ReadFile(filepath.Join(
		root, filepath.FromSlash(manifestAfter.Resolutions[0].ArchivedRecoveryAsset),
	))
	if err != nil || !bytes.Equal(archiveData, transactionBefore) {
		t.Fatalf("归档必须原样保留旧事务字节: err=%v", err)
	}
	eventsAfter, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 || len(eventsAfter) != len(eventsBefore)+1 ||
		eventsAfter[len(eventsAfter)-1].Op != "entries_recover" ||
		eventsAfter[len(eventsAfter)-1].DraftRunID != runID ||
		eventsAfter[len(eventsAfter)-1].RecoveryStatus != draft.RunResolutionSuperseded {
		t.Fatalf("恢复Ledger必须唯一且可关联旧run: before=%d after=%+v corrupt=%d", len(eventsBefore), eventsAfter, corrupt)
	}
	if !reflect.DeepEqual(eventsAfter[:len(eventsBefore)], eventsBefore) {
		t.Fatal("恢复只能追加Ledger，不得改写原失败事件")
	}
	ledgerPath := filepath.Join(root, ".aoci", "ledger.jsonl")
	rawLedger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(rawLedger), "\n"), "\n")
	if len(lines) != len(eventsAfter) {
		t.Fatalf("夹具Ledger行数异常: lines=%d events=%d", len(lines), len(eventsAfter))
	}
	if err := os.WriteFile(
		ledgerPath, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	third, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err != nil || third == nil || !third.AlreadyResolved {
		t.Fatalf("重试应从Manifest机器终态补齐缺失Ledger副作用: err=%v result=%+v", err, third)
	}
	restoredEvents, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 || len(restoredEvents) != len(eventsAfter) ||
		!reflect.DeepEqual(restoredEvents[:len(eventsBefore)], eventsBefore) {
		t.Fatalf("补齐Ledger不得影响原审计前缀: %+v corrupt=%d", restoredEvents, corrupt)
	}
}

func TestEntriesRecoverRejectsOtherPendingGovernanceAsset(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, false)
	pendingPath := filepath.Join(root, ".aoci", "transactions", "remove-unknown.json")
	if err := os.WriteFile(pendingPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := recoverEntriesRun(root, runID, ledger.SourceHuman)
	if err == nil || result == nil || result.Status != draft.RunResolutionPending {
		t.Fatalf("其他未决治理资产必须fail-closed: err=%v result=%+v", err, result)
	}
}

func TestTamperedArchivedEvidenceRestoresPendingState(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, true)
	if _, err := recoverEntriesRun(root, runID, ledger.SourceHuman); err != nil {
		t.Fatal(err)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(
		root, filepath.FromSlash(manifest.Resolutions[0].ArchivedRecoveryAsset),
	)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, append(archiveData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != runID {
		t.Fatalf("归档原始证据被改写后必须重新fail-closed: run=%q err=%v", pending, err)
	}
}

func TestTamperedRecoveredArchiveOverridesAppliedAt(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	if _, err := recoverEntriesRun(root, runID, ledger.SourceHuman); err != nil {
		t.Fatal(err)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || manifest.AppliedAt == "" || len(manifest.Resolutions) != 1 {
		t.Fatalf("fixture has no recovered terminal state: err=%v manifest=%+v", err, manifest)
	}
	archivePath := filepath.Join(
		root, filepath.FromSlash(manifest.Resolutions[0].ArchivedRecoveryAsset),
	)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, append(archiveData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != runID {
		t.Fatalf("a corrupt recovered proof must override AppliedAt and fail closed: run=%q err=%v", pending, err)
	}
}

func TestTamperedGovernanceReceiptRestoresPendingState(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, false)
	if _, err := recoverEntriesRun(root, runID, ledger.SourceHuman); err != nil {
		t.Fatal(err)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || len(manifest.Resolutions) != 1 ||
		len(manifest.Resolutions[0].GovernanceReceipts) == 0 {
		t.Fatalf("fixture has no terminal governance receipt: err=%v manifest=%+v", err, manifest)
	}
	receiptPath := filepath.Join(
		root, ".aoci", "governance",
		"entries-"+manifest.Resolutions[0].GovernanceReceipts[0]+".json",
	)
	receiptData, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["completed_at"] = "2026-01-01T00:00:00Z"
	changed, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, append(changed, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != runID {
		t.Fatalf("a rewritten governance receipt must restore fail-closed pending state: run=%q err=%v", pending, err)
	}
}

func TestResolvedEntriesRunUnblocksCheckAndGuide(t *testing.T) {
	root, runID := buildEntriesBaselineIncompleteRun(t)
	applySupersedingGovernance(t, root, false)
	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = root
	flagJSON = true
	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})
	if pending, _ := draft.LatestPendingRun(root, draft.KindEntries); pending != runID {
		t.Fatalf("恢复前应识别旧pending run: %q", pending)
	}
	if _, err := runCheck(t, root); err == nil {
		t.Fatal("恢复前Check必须fail-closed")
	}
	if err := guardPendingEntriesForAgent(root); err == nil {
		t.Fatal("恢复前Guide/Plan不得报告aligned")
	}
	for _, command := range []*cobra.Command{
		newIndexAgentPlanCmd(), newIndexAgentGuideCmd(),
	} {
		command.SilenceUsage = true
		command.SilenceErrors = true
		if command.Name() == "guide" {
			command.SetArgs([]string{"--agent", "codex"})
		}
		var exitErr *ExitError
		if err := command.Execute(); !errors.As(err, &exitErr) || exitErr.Code != ExitInvalid {
			t.Fatalf("恢复前%s命令必须fail-closed: %v", command.Name(), err)
		}
	}

	recoverCommand := newEntriesRecoverCmd()
	recoverCommand.SilenceUsage = true
	recoverCommand.SilenceErrors = true
	recoverCommand.SetArgs([]string{runID})
	var recoverOutput bytes.Buffer
	recoverCommand.SetOut(&recoverOutput)
	if err := recoverCommand.Execute(); err != nil {
		t.Fatalf("公开recover命令失败: %v\n%s", err, recoverOutput.String())
	}
	var recovered entriesRecoveryResult
	if err := json.Unmarshal(recoverOutput.Bytes(), &recovered); err != nil ||
		recovered.Status != draft.RunResolutionSuperseded {
		t.Fatalf("公开recover JSON结果不符: err=%v result=%+v", err, recovered)
	}
	if pending, err := draft.LatestPendingRun(root, draft.KindEntries); err != nil || pending != "" {
		t.Fatalf("机器终态不应继续pending: run=%q err=%v", pending, err)
	}
	flagJSON = false
	if output, err := runCheck(t, root); err != nil || !strings.Contains(output, "可提交") {
		t.Fatalf("恢复后Check应通过: err=%v\n%s", err, output)
	}
	flagJSON = true
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(root, cfg, doc, indexPath)
	if err != nil || plan.Stage != agentPlanStageAligned {
		t.Fatalf("恢复后Guide/Plan应aligned: err=%v plan=%+v", err, plan)
	}
	planCommand := newIndexAgentPlanCmd()
	planCommand.SilenceUsage = true
	planCommand.SilenceErrors = true
	var planOutput bytes.Buffer
	planCommand.SetOut(&planOutput)
	if err := planCommand.Execute(); err != nil {
		t.Fatalf("恢复后Plan命令失败: %v\n%s", err, planOutput.String())
	}
	var commandPlan agentPlan
	if err := json.Unmarshal(planOutput.Bytes(), &commandPlan); err != nil ||
		commandPlan.Stage != agentPlanStageAligned {
		t.Fatalf("恢复后Plan命令未报aligned: err=%v plan=%+v", err, commandPlan)
	}
	guideCommand := newIndexAgentGuideCmd()
	guideCommand.SilenceUsage = true
	guideCommand.SilenceErrors = true
	guideCommand.SetArgs([]string{"--agent", "codex"})
	var guideOutput bytes.Buffer
	guideCommand.SetOut(&guideOutput)
	if err := guideCommand.Execute(); err != nil {
		t.Fatalf("恢复后Guide命令失败: %v\n%s", err, guideOutput.String())
	}
	var guide agentGuide
	if err := json.Unmarshal(guideOutput.Bytes(), &guide); err != nil ||
		guide.Plan == nil || guide.Plan.Stage != agentPlanStageAligned {
		t.Fatalf("恢复后Guide命令未报aligned: err=%v guide=%+v", err, guide)
	}
}
