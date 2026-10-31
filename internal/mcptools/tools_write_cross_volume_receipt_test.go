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

// A cross-volume batch in receipt mode: take a real code+database machine batch, inject the interruption "Code volume written,
// Database volume failed", and resubmit the same batch as it is.
//
// The recovery semantics require a retry of the same batch to go on and finish the remaining volumes; the commit loop already skips volumes
// at the postimage. allowPostimage used to demand "every asset at the postimage", which a partial state never satisfies,
// so the resubmission stuck on code_candidate_plan_stale (review correction).
func TestCrossVolumeReceiptBatchRecoversFromPartialWrite(t *testing.T) {
	root := databaseCognitionWriteFixture(t, []string{"users"})
	// The fixture's default Code volume entries carry legacy bare relations; the cross-volume write tests all use the R:- volume instead.
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	rebaselineVolumeFixture(t, root)
	// Only after the volume baseline is fixed is source drift manufactured, which yields one Code update candidate.
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
			// A Code candidate carries no batch_id of its own; the batch is
			// code_plan.batch_id, as aoci_update_entry.code_batch_id names it.
			items = append(items, AtomicUpdateItem{Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
				CandidateID: candidate.CandidateID, BatchID: maintain.CodePlan.BatchID,
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
