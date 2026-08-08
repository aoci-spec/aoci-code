// R60-D.1人工Entries Apply原子批量语义测试。
//
// 覆盖:
//   - 两条候选成功时正式索引整批落盘;
//   - 只产生update_entries_batch与entries_apply两条批次级Ledger;
//   - 不再产生逐条update_entry事件;
//   - 第二条规划冲突时第一条内存变换不得部分落盘;
//   - 原子失败须追加整批Rejected Application且不设置AppliedAt;
//   - 规划失败不得前移Baseline;
//   - CAS冲突时底座层落一条update_entries_batch失败事件
//     (R60-F.9-A1: result=conflict + fail_code=write_conflict)——
//     与entries_apply治理层拒绝事件分层并存,两层各记各的语义主体,
//     auto链路仅有底座层故底座失败落账不可省。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	manualAtomicOldA = "a.go[XUT5T]: F:旧职责甲 | R:- | A:- | S:-"
	manualAtomicOldB = "b.go[XUT5T]: F:旧职责乙 | R:- | A:- | S:-"

	manualAtomicNewA = "a.go[XUT5T]: F:原子新职责甲 | R:b.go | A:- | S:-"
	manualAtomicNewB = "b.go[XUT5T]: F:原子新职责乙 | R:a.go | A:- | S:-"
)

