package scopechange

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

const authorizationTestTime = "2026-07-31T03:00:00Z"

func setFixtureAuthorization(t *testing.T, root, automationMode, scopeMode string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(automationMode); err != nil {
		t.Fatal(err)
	}
	policy := cfg.EffectiveManagedScope()
	policy.ApprovalMode = scopeMode
	normalized, err := managedscope.Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &normalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func buildAutoPreview(t *testing.T) (string, CandidateSet, *Preview) {
	t.Helper()
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	return root, candidates, preview
}

func TestAutoAuthorizationGeneratesPolicyBoundReceiptAndAppliesWithoutTTY(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	if preview.Plan.InteractionRequired || preview.Plan.ConfirmationPhrase != "" ||
		preview.Plan.AuthorizationPolicy.EffectiveMode != machinecontract.ApplyAuthorizationAuto {
		t.Fatalf("auto Plan retained an interactive gate: %+v", preview.Plan.AuthorizationPolicy)
	}
	receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Mechanism != machinecontract.ApprovalMechanismPolicyBoundAuto || receipt.ApprovalDigest == "" ||
		receipt.EnvelopeDigest != preview.EnvelopeDigest || receipt.PreviewDigest != preview.PreviewID ||
		receipt.CurrentIndexSHA256 != preview.IndexPostimage.PreimageSHA256 ||
		receipt.ProjectedBaselineSHA256 != preview.BaselinePostimage.PostimageSHA256 ||
		!receipt.RetentionReviewComplete || receipt.RetentionReviewTotal != 1 || receipt.EntryBefore != 3 || receipt.EntryAfter != 2 {
		t.Fatalf("policy-bound receipt is incomplete: %+v", receipt)
	}
	result, err := ApplyAuthorized(root, preview, nil, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto ||
		result.ApprovalDigest != receipt.ApprovalDigest || result.Status != "applied" {
		t.Fatalf("auto Apply did not preserve authorization evidence: %+v", result)
	}
}

func TestAutoApplyCanCreateItsReceiptInternally(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	result, err := Apply(root, preview, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto || result.ApprovalDigest == "" {
		t.Fatalf("automatic Apply omitted its policy Receipt: %+v", result)
	}
	again, err := Apply(root, preview, nil)
	if err != nil || again.Status != "already_applied" || again.ApprovalDigest != result.ApprovalDigest {
		t.Fatalf("repeated auto Apply was not idempotent: %+v err=%v", again, err)
	}
}

func TestReviewLegacyAndOffAuthorizationModes(t *testing.T) {
	t.Run("review_requires_tty_receipt", func(t *testing.T) {
		root, candidates := buildChangeFixture(t)
		setFixtureAuthorization(t, root, config.AutomationModeReview, machinecontract.ScopeApprovalModeInherit)
		preview, err := Build(root, authorizationTestTime, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if !preview.Plan.InteractionRequired || preview.Plan.AuthorizationPolicy.EffectiveMode != machinecontract.ApplyAuthorizationReview {
			t.Fatalf("review Plan did not retain interaction: %+v", preview.Plan)
		}
		if _, err := Apply(root, preview, nil); err == nil || !strings.Contains(err.Error(), "human_approval_required") {
			t.Fatalf("review Apply did not require TTY receipt: %v", err)
		}
		approval, err := NewApproval(preview, "reviewer@example.invalid", authorizationTestTime)
		if err != nil {
			t.Fatal(err)
		}
		if approval.Mechanism != machinecontract.ApprovalMechanismInteractiveDigestConfirmation {
			t.Fatalf("review mechanism drifted: %+v", approval)
		}
		if _, err := Apply(root, preview, approval); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("legacy_preserves_existing_boundary", func(t *testing.T) {
		root, candidates := buildChangeFixture(t)
		preview, err := Build(root, authorizationTestTime, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Plan.AuthorizationPolicy.EffectiveMode != machinecontract.ApplyAuthorizationLegacy || !preview.Plan.InteractionRequired {
			t.Fatalf("legacy approval boundary changed: %+v", preview.Plan.AuthorizationPolicy)
		}
		if _, err := Apply(root, preview, nil); err == nil || !strings.Contains(err.Error(), "human_approval_required") {
			t.Fatalf("legacy reduction did not require approval: %v", err)
		}
	})

	t.Run("off_is_zero_write", func(t *testing.T) {
		root, candidates := buildChangeFixture(t)
		setFixtureAuthorization(t, root, config.AutomationModeOff, machinecontract.ScopeApprovalModeAuto)
		preview, err := Build(root, authorizationTestTime, candidates)
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{"aoci.txt", ".aoci/config.json", ".aoci/baseline.json", ".aoci/curation.json"}
		before := map[string][]byte{}
		for _, rel := range paths {
			before[rel], _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		}
		if _, err := Apply(root, preview, nil); err == nil || !strings.Contains(err.Error(), "automation_off") {
			t.Fatalf("off Apply was not blocked: %v", err)
		}
		for _, rel := range paths {
			after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if !bytes.Equal(after, before[rel]) {
				t.Fatalf("off Apply changed %s", rel)
			}
		}
	})
}

func TestPolicyBoundReceiptFailsClosedOnEveryFormalDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "index", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, "aoci.txt")
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "baseline", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, ".aoci", "baseline.json")
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, append(data, ' '), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "scope_policy", mutate: func(t *testing.T, root string) {
			if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				policy.ApprovalMode = machinecontract.ScopeApprovalModeReview
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _, preview := buildAutoPreview(t)
			receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, root)
			if _, err := ApplyAuthorized(root, preview, nil, receipt); err == nil {
				t.Fatal("drifted preimage accepted an old policy Receipt")
			}
		})
	}
}

func TestPolicyBoundReceiptCannotAuthorizeAnotherEnvelope(t *testing.T) {
	root, candidates, preview := buildAutoPreview(t)
	receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Build(root, "2026-07-31T03:00:01Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if other.EnvelopeDigest == preview.EnvelopeDigest {
		t.Fatal("fixture did not produce distinct envelopes")
	}
	if _, err := ApplyAuthorized(root, other, nil, receipt); err == nil || !strings.Contains(err.Error(), "binding_mismatch") {
		t.Fatalf("old Receipt authorized another Envelope: %v", err)
	}
}

func TestAutoAuthorizationHardBlockersAreFactsNotApprovalPrompts(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocked := func(name, want string, mutate func(*Preview)) {
		t.Helper()
		copyPreview := *preview
		copyPreview.Plan = preview.Plan
		copyPreview.Plan.EntryRemoves = append([]EntryChange{}, preview.Plan.EntryRemoves...)
		copyPreview.Plan.RetentionReview = append([]EntryDisposition{}, preview.Plan.RetentionReview...)
		copyPreview.Plan.WriteSet = append([]string{}, preview.Plan.WriteSet...)
		mutate(&copyPreview)
		reasons := strings.Join(autoAuthorizationBlockers(&copyPreview, cfg), ",")
		if !strings.Contains(reasons, want) {
			t.Fatalf("%s did not block on %s: %s", name, want, reasons)
		}
	}
	assertBlocked("retention", "retention_review_incomplete", func(value *Preview) {
		value.Plan.RetentionReview = nil
	})
	assertBlocked("p0", "p0_or_p1", func(value *Preview) { value.Plan.Risk.P0 = 1 })
	assertBlocked("p1", "p0_or_p1", func(value *Preview) { value.Plan.Risk.P1 = 1 })
	assertBlocked("secret", "high_risk_content_inclusion", func(value *Preview) { value.Plan.Risk.HighRiskOptIn = true })
	assertBlocked("source write", "business_source_write_set", func(value *Preview) {
		value.Plan.WriteSet = append(value.Plan.WriteSet, "main.go")
	})
	assertBlocked("budget relaxation", "budget_policy_relaxation", func(value *Preview) { value.Plan.Risk.BudgetRelaxation = true })
	assertBlocked("explicit drop", "explicit_drop_without_transfer", func(value *Preview) {
		value.Plan.RetentionReview[0].Disposition = DispositionExplicitDrop
	})
}

func TestLargeGovernedEntryRetirementIsAutoAuthorizable(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	preview.Plan.EntryRemoves = make([]EntryChange, 362)
	preview.Plan.RetentionReview = make([]EntryDisposition, 362)
	for index := 0; index < 362; index++ {
		path := fmt.Sprintf("tests/case-%03d_test.go", index)
		preview.Plan.EntryRemoves[index] = EntryChange{Path: path, Action: "remove"}
		preview.Plan.RetentionReview[index] = EntryDisposition{SourcePath: path, ReviewStatus: ReviewStatusReviewed,
			Reviewer: "model-retention-review", Disposition: DispositionNoUniqueSemantics}
	}
	preview.Plan.Risk.LargeReduction = true
	preview.Plan.Risk.EntryRemovalCount = 362
	if reasons := autoAuthorizationBlockers(preview, cfg); len(reasons) != 0 {
		t.Fatalf("large governed retirement was treated as an approval fact: %v", reasons)
	}
}

func TestBudgetDirectionGovernsAutoAuthorization(t *testing.T) {
	oldPolicy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	tight := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	tight.WholeIndex = cognitionbudget.WholeIndexPolicy{TargetTokens: 90000, WarningTokens: 95000, MaxTokens: 100000}
	if budgetRelaxed(oldPolicy, tight) {
		t.Fatal("budget tightening was classified as relaxation")
	}
	relaxed := tight
	relaxed.WholeIndex.MaxTokens++
	if !budgetRelaxed(tight, relaxed) {
		t.Fatal("max increase was not classified as relaxation")
	}
	relaxed = tight
	relaxed.Mode = machinecontract.BudgetModeObserve
	if !budgetRelaxed(tight, relaxed) {
		t.Fatal("enforce to observe was not classified as relaxation")
	}
}

func TestAutoResumeUsesIntentReceiptWithoutReapproval(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	originalFault := transactionFault
	transactionFault = func(point string) error {
		if point == "after_intent" {
			return fmt.Errorf("fixture interruption")
		}
		return nil
	}
	t.Cleanup(func() { transactionFault = originalFault })
	if _, err := Apply(root, preview, nil); err == nil || !strings.Contains(err.Error(), "fixture interruption") {
		t.Fatalf("fixture did not stop after immutable intent: %v", err)
	}
	transactionFault = func(string) error { return nil }
	result, err := Resume(root, preview.EnvelopeDigest[:24])
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto ||
		result.ApprovalDigest == "" {
		t.Fatalf("Resume did not preserve the original auto Receipt: %+v", result)
	}
}

func TestAutoApplyPreservesAbsentConfiguredCurationIdentity(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CurationExclude = append(cfg.CurationExclude, "future/generated-contract.txt")
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, "2026-07-31T03:40:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, preview, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto {
		t.Fatalf("future-path Curation identity did not survive Apply: %+v", result)
	}
}

func TestPolicyReceiptCannotBeForgedByPresentationText(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Mechanism = "locale_says_approved"
	data, err := Encode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePolicyBoundApproval(data); err == nil {
		t.Fatal("presentation text forged a machine authorization Receipt")
	}
}
