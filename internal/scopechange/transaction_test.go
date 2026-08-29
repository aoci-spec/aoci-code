package scopechange

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func writeChangeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScopeIdentitiesIgnoreAuditTimestampCopies(t *testing.T) {
	plan := Plan{Version: machinecontract.ManagedScopeChangePlanV2, PreparedAt: "2026-08-01T00:00:00Z", WriteSet: []string{"aoci.txt"}}
	firstPlanID, err := planIdentity(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PreparedAt = "2026-08-02T00:00:00Z"
	secondPlanID, err := planIdentity(plan)
	if err != nil || firstPlanID != secondPlanID {
		t.Fatalf("Plan identity must ignore its audit timestamp copy: first=%s second=%s err=%v", firstPlanID, secondPlanID, err)
	}

	preview := Preview{Version: machinecontract.ManagedScopeChangePreviewV2, Plan: plan, IndexPostimage: FormalImage{Path: "aoci.txt", PostimageSHA256: "candidate"}}
	firstPreviewID, err := previewIdentity(preview)
	if err != nil {
		t.Fatal(err)
	}
	preview.Plan.PreparedAt = "2099-01-01T00:00:00Z"
	secondPreviewID, err := previewIdentity(preview)
	if err != nil || firstPreviewID != secondPreviewID {
		t.Fatalf("Preview identity must ignore the nested audit timestamp copy: first=%s second=%s err=%v", firstPreviewID, secondPreviewID, err)
	}
	preview.IndexPostimage.PostimageSHA256 = "different-candidate"
	changedPreviewID, err := previewIdentity(preview)
	if err != nil || changedPreviewID == firstPreviewID {
		t.Fatal("Preview identity must continue binding projected formal bytes")
	}
}

func buildChangeFixture(t *testing.T) (string, CandidateSet) {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeChangeFixture(t, root, "main.go", "package sample\n")
	writeChangeFixture(t, root, "main_test.go", "package sample\n")
	indexText := "# test index\n===" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[A.IX.9.T]: F:index | R:- | A:- | S:-\n" +
		"main.go[C.RT.9.T]: F:production | R:- | A:- | S:-\n" +
		"main_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:-\n"
	writeChangeFixture(t, root, "aoci.txt", indexText)
	cfg := config.DefaultConfig()
	full := managedscope.DefaultPolicy(machinecontract.ScopeProfileFull)
	fullNormalized, err := managedscope.Normalize(full)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &fullNormalized
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	budgetNormalized, err := cognitionbudget.Normalize(budget)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CognitionBudget = &budgetNormalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, fullNormalized, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	value := baseline.NewBaseline(files)
	budgetIdentity, _ := cognitionbudget.Identity(budgetNormalized)
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	mainFingerprint, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	curationDocument := &curation.Document{Version: curation.Version, Decisions: []curation.Decision{{
		Path: "main.go", Decision: curation.DecisionInclude, Role: "production source", Reason: "reviewed project scope",
		Confidence: 100, SourceSHA256: mainFingerprint.SHA256, Agent: "scope-test", UpdatedAt: "2026-07-31T00:00:00Z",
	}}}
	if err := curation.Save(root, curationDocument); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	production := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	productionNormalized, err := managedscope.Normalize(production)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &productionNormalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	curationCandidate := *curationDocument
	curationCandidate.Decisions = append([]curation.Decision{}, curationDocument.Decisions...)
	curationCandidate.Decisions[0].Model = "model-retention-review"
	return root, CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Curation: &curationCandidate, Dispositions: []EntryDisposition{{Version: machinecontract.ScopeEntryDispositionV1,
			SourcePath: "main_test.go", CurrentEntrySHA256: entrySHA("main_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:-"),
			TargetRole: machinecontract.ScopeRoleObserve, UniqueSemantics: []string{},
			Disposition: DispositionNoUniqueSemantics, ReviewStatus: ReviewStatusReviewed, Reviewer: "model-reviewer"}}}
}

func buildApprovedPreview(t *testing.T, root string, candidates CandidateSet) (*Preview, *Approval) {
	t.Helper()
	prepared := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	preview, err := Build(root, prepared, candidates)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := NewApproval(preview, "human@example.invalid", prepared)
	if err != nil {
		t.Fatal(err)
	}
	return preview, approval
}

