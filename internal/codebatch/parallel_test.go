package codebatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
)

func parallelIdentities(preimage []byte) ParallelIdentities {
	return ParallelIdentities{CompositeIdentity: strings.Repeat("a", 64), ScopePolicyIdentity: strings.Repeat("b", 64),
		CodeVolumePath: "aoci.code.txt", CodeVolumeSHA256: BytesSHA256(preimage),
		RootSHA256: strings.Repeat("d", 64), MetaSHA256: strings.Repeat("e", 64)}
}

func parallelEntryLine(path string) string {
	base := filepath.Base(path)
	return base + "[CD5T]: F:Provides the fixture unit " + strings.TrimSuffix(base, ".go") + " | R:- | A:- | S:-"
}

// seedVolume writes the Entries for paths through the editor the replay uses,
// so the preimage is byte for byte what a governed write would have left.
func seedVolume(t *testing.T, repoRoot string, paths ...string) []byte {
	t.Helper()
	text := "#AOCI-CODE-VOLUME: 1\n"
	for _, path := range paths {
		next, err := index.InsertEntry(text, path, parallelEntryLine(path), repoRoot)
		if err != nil {
			t.Fatal(err)
		}
		text = next
	}
	return []byte(text)
}

func createOperation(path string) AppliedOperation {
	return AppliedOperation{Path: path, Change: "create", NewEntry: parallelEntryLine(path)}
}

func appliedReceipt(plan ParallelPlan, batchID string, order int, pre, post []byte, operations ...AppliedOperation) AppliedReceipt {
	return AppliedReceipt{Version: AppliedReceiptVersion, LogicalPlan: plan.LogicalPlan, BatchID: batchID, Order: order,
		PreSHA256: BytesSHA256(pre), PostSHA256: BytesSHA256(post), Operations: operations}
}

