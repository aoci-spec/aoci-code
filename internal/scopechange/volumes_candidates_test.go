// Issue #47: under Volumes v1 the candidate channel of a Scope Change edited
// the wrong document and the stale-source guard left a repository with no
// legal move.
//
// The planner reads cfg.IndexPath, which in Volumes v1 is the Root manifest
// with zero Entries. An Entry candidate was therefore projected as an insert
// into the manifest (corrupting the layout on Apply), a disposition matched
// nothing and vanished, and a policy-only candidate set was refused with
// managed_scope_index_source_stale for any index source whose bytes had moved
// since the Baseline. Maintain and update_entry refuse to write while
// desired ≠ active, so a Volumes repository with one stale source and one
// pending policy edit could neither activate the policy nor align the source.
//
// Pinned here: Volumes refuses entries, dispositions and header candidates
// before projecting anything; a stale index source no longer blocks a Volumes
// Scope Change and its old fingerprint is retained in the postimage Baseline so
// the source stays Stale for Maintain; Legacy keeps failing closed; a clean plan
// serializes exactly as before so no in-flight plan_id changes.
package scopechange

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// buildVolumesChangeFixture is buildChangeFixture in the Volumes v1 layout: a
// governed main.go with an Entry in the Code Volume, the three Volumes recorded
// in the Baseline the way scan records them, and one pending no-op exclude rule
// so desired ≠ active and a Scope Change is required.
func buildVolumesChangeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeChangeFixture(t, root, "main.go", "package sample\n")
	writeChangeFixture(t, root, "aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: volumes scope change fixture\n#Global-Invariants: -\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	writeChangeFixture(t, root, "aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	writeChangeFixture(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")

	cfg := config.DefaultConfig()
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &policy
	budget, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CognitionBudget = &budget
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		fingerprint.Role = machinecontract.ScopeRoleIndex
		files[rel] = fingerprint
	}
	value := baseline.NewBaseline(files)
	budgetIdentity, _ := cognitionbudget.Identity(budget)
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: policy.ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budget}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}

	// The policy edit that makes a Scope Change necessary: a rule matching a
	// path that never exists, so nothing but the source judgement is under test.
	cfg, err = config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	edited := *cfg.ManagedScope
	edited.Rules = append(append([]managedscope.Rule{}, edited.Rules...), managedscope.Rule{
		RuleID: "no-op-future", Action: machinecontract.ScopeRoleExclude, Pattern: "never-present.txt",
		PatternKind: machinecontract.ScopePatternFile, Reason: "fixture policy edit", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "scope-test", Order: 100, Enabled: true})
	normalized, err := managedscope.Normalize(edited)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &normalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func emptyCandidateSet() CandidateSet {
	return CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
}

func baselineFingerprint(t *testing.T, root, rel string) baseline.Fingerprint {
	t.Helper()
	value, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint, ok := value.Files[rel]
	if !ok {
		t.Fatalf("%s is not in the Baseline: %v", rel, value.Files)
	}
	return fingerprint
}