// blockBaselineBackupReplacement以非空目录占据备份目标。
// POSIX rename不能覆盖目录，Windows回退删除也不能移除非空目录，
// 因而两端都会在真实AtomicWrite备份阶段稳定失败。
func blockBaselineBackupReplacement(t *testing.T, root string) func() {
	t.Helper()
	backupPath := filepath.Join(root, ".aoci", "baseline.json.bak")
	if err := os.Mkdir(backupPath, 0o755); err != nil {
		t.Fatal(err)
	}
	blockerPath := filepath.Join(backupPath, "blocker")
	if err := os.WriteFile(blockerPath, []byte("block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := true
	return func() {
		if !blocked {
			return
		}
		blocked = false
		if err := os.Remove(blockerPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(backupPath); err != nil {
			t.Fatal(err)
		}
	}
}

// buildManualAtomicEntriesRepo构造不启用Host-Agent Plan防线的Endpoint草稿仓。
//
// 该夹具专门隔离人工Apply原子语义；Generation Plan漂移另由R60-D测试覆盖。
func buildManualAtomicEntriesRepo(
	t *testing.T,
) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(
		filepath.Join(root, ".aoci"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	indexText := "#====人工原子Apply测试索引====\n" +
		"#===头部索引===\n" +
		"#【系统】人工原子Apply测试仓\n" +
		"#A层级: X-程序入口\n" +
		"#B模块: UT-单元测试\n" +
		"#C重要度: 9核心 8高频 7业务 5常规 3辅助 1边缘\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"#S配额: " + machinecontract.NumericText().SQuotaDefaultCompact + "\n" +
		"#===头部索引完毕===\n\n" +
		"#代码索引\n" +
		"===" + filepath.ToSlash(root) + "/===\n" +
		manualAtomicOldA + "\n" +
		manualAtomicOldB + "\n\n" +
		"#代码索引完毕\n"

	manualAtomicWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)
	manualAtomicWriteFile(
		t,
		root,
		"a.go",
		"package demo\n",
	)
	manualAtomicWriteFile(
		t,
		root,
		"b.go",
		"package demo\n",
	)

	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = true
	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, _, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	runID, err := draft.NewRun(root)
	if err != nil {
		t.Fatal(err)
	}

	draftFiles := []string{
		entryDraftFileName("a.go"),
		entryDraftFileName("b.go"),
	}
	for position, content := range []string{
		manualAtomicNewA,
		manualAtomicNewB,
	} {
		if err := draft.WriteFile(
			root,
			runID,
			draftFiles[position],
			[]byte(content+"\n"),
		); err != nil {
			t.Fatal(err)
		}
	}

	manifest := &draft.Manifest{
		RunID:            runID,
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceEndpoint,
		Provider:         "test-endpoint",
		Entries: []draft.EntryStatus{
			{
				Path:         "a.go",
				Status:       "drafted",
				SourceSHA256: snapshot["a.go"].SHA256,
			},
			{
				Path:         "b.go",
				Status:       "drafted",
				SourceSHA256: snapshot["b.go"].SHA256,
			},
		},
		Files: draftFiles,
	}
	if err := draft.SaveManifest(
		root,
		manifest,
	); err != nil {
		t.Fatal(err)
	}

	return root, runID
}

func manualAtomicWriteFile(
	t *testing.T,
	root,
	rel,
	content string,
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
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func readManualAtomicIndex(
	t *testing.T,
	root string,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(root, "aoci.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runManualAtomicCheck(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root
	defer func() {
		flagRepo = oldRepo
	}()

	command := newEntriesCheckCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	err := command.RunE(
		command,
		[]string{runID},
	)
	return output.String(), err
}

func runManualAtomicApply(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root
	defer func() {
		flagRepo = oldRepo
	}()

	command := newEntriesApplyCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	err := command.RunE(
		command,
		[]string{runID},
	)
	return output.String(), err
}

func TestEntriesApplyUsesAtomicBatchPipeline(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(t)

	if output, err := runManualAtomicCheck(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"原子Apply前Check应成功: %v\n%s",
			err,
			output,
		)
	}

	output, err := runManualAtomicApply(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"两条干净候选应原子成功: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		output,
		"合计: 原子应用2 / 拒绝0",
	) {
		t.Fatalf(
			"输出缺少原子批次成功结论: %s",
			output,
		)
	}

	indexText := readManualAtomicIndex(
		t,
		root,
	)
	for _, expected := range []string{
		manualAtomicNewA,
		manualAtomicNewB,
	} {
		if !strings.Contains(
			indexText,
			expected,
		) {
			t.Fatalf(
				"正式索引缺少原子结果%q:\n%s",
				expected,
				indexText,
			)
		}
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Applications) != 1 ||
		manifest.Applications[0].Applied != 2 ||
		manifest.Applications[0].Rejected != 0 ||
		manifest.AppliedAt == "" {
		t.Fatalf(
			"成功Application审计不符: %+v",
			manifest,
		)
	}

	events, _ := ledger.Recent(root, 50)
	updateEntryCount := 0
	batchCount := 0
	applyCount := 0

	for _, event := range events {
		switch event.Op {
		case "update_entry":
			updateEntryCount++
		case "update_entries_batch":
			if event.Source == ledger.SourceHuman &&
				event.PathsCount == 2 &&
				event.AppliedCount == 2 {
				batchCount++
			}
		case "entries_apply":
			if event.Source == ledger.SourceHuman &&
				event.PathsCount == 2 &&
				event.AppliedCount == 2 &&
				event.RejectedCount == 0 {
				applyCount++
			}
		}
	}

	if updateEntryCount != 0 ||
		batchCount != 1 ||
		applyCount != 1 {
		t.Fatalf(
			"人工原子Apply Ledger不符: update_entry=%d batch=%d apply=%d events=%+v",
			updateEntryCount,
			batchCount,
			applyCount,
			events,
		)
	}

	baselineState, exists, err := baseline.Load(root)
	if err != nil ||
		!exists ||
		baselineState == nil {
		t.Fatalf(
			"原子成功后Baseline应存在: exists=%v err=%v",
			exists,
			err,
		)
	}
	for _, rel := range []string{
		"a.go",
		"b.go",
		"aoci.txt",
	} {
		if _, found := baselineState.Files[rel]; !found {
			t.Fatalf(
				"原子成功后Baseline缺少%s: %+v",
				rel,
				baselineState.Files,
			)
		}
	}
}

func TestEntriesApplyAtomicConflictWritesNoPartialBatch(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(t)

	if output, err := runManualAtomicCheck(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"制造冲突前Check应成功: %v\n%s",
			err,
			output,
		)
	}

	indexPath := filepath.Join(
		root,
		"aoci.txt",
	)
	indexBeforeConflict := readManualAtomicIndex(
		t,
		root,
	)
	tampered := strings.Replace(
		indexBeforeConflict,
		manualAtomicOldB,
		manualAtomicOldB+"\n"+manualAtomicOldB,
		1,
	)
	if tampered == indexBeforeConflict {
		t.Fatal(
			"未能构造第二条旧条目重复冲突",
		)
	}
	if err := os.WriteFile(
		indexPath,
		[]byte(tampered),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	baselinePath := filepath.Join(
		root,
		".aoci",
		"baseline.json",
	)
	baselineBeforeApply, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}

	output, err := runManualAtomicApply(
		t,
		root,
		runID,
	)

	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != exitCodeForFail(
			"write_conflict",
		) {
		t.Fatalf(
			"第二条冲突应整批write_conflict: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		output,
		"整批零写入",
	) ||
		!strings.Contains(
			err.Error(),
			"write_conflict",
		) {
		t.Fatalf(
			"冲突输出应明确原子拒绝和分类码: %v\n%s",
			err,
			output,
		)
	}

	indexAfter := readManualAtomicIndex(
		t,
		root,
	)
	if indexAfter != tampered {
		t.Fatalf(
			"原子规划失败后必须保留冲突现场:\n%s",
			indexAfter,
		)
	}
	if strings.Contains(
		indexAfter,
		"原子新职责甲",
	) {
		t.Fatal(
			"第二条冲突时第一条内存变换不得部分落盘",
		)
	}

	baselineAfterApply, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineAfterApply) !=
		string(baselineBeforeApply) {
		t.Fatal(
			"原子规划失败不得前移Baseline",
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Applications) != 1 {
		t.Fatalf(
			"真实原子Apply失败应追加一次Application: %+v",
			manifest,
		)
	}
	application := manifest.Applications[0]
	if application.Applied != 0 ||
		application.Rejected != 2 ||
		application.RejectKinds != "conflict" ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"冲突Application审计不符: %+v",
			manifest,
		)
	}

	events, _ := ledger.Recent(root, 50)
	updateEntryCount := 0
	batchFailCount := 0
	rejectedApplyCount := 0

	for _, event := range events {
		switch event.Op {
		case "update_entry":
			updateEntryCount++
		case "update_entries_batch":
			// A1判决: 底座CAS冲突必须落一条失败事件,
			// 且result与fail_code精确分类(不只计数,防形态漂移)。
			if event.Result == ledger.ResultConflict &&
				event.FailCode == "write_conflict" &&
				event.Source == ledger.SourceHuman {
				batchFailCount++
			}
		case "entries_apply":
			if event.Source == ledger.SourceHuman &&
				event.Result == ledger.ResultConflict &&
				event.AppliedCount == 0 &&
				event.RejectedCount == 2 &&
				event.RejectKinds == "conflict" {
				rejectedApplyCount++
			}
		}
	}

	if updateEntryCount != 0 ||
		batchFailCount != 1 ||
		rejectedApplyCount != 1 {
		t.Fatalf(
			"原子冲突Ledger不符(A1分层落账): update_entry=%d batch_fail=%d rejected_apply=%d events=%+v",
			updateEntryCount,
			batchFailCount,
			rejectedApplyCount,
			events,
		)
	}
}

func TestEntriesApplyBaselineFailureIsNotMarkedAppliedAndReplayRecovers(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	if output, err := runManualAtomicCheck(t, root, runID); err != nil {
		t.Fatalf("Baseline故障前Check应成功: %v\n%s", err, output)
	}
	unblockBaseline := blockBaselineBackupReplacement(t, root)
	output, applyErr := runManualAtomicApply(t, root, runID)
	var exitErr *ExitError
	if !errors.As(applyErr, &exitErr) || exitErr.Code != ExitInternal ||
		!strings.Contains(applyErr.Error(), "Baseline未完成") {
		t.Fatalf("人工Apply必须如实报告索引已写但Baseline失败: %v\n%s", applyErr, output)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AppliedAt != "" || len(manifest.Applications) != 1 ||
		manifest.Applications[0].RejectKinds != "baseline_incomplete" ||
		manifest.Applications[0].Applied != 2 {
		t.Fatalf("Baseline未完成不得伪造成功Application: %+v", manifest)
	}
	events, _ := ledger.Recent(root, 50)
	foundError := false
	for _, event := range events {
		if event.Op == "entries_apply" && event.RejectKinds == "baseline_incomplete" &&
			event.Result == ledger.ResultError {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("Baseline故障必须以error写入Ledger: %+v", events)
	}
	unblockBaseline()
	if output, err := runManualAtomicApply(t, root, runID); err != nil {
		t.Fatalf("重放同一批次应只修复Baseline并完成审计: %v\n%s", err, output)
	}
	manifest, err = draft.LoadManifest(root, runID)
	if err != nil || manifest.AppliedAt == "" || len(manifest.Applications) != 2 ||
		manifest.Applications[1].Applied != 0 || manifest.Applications[1].Recovered != 2 {
		t.Fatalf("受绑定重放未恢复人工Apply终态: err=%v manifest=%+v", err, manifest)
	}
	events, _ = ledger.Recent(root, 100)
	initialApplied, replayZero := false, false
	for _, event := range events {
		if event.Op != "entries_apply" || event.DraftRunID != runID {
			continue
		}
		if event.RejectKinds == "baseline_incomplete" && event.AppliedCount == 2 {
			initialApplied = true
		}
		if event.Result == ledger.ResultOK && event.AppliedCount == 0 && event.RecoveredCount == 2 {
			replayZero = true
		}
	}
	if !initialApplied || !replayZero {
		t.Fatalf("人工恢复Ledger必须区分首次写入与零写入重放: %+v", events)
	}
}

func TestEntriesApplyRejectsLegacyDraftWithoutSourceBinding(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	if output, err := runManualAtomicCheck(t, root, runID); err != nil {
		t.Fatalf("缺绑定反例前Check应成功: %v\n%s", err, output)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].SourceSHA256 = ""
	if err := draft.SaveManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	indexBefore := readManualAtomicIndex(t, root)
	_, applyErr := runManualAtomicApply(t, root, runID)
	var exitErr *ExitError
	if !errors.As(applyErr, &exitErr) || exitErr.Code != ExitInvalid ||
		!strings.Contains(applyErr.Error(), "source_sha256") {
		t.Fatalf("非预览Apply必须拒绝无源码绑定的旧草稿: %v", applyErr)
	}
	if readManualAtomicIndex(t, root) != indexBefore {
		t.Fatal("无源码绑定拒绝必须正式索引零写入")
	}
}

func TestManualApplicationAuditFailureIsLedgerError(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	previous := appendManualEntriesApplication
	appendManualEntriesApplication = func(string, string, draft.ApplicationRecord, bool) error {
		return errors.New("injected manual application audit failure")
	}
	t.Cleanup(func() { appendManualEntriesApplication = previous })
	auditErr := recordManualEntriesApplication(
		root, cfg, runID, "draft-hash", 2, 2, 0, 0, "", true, 0,
	)
	if auditErr == nil {
		t.Fatal("夹具必须触发Application审计失败")
	}
	events, _ := ledger.Recent(root, 50)
	found := false
	for _, event := range events {
		if event.Op == "entries_apply" && event.DraftRunID == runID &&
			event.Result == ledger.ResultError && event.RejectKinds == "application_audit" &&
			event.AppliedCount == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("人工Application审计失败必须保留真实写入数并记error: %+v", events)
	}
}