func submissionsOf(plan Plan) []Submission {
	submissions := make([]Submission, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		submissions = append(submissions, Submission{ObjectRef: candidate.ObjectRef,
			CandidateID: candidate.CandidateID, SourceSHA256: candidate.SourceSHA256})
	}
	return submissions
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Locks: the plan record round-trips with its preimage, every field the
// sibling is judged by is validated on write and read, an interrupted start
// (two records) or a corrupt record reads as no plan, and completion drops
// the record, the preimage, and the applied receipts together.
func TestParallelPlanRecordRoundTripsAndFailsClosedOnCorruption(t *testing.T) {
	root := t.TempDir()
	preimage := seedVolume(t, root, "pkg/a.go", "pkg/b.go")
	ids := parallelIdentities(preimage)
	plan := NewParallelPlan(ids)
	if plan.LogicalPlan != LogicalPlanID(ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256) || plan.PreimageSHA256 != ids.CodeVolumeSHA256 {
		t.Fatalf("new plan binds the identities: %#v", plan)
	}
	if loaded, err := LoadParallelPlan(root); err != nil || loaded != nil {
		t.Fatalf("no record reads as no plan: %v %v", loaded, err)
	}
	if err := SaveParallelPlan(root, plan, []byte("other bytes")); errorCode(err) != "code_parallel_plan_preimage_mismatch" {
		t.Fatalf("preimage must match the plan digest: %v", err)
	}
	if err := SaveParallelPlan(root, plan, preimage); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadParallelPlan(root)
	if err != nil || loaded == nil || loaded.LogicalPlan != plan.LogicalPlan || len(loaded.Siblings) != 0 {
		t.Fatalf("record round-trips: %#v %v", loaded, err)
	}
	if bytes, err := LoadPreimage(root, loaded); err != nil || string(bytes) != string(preimage) {
		t.Fatalf("preimage round-trips: %v", err)
	}
	batchA, batchB := strings.Repeat("1", 64), strings.Repeat("2", 64)
	plan.Siblings = []string{batchA, batchB}
	plan.Delivered = []string{batchA}
	if err := UpdateParallelPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadParallelPlan(root)
	if err != nil || loaded == nil || !loaded.HasSibling(batchA) || !loaded.HasSibling(batchB) || loaded.HasSibling(strings.Repeat("3", 64)) || len(loaded.Delivered) != 1 {
		t.Fatalf("siblings and deliveries round-trip: %#v %v", loaded, err)
	}
	if !loaded.PlanHolds(ids) {
		t.Fatal("the plan holds against the identities it was issued against")
	}
	moved := ids
	moved.MetaSHA256 = strings.Repeat("f", 64)
	var none *ParallelPlan
	if loaded.PlanHolds(moved) || none.PlanHolds(ids) {
		t.Fatal("a changed Meta or no plan does not hold")
	}

	for name, broken := range map[string]func(ParallelPlan) ParallelPlan{
		"version":            func(p ParallelPlan) ParallelPlan { p.Version = "other/v1"; return p },
		"id":                 func(p ParallelPlan) ParallelPlan { p.PreimageSHA256 = strings.Repeat("9", 64); return p },
		"duplicate sibling":  func(p ParallelPlan) ParallelPlan { p.Siblings = []string{batchA, batchA}; return p },
		"unknown delivery":   func(p ParallelPlan) ParallelPlan { p.Delivered = []string{strings.Repeat("3", 64)}; return p },
		"volume path":        func(p ParallelPlan) ParallelPlan { p.CodeVolumePath = "../aoci.code.txt"; return p },
		"database identity":  func(p ParallelPlan) ParallelPlan { p.DatabaseSHA256 = "short"; return p },
		"malformed sibling":  func(p ParallelPlan) ParallelPlan { p.Siblings = []string{"batch"}; return p },
		"composite identity": func(p ParallelPlan) ParallelPlan { p.CompositeIdentity = ""; return p },
	} {
		if err := UpdateParallelPlan(root, broken(plan)); err == nil {
			t.Fatalf("%s: an invalid record is refused before writing", name)
		}
	}

	// A second record is an interrupted start: neither is trusted.
	second := plan
	second.CompositeIdentity = strings.Repeat("c", 64)
	second.LogicalPlan = LogicalPlanID(second.CompositeIdentity, second.ScopePolicyIdentity, second.CodeVolumePath, second.PreimageSHA256)
	if err := writeParallelPlan(root, second); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadParallelPlan(root); err != nil || loaded != nil {
		t.Fatalf("two records read as no plan: %v %v", loaded, err)
	}
	if err := os.Remove(parallelPlanPath(root, second.LogicalPlan)); err != nil {
		t.Fatal(err)
	}
	// Trailing JSON, an unknown field, and a duplicate key are corruption.
	recordPath := parallelPlanPath(root, plan.LogicalPlan)
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, corrupt := range map[string][]byte{
		"trailing":      append(append([]byte{}, record...), []byte("{}\n")...),
		"unknown field": []byte(strings.Replace(string(record), "\"version\"", "\"extra\": 1, \"version\"", 1)),
		"duplicate key": []byte(strings.Replace(string(record), "\"version\"", "\"version\": \"x\", \"version\"", 1)),
	} {
		if err := os.WriteFile(recordPath, corrupt, 0o644); err != nil {
			t.Fatal(err)
		}
		if loaded, err := LoadParallelPlan(root); err != nil || loaded != nil {
			t.Fatalf("%s: a corrupt record reads as no plan: %v %v", name, loaded, err)
		}
	}
	if err := os.WriteFile(recordPath, record, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preimagePath(root, plan.LogicalPlan), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPreimage(root, &plan); errorCode(err) != "code_parallel_plan_preimage_mismatch" {
		t.Fatalf("a drifted preimage is refused: %v", err)
	}
	if err := SaveApplied(root, appliedReceipt(plan, batchA, 1, preimage, []byte("post"), createOperation("pkg/c.go"))); err != nil {
		t.Fatal(err)
	}
	if err := RemoveParallelPlans(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parallelDirectory(root))
	if err != nil || len(entries) != 0 {
		t.Fatalf("completion drops the record, preimage, and applied receipts: %v %v", entries, err)
	}
	if _, err := LoadPreimage(root, &plan); errorCode(err) != "code_parallel_plan_preimage_missing" {
		t.Fatalf("no preimage after removal: %v", err)
	}
	if err := RemoveParallelPlans(filepath.Join(root, "absent")); err != nil {
		t.Fatalf("removing from a repository without the directory is a no-op: %v", err)
	}
}

// Locks: an applied receipt is written once (an identical replay is
// idempotent, a different record for the same batch is a conflict), is
// validated field by field, loads in apply order with no gap, and the replay
// of the preimage plus the applied operations reproduces the Volume only when
// every pre and post digest in the chain is honest.
func TestAppliedSiblingsReplayOntoThePreimage(t *testing.T) {
	root := t.TempDir()
	preimage := seedVolume(t, root, "pkg/a.go", "pkg/b.go")
	ids := parallelIdentities(preimage)
	plan := NewParallelPlan(ids)
	batch1, batch2, batch3 := strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)
	plan.Siblings = []string{batch1, batch2, batch3}
	if err := SaveParallelPlan(root, plan, preimage); err != nil {
		t.Fatal(err)
	}
	if proven, err := ReplayProves(root, root, &plan, ids.CodeVolumeSHA256); err != nil || !proven {
		t.Fatalf("with nothing applied the preimage is the current Volume: %v %v", proven, err)
	}
	if proven, err := ReplayProves(root, root, &plan, BytesSHA256([]byte("other"))); err != nil || proven {
		t.Fatalf("an unexplained Volume is not proven: %v %v", proven, err)
	}

	post1, err := index.InsertEntry(string(preimage), "pkg/c.go", parallelEntryLine("pkg/c.go"), root)
	if err != nil {
		t.Fatal(err)
	}
	first := appliedReceipt(plan, batch1, 1, preimage, []byte(post1), createOperation("pkg/c.go"))
	if err := SaveApplied(root, first); err != nil {
		t.Fatal(err)
	}
	if err := SaveApplied(root, first); err != nil {
		t.Fatalf("an identical record is an idempotent replay: %v", err)
	}
	conflicting := first
	conflicting.Order = 2
	if err := SaveApplied(root, conflicting); errorCode(err) != "code_parallel_applied_conflict" {
		t.Fatalf("a different record for the same batch is a conflict: %v", err)
	}
	if applied := AppliedBatchIDs(root, &plan); !applied[batch1] || applied[batch2] {
		t.Fatalf("applied siblings: %v", applied)
	}
	if proven, err := ReplayProves(root, root, &plan, BytesSHA256([]byte(post1))); err != nil || !proven {
		t.Fatalf("the replayed postimage is the current Volume: %v %v", proven, err)
	}
	if proven, err := ReplayProves(root, root, &plan, ids.CodeVolumeSHA256); err != nil || proven {
		t.Fatalf("once a sibling applied, the preimage is no longer current: %v %v", proven, err)
	}

	oldLine := parallelEntryLine("pkg/a.go")
	newLine := "a.go[CD5T]: F:Provides the revised fixture unit a | R:- | A:- | S:-"
	post2, err := index.ReplaceEntryForPath(post1, root, "pkg/a.go", oldLine, newLine)
	if err != nil {
		t.Fatal(err)
	}
	update := AppliedOperation{Path: "pkg/a.go", Change: "update", OldEntry: oldLine, NewEntry: newLine}
	if err := SaveApplied(root, appliedReceipt(plan, batch2, 2, []byte(post1), []byte(post2), update)); err != nil {
		t.Fatal(err)
	}
	applied, err := LoadApplied(root, &plan)
	if err != nil || len(applied) != 2 || applied[0].BatchID != batch1 || applied[1].BatchID != batch2 {
		t.Fatalf("applied receipts load in apply order: %#v %v", applied, err)
	}
	replayed, err := Replay(root, preimage, applied)
	if err != nil || string(replayed) != post2 {
		t.Fatalf("the replay reproduces the Volume byte for byte: %v", err)
	}
	if proven, err := ReplayProves(root, root, &plan, BytesSHA256([]byte(post2))); err != nil || !proven {
		t.Fatalf("two applied siblings prove the current Volume: %v %v", proven, err)
	}
	if _, err := Replay(root, []byte("not the preimage"), applied); errorCode(err) != "code_parallel_replay_preimage_mismatch" {
		t.Fatalf("a replay from other bytes fails: %v", err)
	}
	dishonest := append([]AppliedReceipt{}, applied...)
	dishonest[1].PostSHA256 = strings.Repeat("9", 64)
	if _, err := Replay(root, preimage, dishonest); errorCode(err) != "code_parallel_replay_postimage_mismatch" {
		t.Fatalf("a postimage the operations do not produce fails: %v", err)
	}
	broken := append([]AppliedReceipt{}, applied...)
	broken[1].Operations = []AppliedOperation{{Path: "pkg/zzz.go", Change: "update", OldEntry: "zzz.go[CD5T]: F:Absent | R:- | A:- | S:-", NewEntry: newLine}}
	if _, err := Replay(root, preimage, broken); errorCode(err) != "code_parallel_replay_failed" {
		t.Fatalf("an operation the editor refuses fails the replay: %v", err)
	}

	for name, invalid := range map[string]AppliedReceipt{
		"order":            appliedReceipt(plan, batch3, 0, []byte(post2), []byte("x"), createOperation("pkg/d.go")),
		"no operations":    appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x")),
		"update old entry": appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x"), AppliedOperation{Path: "pkg/a.go", Change: "update", NewEntry: newLine}),
		"create old entry": appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x"), AppliedOperation{Path: "pkg/d.go", Change: "create", OldEntry: oldLine, NewEntry: newLine}),
		"change":           appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x"), AppliedOperation{Path: "pkg/d.go", Change: "delete", NewEntry: newLine}),
		"path":             appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x"), AppliedOperation{Path: "../d.go", Change: "create", NewEntry: newLine}),
		"version":          {Version: "other/v1", LogicalPlan: plan.LogicalPlan, BatchID: batch3, Order: 3, PreSHA256: BytesSHA256([]byte(post2)), PostSHA256: BytesSHA256([]byte("x")), Operations: []AppliedOperation{createOperation("pkg/d.go")}},
	} {
		if err := SaveApplied(root, invalid); errorCode(err) != "code_parallel_applied_invalid" {
			t.Fatalf("%s: an invalid applied receipt is refused: %v", name, err)
		}
	}
	// A gap in the apply order and a receipt of another plan are corrupt records.
	if err := SaveApplied(root, appliedReceipt(plan, batch3, 4, []byte(post2), []byte("x"), createOperation("pkg/d.go"))); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadApplied(root, &plan); errorCode(err) != "code_parallel_applied_order_invalid" {
		t.Fatalf("a gap in the apply order fails: %v", err)
	}
	if _, err := ReplayProves(root, root, &plan, BytesSHA256([]byte(post2))); err == nil {
		t.Fatal("the replay proof reports the corrupt record")
	}
	if err := os.Remove(appliedPath(root, batch3)); err != nil {
		t.Fatal(err)
	}
	foreign := appliedReceipt(plan, batch3, 3, []byte(post2), []byte("x"), createOperation("pkg/d.go"))
	foreign.LogicalPlan = strings.Repeat("8", 64)
	if err := SaveApplied(root, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadApplied(root, &plan); errorCode(err) != "code_parallel_applied_invalid" {
		t.Fatalf("a receipt of another plan fails: %v", err)
	}
}

// Locks: a sibling is the leading free candidates in plan order, under the
// count cap and the byte budget, and nothing when every target is held.
func TestCutSiblingTakesTheLeadingFreeCandidates(t *testing.T) {
	ordered := codeCandidates(6)
	held := map[string]bool{"code:0000.go": true, "code:0002.go": true}
	got := CutSibling(ordered, held, 3, Budget{})
	if len(got) != 3 || got[0].ObjectRef != "code:0001.go" || got[1].ObjectRef != "code:0003.go" || got[2].ObjectRef != "code:0004.go" {
		t.Fatalf("free candidates in order under the cap: %v", got)
	}
	if got := CutSibling(ordered, held, 10, fixedSize(400)); len(got) != 2 {
		t.Fatalf("the byte budget cuts the prefix: %d", len(got))
	}
	everything := map[string]bool{}
	for _, candidate := range ordered {
		everything[candidate.ObjectRef] = true
	}
	if got := CutSibling(ordered, everything, 3, Budget{}); got != nil {
		t.Fatalf("nothing free cuts nothing: %v", got)
	}
}

// Locks: a sibling's Apply is accepted only while the plan holds and the
// current Volume is the preimage or a postimage the applied siblings replay
// to; an applied sibling, a partial submission, an ordinary batch, and a
// dropped plan are refused; the recovery path's own proof skips the replay.
func TestValidateParallelSubmissionJudgesTheReplayedVolume(t *testing.T) {
	root := t.TempDir()
	preimage := seedVolume(t, root, "pkg/a.go")
	ids := parallelIdentities(preimage)
	plan := NewParallelPlan(ids)
	all := codeCandidates(5)
	budget := Budget{Total: 24576}
	if _, err := BuildSiblingPlan(root, plan.LogicalPlan, ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, all[:2], 0, budget); err == nil {
		t.Fatal("a sibling needs a positive cap")
	}
	if _, err := BuildSiblingPlan(root, plan.LogicalPlan, ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, nil, 2, budget); err == nil {
		t.Fatal("a sibling needs candidates")
	}
	if _, err := BuildSiblingPlan(root, "plan", ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, all[:2], 2, budget); err == nil {
		t.Fatal("a sibling needs its logical plan")
	}
	firstCut := CutSibling(all, nil, 2, Budget{})
	siblingA, err := BuildSiblingPlan(root, plan.LogicalPlan, ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, firstCut, 2, budget)
	if err != nil {
		t.Fatal(err)
	}
	if siblingA.LogicalPlan != plan.LogicalPlan || siblingA.Included != 2 || siblingA.TransportBudgetBytes != 24576 || siblingA.TotalTargets != 5 {
		t.Fatalf("the sibling carries the logical plan and the budget: %#v", siblingA)
	}
	held := map[string]bool{}
	for _, candidate := range firstCut {
		held[candidate.ObjectRef] = true
	}
	siblingB, err := BuildSiblingPlan(root, plan.LogicalPlan, ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, CutSibling(all, held, 2, Budget{Bytes: 10, Size: func(Candidate) int { return 100 }}), 2, Budget{Bytes: 500})
	if err != nil {
		t.Fatal(err)
	}
	if siblingB.BatchID == siblingA.BatchID || siblingB.Included != 1 || siblingB.TransportBudgetBytes != 500 {
		t.Fatalf("the second sibling holds the next free target under the byte budget: %#v", siblingB)
	}
	plan.Siblings = []string{siblingA.BatchID, siblingB.BatchID}
	ordinary, err := BuildPlan(root, ids.CompositeIdentity, ids.ScopePolicyIdentity, ids.CodeVolumePath, ids.CodeVolumeSHA256, all, 200)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingA.BatchID, ids, submissionsOf(siblingA), false); errorCode(err) != "code_candidate_plan_stale" {
		t.Fatalf("a sibling without its plan on record is stale: %v", err)
	}
	if err := SaveParallelPlan(root, plan, preimage); err != nil {
		t.Fatal(err)
	}
	receipt, loaded, err := ValidateParallelSubmission(root, root, siblingA.BatchID, ids, submissionsOf(siblingA), false)
	if err != nil || receipt.LogicalPlan != plan.LogicalPlan || loaded == nil || loaded.LogicalPlan != plan.LogicalPlan {
		t.Fatalf("a complete sibling submission against the preimage is accepted: %v", err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, ordinary.BatchID, ids, submissionsOf(ordinary), false); errorCode(err) != "code_candidate_batch_not_parallel" {
		t.Fatalf("an ordinary batch is not a sibling: %v", err)
	}
	moved := ids
	moved.RootSHA256 = strings.Repeat("f", 64)
	if _, _, err := ValidateParallelSubmission(root, root, siblingA.BatchID, moved, submissionsOf(siblingA), false); errorCode(err) != "code_candidate_plan_stale" {
		t.Fatalf("a changed Root invalidates the sibling: %v", err)
	}
	unexplained := ids
	unexplained.CodeVolumeSHA256 = BytesSHA256([]byte("edited by hand"))
	if _, _, err := ValidateParallelSubmission(root, root, siblingA.BatchID, unexplained, submissionsOf(siblingA), false); errorCode(err) != "code_candidate_volume_preimage_stale" {
		t.Fatalf("a Volume no applied sibling explains is stale: %v", err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingA.BatchID, unexplained, submissionsOf(siblingA), true); err != nil {
		t.Fatalf("the recovery path's own proof skips the replay: %v", err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingB.BatchID, ids, submissionsOf(siblingB)[:0], false); errorCode(err) != "code_candidate_batch_incomplete" {
		t.Fatalf("a partial submission is refused: %v", err)
	}

	post1, err := index.InsertEntry(string(preimage), "0000.go", parallelEntryLine("0000.go"), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveApplied(root, appliedReceipt(plan, siblingA.BatchID, 1, preimage, []byte(post1), createOperation("0000.go"))); err != nil {
		t.Fatal(err)
	}
	advanced := ids
	advanced.CodeVolumeSHA256 = BytesSHA256([]byte(post1))
	if _, _, err := ValidateParallelSubmission(root, root, siblingB.BatchID, advanced, submissionsOf(siblingB), false); err != nil {
		t.Fatalf("the sibling that lands second meets the first's postimage: %v", err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingB.BatchID, ids, submissionsOf(siblingB), false); errorCode(err) != "code_candidate_volume_preimage_stale" {
		t.Fatalf("the preimage is no longer current once a sibling applied: %v", err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingA.BatchID, advanced, submissionsOf(siblingA), false); errorCode(err) != "code_candidate_batch_already_applied" {
		t.Fatalf("an applied sibling is not applied twice: %v", err)
	}
	if err := RemoveParallelPlans(root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateParallelSubmission(root, root, siblingB.BatchID, advanced, submissionsOf(siblingB), false); errorCode(err) != "code_candidate_plan_stale" {
		t.Fatalf("a completed plan's siblings are stale: %v", err)
	}
}

// Locks: a receipt's sources are current only while every target still has
// the exact bytes it was issued against.
func TestReceiptSourcesCurrentReadsTheTargetBytes(t *testing.T) {
	root := t.TempDir()
	content := []byte("package fixture\n")
	if err := os.WriteFile(filepath.Join(root, "0000.go"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := codeCandidates(2)
	candidates[0].SourceSHA256 = BytesSHA256(content)
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt", strings.Repeat("c", 64), candidates, 200)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if ReceiptSourcesCurrent(root, receipt) {
		t.Fatal("a missing target source is not current")
	}
	if err := os.WriteFile(filepath.Join(root, "0001.go"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ReceiptSourcesCurrent(root, receipt) {
		t.Fatal("a target whose bytes moved is not current")
	}
	receipt.Targets = receipt.Targets[:1]
	if !ReceiptSourcesCurrent(root, receipt) {
		t.Fatal("exact bytes are current")
	}
}