func TestVolumesStaleIndexSourceIsRetainedNotBlocked(t *testing.T) {
	root := buildVolumesChangeFixture(t)
	old := baselineFingerprint(t, root, "main.go")
	writeChangeFixture(t, root, "main.go", "package sample\n\nfunc Added() {}\n")
	live, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}

	preview, err := Build(root, authorizationTestTime, emptyCandidateSet())
	if err != nil {
		t.Fatalf("a policy-only change must plan over a stale source in Volumes, where no candidate can clear it: %v", err)
	}
	retained := []string{}
	for _, item := range preview.Plan.SourceStaleRetained {
		retained = append(retained, item.Path)
		if item.Path == "main.go" && item.SourceSHA256 != old.SHA256 {
			t.Fatalf("the report must carry the retained Baseline digest, got %s want %s", item.SourceSHA256, old.SHA256)
		}
	}
	if strings.Join(retained, ",") != "main.go" {
		t.Fatalf("the retained source must be reported, not silently accepted: %v", retained)
	}
	// The postimage Baseline keeps the fingerprint the Entry was bound to, so
	// Maintain still classifies the source Stale after Apply; the guard binds
	// the live bytes so a further edit before Apply is still a replay mismatch.
	if got := preview.Baseline.Files["main.go"].SHA256; got != old.SHA256 {
		t.Fatalf("postimage Baseline stamped the new bytes onto the old Entry: got %s want %s", got, old.SHA256)
	}
	var published baseline.Baseline
	if err := json.Unmarshal(preview.BaselinePostimage.PostimageBytes, &published); err != nil {
		t.Fatal(err)
	}
	if published.Files["main.go"].SHA256 != old.SHA256 {
		t.Fatalf("serialized postimage disagrees with the plan: %s", published.Files["main.go"].SHA256)
	}
	if preview.SourceGuard["main.go"].SHA256 != live.SHA256 {
		t.Fatalf("source guard must bind the bytes the plan was minted against: %s", preview.SourceGuard["main.go"].SHA256)
	}
	// One digest per path in the plan: the preserved object agrees with the
	// postimage Baseline, not with the live bytes the guard binds.
	preserved := ""
	for _, item := range preview.Plan.Preserved {
		if item.Path == "main.go" {
			preserved = item.SourceSHA256
		}
	}
	if preserved != old.SHA256 {
		t.Fatalf("preserved object must carry the retained digest: got %s want %s", preserved, old.SHA256)
	}
	if preview.IndexPostimage.PostimageSHA256 != preview.IndexPostimage.PreimageSHA256 {
		t.Fatal("a policy-only Volumes change must leave the Root manifest byte-identical")
	}
	if len(preview.Plan.EntryCreates)+len(preview.Plan.EntryUpdates)+len(preview.Plan.EntryRemoves) != 0 {
		t.Fatalf("no Entry may move through the manifest: %+v", preview.Plan)
	}

	prepared := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	approval, err := NewApproval(preview, "human@example.invalid", prepared)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	result, err := Apply(root, preview, approval)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if manifestAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(manifestAfter, manifestBefore) {
		t.Fatal("Apply rewrote the Root manifest")
	}
	if got := baselineFingerprint(t, root, "main.go").SHA256; got != old.SHA256 {
		t.Fatalf("active Baseline after Apply must still hold the Entry's digest: got %s want %s", got, old.SHA256)
	}
	if got := baselineFingerprint(t, root, "main.go").SHA256; got == live.SHA256 {
		t.Fatal("fixture precondition broken: old and live digests coincide")
	}
}

func TestVolumesCandidateSetWithEntryContentIsRefused(t *testing.T) {
	root := buildVolumesChangeFixture(t)
	live, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	entry := EntryCandidate{CandidateID: "c1", Path: "main.go", SourceSHA256: live.SHA256,
		NewEntry: "main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:-", ReviewStatus: ReviewStatusReviewed}
	disposition := EntryDisposition{Version: machinecontract.ScopeEntryDispositionV1, SourcePath: "main.go",
		CurrentEntrySHA256: entrySHA("main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic"),
		TargetRole:         machinecontract.ScopeRoleExclude, UniqueSemantics: []string{}, Disposition: DispositionNoUniqueSemantics,
		ReviewStatus: ReviewStatusReviewed, Reviewer: "model-reviewer"}
	header := &HeaderCandidate{CandidateID: "h1", CurrentHeaderSHA256: "", NewHeader: "# rewritten", ReviewStatus: ReviewStatusReviewed}
	for _, test := range []struct {
		name       string
		candidates CandidateSet
		names      string
	}{
		{"entries", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1, Entries: []EntryCandidate{entry}, Dispositions: []EntryDisposition{}}, "1 entries"},
		{"dispositions", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1, Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{disposition}}, "1 dispositions"},
		{"header", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1, Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}, Header: header}, "a header candidate"},
	} {
		_, err := Build(root, authorizationTestTime, test.candidates)
		if err == nil || !strings.Contains(err.Error(), "managed_scope_volumes_entry_candidates_unsupported") {
			t.Fatalf("%s: Volumes must refuse Entry content in a candidate set before projecting it: %v", test.name, err)
		}
		if !strings.Contains(err.Error(), test.names) || !strings.Contains(err.Error(), "aoci_maintain") {
			t.Fatalf("%s: the refusal must name what was carried and the path that authors Entries: %v", test.name, err)
		}
	}
	// Without Entry content the same fixture plans.
	if _, err := Build(root, authorizationTestTime, emptyCandidateSet()); err != nil {
		t.Fatalf("policy-only set must still plan: %v", err)
	}
}

func TestLegacyStaleIndexSourceStillBlocksWhileVolumesRetains(t *testing.T) {
	// The Legacy branch is unchanged: the candidate channel does edit its
	// document, so a stale source there still needs a candidate.
	root, candidates := buildChangeFixture(t)
	writeChangeFixture(t, root, "main.go", "package sample\n\nfunc Added() {}\n")
	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_index_source_stale") {
		t.Fatalf("Legacy must keep failing closed on a stale index source: %v", err)
	}
}

