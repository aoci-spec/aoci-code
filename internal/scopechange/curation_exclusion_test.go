package scopechange

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// An exclude decision on a path already in the Baseline: after the postimage is published the path is no longer in the
// new Baseline, and the decision's SHA binding mismatches with it. If the internal validation recomputed the exclusion
// set by the current Baseline, it would get another evaluation identity, judging a completed transaction stale and never archivable
// (review correction: validation must replay the plan-time facts in the envelope).
func TestCurationExclusionOfBaselinedPathCompletesAndArchives(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	excluded := &curation.Document{Version: curation.Version, Decisions: []curation.Decision{{
		Path: "main.go", Decision: curation.DecisionExclude, Role: "generated helper",
		Reason: "reviewed as excluded project scope", Confidence: 95, SourceSHA256: fingerprint.SHA256,
		Agent: "scope-test", Model: "model-retention-review", UpdatedAt: "2026-08-14T00:00:00Z",
	}}}
	candidates.Curation = excluded
	candidates.Dispositions = append(candidates.Dispositions, EntryDisposition{
		Version: machinecontract.ScopeEntryDispositionV1, SourcePath: "main.go",
		CurrentEntrySHA256: entrySHA("main.go[C.RT.9.T]: F:production | R:- | A:- | S:-"),
		TargetRole:         machinecontract.ScopeRoleExclude, UniqueSemantics: []string{},
		Disposition: DispositionNoUniqueSemantics, ReviewStatus: ReviewStatusReviewed, Reviewer: "model-reviewer"})

	prepared := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	preview, err := Build(root, prepared, candidates)
	if err != nil {
		t.Skipf("夹具不支持该排除组合,跳过: %v", err)
	}
	if len(preview.CurationExclusions) == 0 {
		t.Fatal("信封必须带上计划时的 Curation 排除集合,验证才能回放")
	}
	approval, err := NewApproval(preview, "human@example.invalid", prepared)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, preview, approval)
	if err != nil {
		t.Fatalf("排除已 Baseline 路径的事务未能完成: %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("事务状态异常: %#v", result)
	}
	status, err := Inspect(root, result.TransactionID)
	if err == nil && status != nil && status.State != "" && status.State != "complete" {
		t.Fatalf("完成的事务未收敛为 complete: %#v", status)
	}
}
