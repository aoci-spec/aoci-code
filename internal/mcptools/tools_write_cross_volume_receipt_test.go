package mcptools

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// 收据模式的跨卷批次: 取一份真实的 code+database 机器批次,注入"写完 Code 卷、
// Database 卷失败"的中断,然后原样重交同一批次。
//
// 恢复语义要求同批重试能接着写完剩余卷 —— 提交环本就会跳过已到 postimage 的
// 卷。此前 allowPostimage 要求"全部资产都到 postimage",部分状态永远不满足,
// 重交会卡在 code_candidate_plan_stale 上(审查修正)。
func TestCrossVolumeReceiptBatchRecoversFromPartialWrite(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	// 夹具默认的 Code 卷条目带遗留裸关系,跨卷写测试统一改用 R:- 的卷。
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	rebaselineVolumeFixture(t, root)
	// 卷基线固定后再制造源码漂移,才会得到一个 Code 更新候选。
	writeVolumeTestFile(t, root, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")

	session := connectMCPClient(t, root)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeAll})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.CodePlan == nil || maintain.DatabasePlan == nil {
		t.Fatalf("夹具未同时产生 Code 与 Database 机器批次: %s", raw)
	}
	items := make([]AtomicUpdateItem, 0, len(maintain.Candidates))
	for _, candidate := range maintain.Candidates {
		switch candidate.Domain {
		case cognition.ScopeCode:
			items = append(items, AtomicUpdateItem{Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
				CandidateID: candidate.CandidateID, BatchID: candidate.BatchID,
				NewEntry: filepath.Base(candidate.Path) +
					"[CD5S]: F:run the fixture main entry point | R:- | A:main | S:Keep execution deterministic"})
		case cognition.ScopeDatabase:
			items = append(items, AtomicUpdateItem{ObjectRef: candidate.ObjectRef,
				CandidateID: candidate.CandidateID, BatchID: candidate.BatchID,
				NewEntry: modelAuthoredDatabaseTestEntry(candidate.ObjectRef)})
		}
	}
	if len(items) != 2 {
		t.Fatalf("期望恰好一个 Code 候选和一个 Database 候选,实际 %d 个", len(items))
	}

	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if filepath.Base(target) == "aoci.database.txt" {
			return errors.New("simulated Database Volume write failure")
		}
		return original(target, data, expected)
	}
	interrupted, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || interrupted == nil || interrupted.BaselineComplete ||
		!strings.Contains(interrupted.BaselineNote, "recovery_required") {
		t.Fatalf("跨卷中断未按可恢复状态返回: outcome=%#v fail=%+v", interrupted, fail)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "run the fixture main entry point") {
		t.Fatal("故障注入未造出 已写Code卷未写Database卷 的部分状态")
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || !pending {
		t.Fatalf("部分状态缺少恢复证据: pending=%v err=%v", pending, err)
	}

	recovered, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete {
		t.Fatalf("同批重交未能滚动完成: outcome=%#v fail=%+v", recovered, fail)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.database.txt"), "database://primary/public/users") &&
		!strings.Contains(volumeFileText(t, root, "aoci.database.txt"), "users[") {
		t.Fatal("重交后 Database 卷仍未写入")
	}
	stillPending, err := UpdateEntriesAtomicRecoveryPending(root, items)
	if err != nil || stillPending {
		t.Fatalf("完成后恢复证据应被收敛: pending=%v err=%v", stillPending, err)
	}
}

func rebaselineVolumeFixture(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
}