func TestPlanIdentityUnchangedWithoutStaleRetention(t *testing.T) {
	// source_stale_retained is omitempty for the same reason as
	// source_line_ending_only: a plan without retention serializes exactly as
	// before, so no in-flight plan_id changes on upgrade.
	root := buildVolumesChangeFixture(t)
	preview, err := Build(root, authorizationTestTime, emptyCandidateSet())
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Plan.SourceStaleRetained) != 0 {
		t.Fatalf("clean fixture must retain nothing: %+v", preview.Plan.SourceStaleRetained)
	}
	if bytes.Contains(mustMarshalPlan(t, preview.Plan), []byte("source_stale_retained")) {
		t.Fatal("an empty additive field must not enter the serialized plan")
	}
	recomputed, err := planIdentity(preview.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != preview.Plan.PlanID {
		t.Fatalf("plan identity is not reproducible: %s vs %s", recomputed, preview.Plan.PlanID)
	}
}

// Issue #46: a rejected candidate or disposition said only which path was
// wrong. The operator learned the required fields from types.go. The refusal
// now names the field and the position.
func TestCandidateAndDispositionRefusalsNameTheField(t *testing.T) {
	good := EntryCandidate{CandidateID: "c1", Path: "main.go", SourceSHA256: "x", NewEntry: "y", ReviewStatus: ReviewStatusReviewed}
	for _, test := range []struct {
		name   string
		values []EntryCandidate
		want   []string
	}{
		{"empty candidate_id", []EntryCandidate{{Path: "main.go", ReviewStatus: ReviewStatusReviewed}}, []string{"main.go", "entries[0]", "candidate_id is empty"}},
		{"review_status", []EntryCandidate{good, {CandidateID: "c2", Path: "other.go", ReviewStatus: "draft"}}, []string{"other.go", "entries[1]", `review_status must be "reviewed"`}},
		{"duplicate candidate_id", []EntryCandidate{good, {CandidateID: "c1", Path: "other.go", ReviewStatus: ReviewStatusReviewed}}, []string{"entries[1]", "candidate_id repeats"}},
		{"path form", []EntryCandidate{{CandidateID: "c1", Path: "./main.go", ReviewStatus: ReviewStatusReviewed}}, []string{"entries[0]", "path must be"}},
		{"duplicate path", []EntryCandidate{good, {CandidateID: "c2", Path: "main.go", ReviewStatus: ReviewStatusReviewed}}, []string{"managed_scope_entry_candidate_duplicate", "entries[1]"}},
	} {
		_, err := candidateMap(test.values)
		if err == nil {
			t.Fatalf("%s: accepted", test.name)
		}
		for _, want := range test.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %q does not name %q", test.name, err.Error(), want)
			}
		}
	}
	if _, err := candidateMap([]EntryCandidate{good}); err != nil {
		t.Fatalf("a well-formed candidate must pass: %v", err)
	}
	goodDisposition := EntryDisposition{Version: machinecontract.ScopeEntryDispositionV1, SourcePath: "a.go",
		CurrentEntrySHA256: "d", TargetRole: machinecontract.ScopeRoleObserve, UniqueSemantics: []string{},
		Disposition: DispositionNoUniqueSemantics, ReviewStatus: ReviewStatusReviewed, Reviewer: "r"}
	for _, test := range []struct {
		name   string
		mutate func(*EntryDisposition)
		want   []string
	}{
		{"version", func(v *EntryDisposition) { v.Version = "" }, []string{"a.go", "dispositions[0]", "version must be"}},
		{"current_entry_sha256", func(v *EntryDisposition) { v.CurrentEntrySHA256 = "" }, []string{"current_entry_sha256 is empty"}},
		{"unique_semantics", func(v *EntryDisposition) { v.UniqueSemantics = nil }, []string{"unique_semantics must be present"}},
		{"reviewer", func(v *EntryDisposition) { v.Reviewer = "" }, []string{"reviewer is empty"}},
		{"review_status", func(v *EntryDisposition) { v.ReviewStatus = "pending" }, []string{`review_status must be "reviewed"`}},
	} {
		value := goodDisposition
		test.mutate(&value)
		_, err := dispositionMap([]EntryDisposition{value})
		if err == nil {
			t.Fatalf("%s: accepted", test.name)
		}
		for _, want := range test.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %q does not name %q", test.name, err.Error(), want)
			}
		}
	}
	if _, err := dispositionMap([]EntryDisposition{goodDisposition}); err != nil {
		t.Fatalf("a well-formed disposition must pass: %v", err)
	}
}