func TestScopeChangeRequiresApprovalAndAppliesOneGovernedPostimage(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	if !preview.Plan.Risk.LargeReduction || preview.Plan.Risk.EntryRemovalThreshold != 25 ||
		preview.Plan.Risk.EntryRemovalPercentThreshold != 25 {
		t.Fatalf("configured count/percentage reduction thresholds not enforced: %+v", preview.Plan.Risk)
	}
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineBefore, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if _, err := Apply(root, preview, nil); err == nil || err.Error() != "managed_scope_human_approval_required" {
		t.Fatalf("scope reduction did not require approval: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("unapproved Apply changed index")
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json")); !bytes.Equal(after, baselineBefore) {
		t.Fatal("unapproved Apply changed Baseline")
	}
	result, err := Apply(root, preview, approval)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.RecoveryAvailable {
		t.Fatalf("unexpected result: %+v", result)
	}
	indexAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if bytes.Contains(indexAfter, []byte("main_test.go[")) {
		t.Fatal("test Entry survived index to observe transition")
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists || baseline.EffectiveRole(value.Files["main_test.go"]) != machinecontract.ScopeRoleObserve {
		t.Fatalf("observe fingerprint not activated: %+v exists=%v err=%v", value, exists, err)
	}
	status, err := Inspect(root, preview.EnvelopeDigest[:24])
	if err != nil || status.State != "complete" || status.RecoveryAvailable {
		t.Fatalf("completed transaction status invalid: %+v err=%v", status, err)
	}
}

func TestObservedEvidenceCannotBeWashedByPolicyEdit(t *testing.T) {
	active := &baseline.Baseline{Files: map[string]baseline.Fingerprint{
		"tests/changed_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: fmt.Sprintf("%064d", 1)},
		"tests/removed_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: fmt.Sprintf("%064d", 2)},
		"src/main.go":           {Role: machinecontract.ScopeRoleIndex, SHA256: fmt.Sprintf("%064d", 3)},
	}}
	desired := map[string]baseline.Fingerprint{
		"tests/changed_test.go": {Role: machinecontract.ScopeRoleObserve, SHA256: fmt.Sprintf("%064d", 4)},
		"tests/new_test.go":     {Role: machinecontract.ScopeRoleObserve, SHA256: fmt.Sprintf("%064d", 5)},
		"src/main.go":           {Role: machinecontract.ScopeRoleIndex, SHA256: fmt.Sprintf("%064d", 3)},
	}
	roles := map[string]string{
		"tests/changed_test.go": machinecontract.ScopeRoleObserve,
		"tests/new_test.go":     machinecontract.ScopeRoleObserve,
		"src/main.go":           machinecontract.ScopeRoleIndex,
	}
	changes := observedEvidenceChanges(active, desired, roles, nil, false, true)
	if got, want := fmt.Sprint(changes), "[tests/changed_test.go tests/removed_test.go]"; got != want {
		t.Fatalf("policy transition washed or overclassified observe drift: got %s want %s", got, want)
	}
	changes = observedEvidenceChanges(active, desired, roles, nil, true, true)
	if got, want := fmt.Sprint(changes), "[tests/changed_test.go tests/new_test.go tests/removed_test.go]"; got != want {
		t.Fatalf("aligned policy missed observe drift: got %s want %s", got, want)
	}
}

func TestMissingIndexedSourceCanEnterReviewedExcludeTransition(t *testing.T) {
	active := &baseline.Baseline{Files: map[string]baseline.Fingerprint{
		"removed.go": {Role: machinecontract.ScopeRoleIndex, SHA256: fmt.Sprintf("%064d", 1)},
	}}
	if err := validateSourcesPresent(t.TempDir(), active, map[string]string{}); err != nil {
		t.Fatalf("missing indexed source must reach disposition and approval validation: %v", err)
	}
}

func applyProductionScopeFixture(t *testing.T) string {
	t.Helper()
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	if _, err := Apply(root, preview, approval); err != nil {
		t.Fatal(err)
	}
	return root
}

func addDesiredRule(t *testing.T, root string, rule managedscope.Rule) {
	t.Helper()
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, rule)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScopeChangeSupportsEveryRoleTransition(t *testing.T) {
	t.Run("index_to_exclude", func(t *testing.T) {
		root, candidates := buildChangeFixture(t)
		addDesiredRule(t, root, managedscope.Rule{RuleID: "user-exclude-test", Action: machinecontract.ScopeRoleExclude,
			Pattern: "main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "fixture is fully excluded",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
		candidates.Dispositions[0].TargetRole = machinecontract.ScopeRoleExclude
		preview, err := Build(root, "2026-07-31T00:20:00Z", candidates)
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Plan.IndexRemoved) != 1 || len(preview.Plan.ExcludeAdded) != 1 || len(preview.Plan.EntryRemoves) != 1 {
			t.Fatalf("index to exclude plan incomplete: %+v", preview.Plan)
		}
	})

	t.Run("observe_to_index", func(t *testing.T) {
		root := applyProductionScopeFixture(t)
		addDesiredRule(t, root, managedscope.Rule{RuleID: "user-index-test", Action: machinecontract.ScopeRoleIndex,
			Pattern: "main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "test owns a formal contract",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
		fingerprint, err := baseline.HashFile(filepath.Join(root, "main_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		candidates := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1, Entries: []EntryCandidate{{
			CandidateID: "observe-to-index", Path: "main_test.go", SourceSHA256: fingerprint.SHA256,
			NewEntry: "main_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:-", ReviewStatus: ReviewStatusReviewed,
		}}, Dispositions: []EntryDisposition{}}
		preview, err := Build(root, "2026-07-31T00:21:00Z", candidates)
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Plan.ObserveRemoved) != 1 || len(preview.Plan.IndexAdded) != 1 || len(preview.Plan.EntryCreates) != 1 {
			t.Fatalf("observe to index plan incomplete: %+v", preview.Plan)
		}
	})

	t.Run("observe_to_exclude", func(t *testing.T) {
		root := applyProductionScopeFixture(t)
		addDesiredRule(t, root, managedscope.Rule{RuleID: "user-exclude-test", Action: machinecontract.ScopeRoleExclude,
			Pattern: "main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "test is outside cognition evidence",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
		preview, err := Build(root, "2026-07-31T00:22:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1})
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Plan.ObserveRemoved) != 1 || len(preview.Plan.ExcludeAdded) != 1 || len(preview.Plan.EntryRemoves) != 0 {
			t.Fatalf("observe to exclude plan incomplete: %+v", preview.Plan)
		}
	})

	for _, target := range []string{machinecontract.ScopeRoleObserve, machinecontract.ScopeRoleIndex} {
		t.Run("exclude_to_"+target, func(t *testing.T) {
			root := applyProductionScopeFixture(t)
			writeChangeFixture(t, root, "fixtures/case.txt", "fixture\n")
			addDesiredRule(t, root, managedscope.Rule{RuleID: "user-promote-fixture", Action: target,
				Pattern: "fixtures/case.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "reviewed project exception",
				Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
			candidates := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1}
			if target == machinecontract.ScopeRoleIndex {
				fingerprint, err := baseline.HashFile(filepath.Join(root, "fixtures", "case.txt"))
				if err != nil {
					t.Fatal(err)
				}
				candidates.Entries = []EntryCandidate{{CandidateID: "exclude-to-index", Path: "fixtures/case.txt",
					SourceSHA256: fingerprint.SHA256, NewEntry: "case.txt[T.RT.5.T]: F:reviewed fixture contract | R:- | A:- | S:-",
					ReviewStatus: ReviewStatusReviewed}}
			}
			preview, err := Build(root, "2026-07-31T00:23:00Z", candidates)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Plan.ExcludeRemoved) != 1 {
				t.Fatalf("exclude removal missing for %s: %+v", target, preview.Plan)
			}
			if target == machinecontract.ScopeRoleObserve && (len(preview.Plan.ObserveAdded) != 1 || len(preview.Plan.EntryCreates) != 0) {
				t.Fatalf("exclude to observe plan incomplete: %+v", preview.Plan)
			}
			if target == machinecontract.ScopeRoleIndex && (len(preview.Plan.IndexAdded) != 1 || len(preview.Plan.EntryCreates) != 1) {
				t.Fatalf("exclude to index plan incomplete: %+v", preview.Plan)
			}
		})
	}
}

func TestRetentionDispositionRequiresReviewedBoundTransfer(t *testing.T) {
	current, ok := index.ParseEntryLine("old_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:unique recovery invariant", 1)
	if !ok {
		t.Fatal("parse current Entry")
	}
	target, ok := index.ParseEntryLine("main.go[C.RT.9.T]: F:production | R:- | A:- | S:recovery invariant", 1)
	if !ok {
		t.Fatal("parse target Entry")
	}
	entries := map[string]*index.Entry{"old_test.go": current, "main.go": target}
	candidates := map[string]EntryCandidate{"main.go": {CandidateID: "transfer", Path: "main.go", ReviewStatus: ReviewStatusReviewed}}
	header := &HeaderCandidate{CandidateID: "header", ReviewStatus: ReviewStatusReviewed}
	base := EntryDisposition{Version: machinecontract.ScopeEntryDispositionV1, SourcePath: "old_test.go",
		CurrentEntrySHA256: entrySHA(current.FullLine), TargetRole: machinecontract.ScopeRoleObserve,
		ReviewStatus: ReviewStatusReviewed, Reviewer: "model-reviewer"}
	for _, testCase := range []struct {
		name        string
		disposition string
		unique      []string
		target      string
		header      *HeaderCandidate
	}{
		{name: "no_unique", disposition: DispositionNoUniqueSemantics, unique: []string{}},
		{name: "entry", disposition: DispositionTransferEntry, unique: []string{"recovery invariant"}, target: "main.go"},
		{name: "spec", disposition: DispositionTransferSpec, unique: []string{"recovery invariant"}, target: "main.go"},
		{name: "header", disposition: DispositionTransferHeader, unique: []string{"global invariant"}, header: header},
		{name: "approved_drop", disposition: DispositionExplicitDrop, unique: []string{"obsolete test-only detail"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			value := base
			value.Disposition, value.UniqueSemantics, value.TargetEntry = testCase.disposition, testCase.unique, testCase.target
			if err := validateDisposition(value, current, machinecontract.ScopeRoleObserve, entries, candidates, testCase.header); err != nil {
				t.Fatalf("valid reviewed disposition rejected: %v", err)
			}
		})
	}
	invalid := base
	invalid.Disposition, invalid.UniqueSemantics, invalid.TargetEntry = DispositionTransferEntry, []string{"recovery invariant"}, "main.go"
	if err := validateDisposition(invalid, current, machinecontract.ScopeRoleObserve, entries, map[string]EntryCandidate{}, nil); err == nil {
		t.Fatal("semantic transfer without a reviewed target candidate was accepted")
	}
	invalid.Disposition, invalid.UniqueSemantics, invalid.TargetEntry = DispositionExplicitDrop, []string{}, ""
	if err := validateDisposition(invalid, current, machinecontract.ScopeRoleObserve, entries, candidates, nil); err == nil {
		t.Fatal("empty explicit drop was accepted")
	}
}

func TestBudgetModeOrLimitChangeRequiresDigestApproval(t *testing.T) {
	root := applyProductionScopeFixture(t)
	if err := config.MutateCognitionBudget(root, func(policy *cognitionbudget.Policy) error {
		policy.Mode = machinecontract.BudgetModeObserve
		policy.WholeIndex.MaxTokens++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, "2026-07-31T00:24:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.Risk.BudgetPolicyChange || !preview.Plan.Risk.BudgetRelaxation ||
		!preview.Plan.InteractionRequired || preview.Plan.ConfirmationPhrase == "" {
		t.Fatalf("budget relaxation was not approval-bound: %+v", preview.Plan)
	}
	if _, err := Apply(root, preview, nil); err == nil || err.Error() != "managed_scope_human_approval_required" {
		t.Fatalf("budget relaxation applied without human digest approval: %v", err)
	}
}

func TestLegacyObserveToStricterEnforceIsPolicyChangeNotRelaxation(t *testing.T) {
	oldPolicy := cognitionbudget.LegacyPolicy()
	newPolicy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	newPolicy.WholeIndex = cognitionbudget.WholeIndexPolicy{TargetTokens: 90_000, WarningTokens: 95_000, MaxTokens: 100_000}
	if !budgetRelaxed(newPolicy, oldPolicy) {
		t.Fatal("enforce to observe must be classified as relaxation")
	}
	if budgetRelaxed(oldPolicy, newPolicy) {
		t.Fatal("observe to stricter enforce must not be classified as relaxation")
	}
}

func TestScopeChangeResumeAfterIndexWrite(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	originalFault := transactionFault
	failed := false
	transactionFault = func(point string) error {
		if point == "after_publish_aoci_txt" && !failed {
			failed = true
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { transactionFault = originalFault })
	if _, err := Apply(root, preview, approval); err == nil {
		t.Fatal("fault did not interrupt Apply")
	}
	transactionFault = func(string) error { return nil }
	transactionID := preview.EnvelopeDigest[:24]
	status, err := Inspect(root, transactionID)
	if err != nil || status.State != "partial" || !status.RecoveryAvailable || !status.RollbackAvailable {
		t.Fatalf("partial recovery status invalid: %+v err=%v", status, err)
	}
	result, err := Resume(root, transactionID)
	if err != nil || result.Status != "applied" {
		t.Fatalf("resume failed: %+v err=%v", result, err)
	}
}

func TestScopeChangeRollbackRestoresExactPreimages(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineBefore, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	originalFault := transactionFault
	transactionFault = func(point string) error {
		if point == "after_publish_aoci_txt" {
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { transactionFault = originalFault })
	if _, err := Apply(root, preview, approval); err == nil {
		t.Fatal("fault did not interrupt Apply")
	}
	transactionFault = func(string) error { return nil }
	result, err := Rollback(root, preview.EnvelopeDigest[:24])
	if err != nil || result.Status != "rolled_back" {
		t.Fatalf("rollback failed: %+v err=%v", result, err)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("index preimage was not restored")
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json")); !bytes.Equal(after, baselineBefore) {
		t.Fatal("Baseline preimage was not restored")
	}
}

func TestScopeChangeDispositionCoverageAndBudgetFailBeforeWrite(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	validCandidates := candidates
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	candidates.Dispositions = nil
	prepared := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if _, err := Build(root, prepared, candidates); err == nil {
		t.Fatal("missing disposition was accepted")
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("failed Build wrote index")
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	budget := cfg.EffectiveCognitionBudget()
	budget.WholeIndex.TargetTokens, budget.WholeIndex.WarningTokens, budget.WholeIndex.MaxTokens = 1, 2, 3
	cfg.CognitionBudget = &budget
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, prepared, validCandidates); err == nil || !strings.Contains(err.Error(), "whole_index_budget_exceeded") {
		t.Fatalf("projected Whole-Index over max was not rejected: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("budget rejection wrote index")
	}
}

func TestScopeChangeResumesEveryFormalPublishAndLedgerBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("full transaction fault matrix runs in Full Confidence")
	}
	for _, faultPoint := range []string{
		"after_publish__aoci_curation_json",
		"before_publish__aoci_baseline_json",
		"before_ledger",
		"before_archive",
	} {
		t.Run(faultPoint, func(t *testing.T) {
			root, candidates := buildChangeFixture(t)
			preview, approval := buildApprovedPreview(t, root, candidates)
			if preview.CurationPostimage == nil {
				t.Fatal("fixture did not bind Curation as a formal image")
			}
			originalFault := transactionFault
			failed := false
			transactionFault = func(point string) error {
				if point == faultPoint && !failed {
					failed = true
					return errors.New("injected " + point)
				}
				return nil
			}
			t.Cleanup(func() { transactionFault = originalFault })
			if _, err := Apply(root, preview, approval); err == nil {
				t.Fatalf("fault %s did not interrupt Apply", faultPoint)
			}
			status, err := Inspect(root, preview.EnvelopeDigest[:24])
			if err != nil || !status.RecoveryAvailable || status.ThirdPartyConflict {
				t.Fatalf("fault %s lost recoverability: status=%+v err=%v", faultPoint, status, err)
			}
			transactionFault = func(string) error { return nil }
			result, err := Resume(root, preview.EnvelopeDigest[:24])
			if err != nil || result.Status != "applied" {
				t.Fatalf("fault %s did not resume: result=%+v err=%v", faultPoint, result, err)
			}
			if faultPoint == "before_archive" {
				events, corrupt := ledger.Recent(root, 0)
				terminal := 0
				for _, event := range events {
					if event.Op == "managed_scope_change_apply" && event.RecoveryTransactionID == preview.EnvelopeDigest[:24] {
						terminal++
					}
				}
				if corrupt != 0 || terminal != 1 {
					t.Fatalf("archive retry duplicated terminal Ledger event: terminal=%d corrupt=%d", terminal, corrupt)
				}
			}
		})
	}
}

func TestScopeChangeResumeFailsClosedOnThirdPartyFormalAssetConflict(t *testing.T) {
	for _, rel := range []string{".aoci/config.json", ".aoci/curation.json", ".aoci/baseline.json"} {
		t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
			root, candidates := buildChangeFixture(t)
			preview, approval := buildApprovedPreview(t, root, candidates)
			originalFault := transactionFault
			transactionFault = func(point string) error {
				if point == "after_publish_aoci_txt" {
					return errors.New("injected")
				}
				return nil
			}
			t.Cleanup(func() { transactionFault = originalFault })
			if _, err := Apply(root, preview, approval); err == nil {
				t.Fatal("fixture did not stop after index postimage")
			}
			transactionFault = func(string) error { return nil }
			path := filepath.Join(root, filepath.FromSlash(rel))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, byte('\n')), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Resume(root, preview.EnvelopeDigest[:24]); err == nil || !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("third-party %s mutation did not fail closed: %v", rel, err)
			}
			status, err := Inspect(root, preview.EnvelopeDigest[:24])
			if err != nil || !status.ThirdPartyConflict {
				t.Fatalf("third-party %s conflict not inspectable: status=%+v err=%v", rel, status, err)
			}
		})
	}
}

func TestConcurrentScopeChangeUsesSharedGlobalWriteLock(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		t.Fatal(err)
	}
	rebuiltWhileLocked, err := Build(root, preview.Plan.PreparedAt, preview.CandidateSet)
	if err != nil || rebuiltWhileLocked.EnvelopeDigest != preview.EnvelopeDigest {
		_ = lock.Release()
		after := ""
		if rebuiltWhileLocked != nil {
			after = rebuiltWhileLocked.EnvelopeDigest
		}
		t.Fatalf("runtime lock changed replay envelope: before=%s after=%s err=%v", preview.EnvelopeDigest, after, err)
	}
	type outcome struct {
		result *Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, applyErr := Apply(root, preview, approval)
		done <- outcome{result: result, err: applyErr}
	}()
	select {
	case completed := <-done:
		_ = lock.Release()
		t.Fatalf("Scope Change bypassed shared lock: result=%+v err=%v", completed.result, completed.err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	completed := <-done
	if completed.err != nil || completed.result == nil || completed.result.Status != "applied" {
		t.Fatalf("Scope Change did not proceed after lock release: result=%+v err=%v", completed.result, completed.err)
	}
}

func TestScopeChangeEnvelopeIsDeterministicAcrossRepeatedPlanning(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	prepared := "2026-07-31T00:10:00Z"
	want := ""
	for attempt := 0; attempt < 50; attempt++ {
		preview, err := Build(root, prepared, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if preview.EnvelopeVersion != machinecontract.ManagedScopeChangeEnvelopeV2 {
			t.Fatalf("missing envelope contract: %+v", preview)
		}
		if want == "" {
			want = preview.EnvelopeDigest
		} else if preview.EnvelopeDigest != want {
			t.Fatalf("nondeterministic envelope: want=%s got=%s", want, preview.EnvelopeDigest)
		}
	}
}

func TestConcurrentIdenticalApplyIsIdempotent(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	type applyOutcome struct {
		result *Result
		err    error
	}
	start := make(chan struct{})
	done := make(chan applyOutcome, 2)
	for attempt := 0; attempt < 2; attempt++ {
		go func() {
			<-start
			result, err := Apply(root, preview, approval)
			done <- applyOutcome{result: result, err: err}
		}()
	}
	close(start)
	statuses := map[string]int{}
	for attempt := 0; attempt < 2; attempt++ {
		completed := <-done
		if completed.err != nil || completed.result == nil {
			t.Fatalf("concurrent identical Apply failed: result=%+v err=%v", completed.result, completed.err)
		}
		statuses[completed.result.Status]++
	}
	if statuses["applied"] != 1 || statuses["already_applied"] != 1 {
		t.Fatalf("concurrent Apply terminal states not idempotent: %+v", statuses)
	}
}
