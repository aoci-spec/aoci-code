package scopechange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// EffectiveApplyAuthorizationMode 解析当前团队配置下的生效授权模式。
// Scope Change 与首次 Baseline 建立共用同一解析,收据里记录的模式才能与
// 事务授权时使用的模式严格同源。
func EffectiveApplyAuthorizationMode(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	if cfg.EffectiveAutomationMode() == config.AutomationModeOff {
		return machinecontract.ApplyAuthorizationOff, nil
	}
	switch cfg.EffectiveManagedScope().ApprovalMode {
	case machinecontract.ScopeApprovalModeAuto:
		return machinecontract.ApplyAuthorizationAuto, nil
	case machinecontract.ScopeApprovalModeReview:
		return machinecontract.ApplyAuthorizationReview, nil
	case machinecontract.ScopeApprovalModeInherit:
		switch cfg.EffectiveAutomationMode() {
		case config.AutomationModeAuto:
			return machinecontract.ApplyAuthorizationAuto, nil
		case config.AutomationModeReview:
			return machinecontract.ApplyAuthorizationReview, nil
		case config.AutomationModeLegacy:
			return machinecontract.ApplyAuthorizationLegacy, nil
		}
	}
	return "", fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
}

func resolveApplyAuthorizationPolicy(cfg *config.Config) (ApplyAuthorizationPolicy, string, error) {
	if cfg == nil {
		return ApplyAuthorizationPolicy{}, "", fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	policy := cfg.EffectiveManagedScope()
	value := ApplyAuthorizationPolicy{
		Version:           machinecontract.ApplyAuthorizationPolicyV1,
		Operation:         Operation,
		AutomationMode:    cfg.EffectiveAutomationMode(),
		ScopeApprovalMode: policy.ApprovalMode,
	}
	mode, err := EffectiveApplyAuthorizationMode(cfg)
	if err != nil {
		return ApplyAuthorizationPolicy{}, "", err
	}
	value.EffectiveMode = mode
	identity, err := digestJSON(value)
	if err != nil {
		return ApplyAuthorizationPolicy{}, "", err
	}
	return value, identity, nil
}

// NewPolicyBoundApproval proves the current team policy, deterministic replay,
// hard gates, formal preimages, and recovery before creating an Auto receipt.
func NewPolicyBoundApproval(repositoryRoot string, preview *Preview, createdAt string) (*PolicyBoundApproval, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || preview == nil {
		return nil, fmt.Errorf("managed_scope_auto_authorization_input_invalid")
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil || parsed.Location() != time.UTC {
		return nil, fmt.Errorf("managed_scope_auto_authorization_timestamp_invalid")
	}
	if err := validatePreview(preview); err != nil {
		return nil, err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	policy, policyIdentity, err := resolveApplyAuthorizationPolicy(cfg)
	if err != nil {
		return nil, err
	}
	if policy.EffectiveMode != machinecontract.ApplyAuthorizationAuto {
		return nil, fmt.Errorf("managed_scope_auto_authorization_unavailable: %s", policy.EffectiveMode)
	}
	if err := validateAuthorizationPolicyBinding(preview, policy, policyIdentity); err != nil {
		return nil, err
	}
	if err := cognitiontxn.RejectOtherPending(root, ""); err != nil {
		return nil, fmt.Errorf("managed_scope_auto_authorization_blocked: pending_transaction")
	}
	rebuilt, err := Build(root, preview.Plan.PreparedAt, preview.CandidateSet)
	if err != nil || rebuilt.EnvelopeDigest != preview.EnvelopeDigest {
		return nil, fmt.Errorf("managed_scope_auto_authorization_blocked: deterministic_replay_failed")
	}
	if err := classifyPreviewPreimages(root, preview); err != nil {
		return nil, fmt.Errorf("managed_scope_auto_authorization_blocked: third_party_conflict")
	}
	if reasons := autoAuthorizationBlockers(preview, cfg); len(reasons) != 0 {
		return nil, fmt.Errorf("managed_scope_auto_authorization_blocked: %s", strings.Join(reasons, ","))
	}
	receipt := &PolicyBoundApproval{
		Version:                     machinecontract.PolicyBoundApprovalV1,
		Mechanism:                   machinecontract.ApprovalMechanismPolicyBoundAuto,
		Operation:                   Operation,
		RepositoryIdentity:          preview.Plan.RepositoryRootIdentity,
		AutomationMode:              policy.AutomationMode,
		ScopeApprovalMode:           policy.ScopeApprovalMode,
		AuthorizationPolicyIdentity: policyIdentity,
		EnvelopeDigest:              preview.EnvelopeDigest,
		PreviewDigest:               preview.PreviewID,
		CurrentIndexSHA256:          preview.IndexPostimage.PreimageSHA256,
		CurrentBaselineSHA256:       preview.BaselinePostimage.PreimageSHA256,
		CurrentConfigSHA256:         preview.ConfigPostimage.PreimageSHA256,
		CurrentScopePolicyIdentity:  preview.Plan.NewPolicyIdentity,
		CurrentCurationIdentity:     currentCurationIdentity(preview),
		ProjectedIndexSHA256:        preview.IndexPostimage.PostimageSHA256,
		ProjectedBaselineSHA256:     preview.BaselinePostimage.PostimageSHA256,
		ProjectedWholeIndexTokens:   preview.Plan.WholeIndexAfter.WholeIndexTokens,
		EntryBefore:                 preview.Plan.WholeIndexBefore.EntryCount,
		EntryAfter:                  preview.Plan.WholeIndexAfter.EntryCount,
		IndexCount:                  preview.Evaluation.IndexCount,
		ObserveCount:                preview.Evaluation.ObserveCount,
		ExcludeCount:                preview.Evaluation.ExcludeCount,
		RetentionReviewTotal:        len(preview.Plan.RetentionReview),
		RetentionReviewComplete:     retentionReviewComplete(preview),
		P0:                          preview.Plan.Risk.P0,
		P1:                          preview.Plan.Risk.P1,
		WriteSet:                    append([]string{}, preview.Plan.WriteSet...),
		GuardSet:                    append([]string{}, preview.Plan.GuardSet...),
		RecoveryDirection:           preview.Plan.RecoveryDirection,
		CreatedAt:                   createdAt,
	}
	receipt.ApprovalDigest, err = policyBoundApprovalIdentity(*receipt)
	return receipt, err
}

// DecodePolicyBoundApproval rejects unknown fields, versions, and digest drift.
func DecodePolicyBoundApproval(data []byte) (*PolicyBoundApproval, error) {
	var value PolicyBoundApproval
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != machinecontract.PolicyBoundApprovalV1 {
		return nil, fmt.Errorf("managed_scope_policy_bound_approval_invalid")
	}
	want, err := policyBoundApprovalIdentity(value)
	if err != nil || want != value.ApprovalDigest {
		return nil, fmt.Errorf("managed_scope_policy_bound_approval_identity_invalid")
	}
	return &value, nil
}

func policyBoundApprovalIdentity(value PolicyBoundApproval) (string, error) {
	value.ApprovalDigest = ""
	return digestJSON(value)
}

func validateAuthorizationPolicyBinding(preview *Preview, policy ApplyAuthorizationPolicy, identity string) error {
	if preview.Plan.AuthorizationPolicy != policy || preview.Plan.AuthorizationPolicyIdentity != identity {
		return fmt.Errorf("managed_scope_apply_authorization_policy_drift")
	}
	want, err := digestJSON(preview.Plan.AuthorizationPolicy)
	if err != nil || want != preview.Plan.AuthorizationPolicyIdentity {
		return fmt.Errorf("managed_scope_apply_authorization_policy_identity_invalid")
	}
	return nil
}

func validatePolicyBoundApproval(preview *Preview, receipt *PolicyBoundApproval) error {
	if receipt == nil || receipt.Version != machinecontract.PolicyBoundApprovalV1 ||
		receipt.Mechanism != machinecontract.ApprovalMechanismPolicyBoundAuto || receipt.Operation != Operation {
		return fmt.Errorf("managed_scope_policy_bound_approval_required")
	}
	want, err := policyBoundApprovalIdentity(*receipt)
	if err != nil || want != receipt.ApprovalDigest {
		return fmt.Errorf("managed_scope_policy_bound_approval_digest_mismatch")
	}
	expected := PolicyBoundApproval{
		Version:                     machinecontract.PolicyBoundApprovalV1,
		Mechanism:                   machinecontract.ApprovalMechanismPolicyBoundAuto,
		Operation:                   Operation,
		RepositoryIdentity:          preview.Plan.RepositoryRootIdentity,
		AutomationMode:              preview.Plan.AuthorizationPolicy.AutomationMode,
		ScopeApprovalMode:           preview.Plan.AuthorizationPolicy.ScopeApprovalMode,
		AuthorizationPolicyIdentity: preview.Plan.AuthorizationPolicyIdentity,
		EnvelopeDigest:              preview.EnvelopeDigest,
		PreviewDigest:               preview.PreviewID,
		CurrentIndexSHA256:          preview.IndexPostimage.PreimageSHA256,
		CurrentBaselineSHA256:       preview.BaselinePostimage.PreimageSHA256,
		CurrentConfigSHA256:         preview.ConfigPostimage.PreimageSHA256,
		CurrentScopePolicyIdentity:  preview.Plan.NewPolicyIdentity,
		CurrentCurationIdentity:     currentCurationIdentity(preview),
		ProjectedIndexSHA256:        preview.IndexPostimage.PostimageSHA256,
		ProjectedBaselineSHA256:     preview.BaselinePostimage.PostimageSHA256,
		ProjectedWholeIndexTokens:   preview.Plan.WholeIndexAfter.WholeIndexTokens,
		EntryBefore:                 preview.Plan.WholeIndexBefore.EntryCount,
		EntryAfter:                  preview.Plan.WholeIndexAfter.EntryCount,
		IndexCount:                  preview.Evaluation.IndexCount,
		ObserveCount:                preview.Evaluation.ObserveCount,
		ExcludeCount:                preview.Evaluation.ExcludeCount,
		RetentionReviewTotal:        len(preview.Plan.RetentionReview),
		RetentionReviewComplete:     retentionReviewComplete(preview),
		P0:                          preview.Plan.Risk.P0,
		P1:                          preview.Plan.Risk.P1,
		WriteSet:                    append([]string{}, preview.Plan.WriteSet...),
		GuardSet:                    append([]string{}, preview.Plan.GuardSet...),
		RecoveryDirection:           preview.Plan.RecoveryDirection,
		CreatedAt:                   receipt.CreatedAt,
		ApprovalDigest:              receipt.ApprovalDigest,
	}
	expected.ApprovalDigest = ""
	expectedDigest, err := policyBoundApprovalIdentity(expected)
	if err != nil || receipt.ApprovalDigest != expectedDigest {
		return fmt.Errorf("managed_scope_policy_bound_approval_binding_mismatch")
	}
	parsed, err := time.Parse(time.RFC3339, receipt.CreatedAt)
	if err != nil || parsed.Location() != time.UTC {
		return fmt.Errorf("managed_scope_policy_bound_approval_timestamp_invalid")
	}
	return nil
}

func autoAuthorizationBlockers(preview *Preview, cfg *config.Config) []string {
	reasons := []string{}
	if preview.Plan.Risk.HighRiskOptIn || preview.CandidateSet.SafetyApproval != nil || len(cfg.SafeInventoryHighRiskOptIn) != 0 {
		reasons = append(reasons, "high_risk_content_inclusion")
	}
	if preview.Plan.Risk.BudgetRelaxation {
		reasons = append(reasons, "budget_policy_relaxation")
	}
	if preview.Plan.Risk.ApprovalPolicyRelaxation {
		reasons = append(reasons, "approval_policy_relaxation")
	}
	if preview.Plan.Risk.P0 != 0 || preview.Plan.Risk.P1 != 0 {
		reasons = append(reasons, "p0_or_p1")
	}
	if preview.Plan.Risk.CognitionCoverageReduction {
		reasons = append(reasons, "cognition_coverage_reduction_requires_independent_review")
	}
	if preview.Plan.Risk.TransportConstraintNotAllowed {
		reasons = append(reasons, "transport_constraint_not_allowed")
	}
	if !retentionReviewComplete(preview) {
		reasons = append(reasons, "retention_review_incomplete")
	}
	for _, disposition := range preview.Plan.RetentionReview {
		if disposition.Disposition == DispositionExplicitDrop {
			reasons = append(reasons, "explicit_drop_without_transfer")
			break
		}
	}
	if len(preview.Plan.WholeIndexAfter.Violations) != 0 ||
		preview.Plan.WholeIndexAfter.WholeIndexTokens > preview.Plan.WholeIndexAfter.MaxTokens {
		reasons = append(reasons, "cognition_budget_exceeded")
	}
	if preview.Plan.RecoveryDirection == "" {
		reasons = append(reasons, "recovery_unavailable")
	}
	allowedWrites := map[string]bool{
		".aoci/config.json": true, ".aoci/curation.json": true,
		".aoci/baseline.json": true, ".aoci/ledger.jsonl": true,
	}
	// Build binds this to cfg.IndexPath for Legacy or to the Root-declared Code
	// Volume for Volumes. Apply rebuilds the complete Preview before any write.
	allowedWrites[preview.IndexPostimage.Path] = true
	for _, path := range preview.Plan.WriteSet {
		if !allowedWrites[path] {
			reasons = append(reasons, "business_source_write_set")
			break
		}
	}
	for _, image := range formalImages(preview) {
		if !allowedWrites[image.Path] {
			reasons = append(reasons, "business_source_postimage")
			break
		}
	}
	sort.Strings(reasons)
	return deduplicate(reasons)
}

func retentionReviewComplete(preview *Preview) bool {
	if preview == nil || len(preview.Plan.RetentionReview) != len(preview.Plan.EntryRemoves) {
		return false
	}
	seen := map[string]bool{}
	for _, disposition := range preview.Plan.RetentionReview {
		if disposition.SourcePath == "" || disposition.ReviewStatus != ReviewStatusReviewed ||
			disposition.Reviewer == "" || seen[disposition.SourcePath] {
			return false
		}
		seen[disposition.SourcePath] = true
	}
	for _, removal := range preview.Plan.EntryRemoves {
		if !seen[removal.Path] {
			return false
		}
	}
	return true
}

func currentCurationIdentity(preview *Preview) string {
	if preview != nil && preview.CurationPostimage != nil {
		return preview.CurationPostimage.PreimageSHA256
	}
	return digestStrings([]string{"absent"})
}

func validatePolicyBoundApprovalLive(root string, preview *Preview, receipt *PolicyBoundApproval) error {
	if err := validatePolicyBoundApproval(preview, receipt); err != nil {
		return err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	policy, identity, err := resolveApplyAuthorizationPolicy(cfg)
	if err != nil || policy.EffectiveMode != machinecontract.ApplyAuthorizationAuto {
		return fmt.Errorf("managed_scope_apply_authorization_policy_drift")
	}
	if err := validateAuthorizationPolicyBinding(preview, policy, identity); err != nil {
		return err
	}
	if err := classifyPreviewPreimages(root, preview); err != nil {
		return err
	}
	if reasons := autoAuthorizationBlockers(preview, cfg); len(reasons) != 0 {
		return fmt.Errorf("managed_scope_auto_authorization_blocked: %s", strings.Join(reasons, ","))
	}
	return nil
}

func authorizationMechanism(intent *RecoveryIntent) string {
	if intent == nil {
		return ""
	}
	if intent.PolicyBoundApproval != nil {
		return intent.PolicyBoundApproval.Mechanism
	}
	if intent.Approval != nil {
		return intent.Approval.Mechanism
	}
	return "legacy_no_interaction_required"
}

func authorizationDigest(intent *RecoveryIntent) string {
	if intent == nil {
		return ""
	}
	if intent.PolicyBoundApproval != nil {
		return intent.PolicyBoundApproval.ApprovalDigest
	}
	if intent.Approval != nil {
		return intent.Approval.ApprovalDigest
	}
	return ""
}

func validateIntentAuthorization(intent *RecoveryIntent) error {
	if intent == nil {
		return fmt.Errorf("managed_scope_recovery_authorization_invalid")
	}
	wantIdentity, err := digestJSON(intent.Preview.Plan.AuthorizationPolicy)
	if err != nil || wantIdentity != intent.Preview.Plan.AuthorizationPolicyIdentity {
		return fmt.Errorf("managed_scope_apply_authorization_policy_identity_invalid")
	}
	// Resume and idempotent re-apply must reach the same verdict as the original
	// Apply, so the branch is selected the same way.
	switch applyAuthorizationBranch(
		intent.Preview.Plan.AuthorizationPolicy.EffectiveMode,
		intent.Preview.Plan.InteractionRequired,
	) {
	case machinecontract.ApplyAuthorizationAuto:
		if intent.Approval != nil {
			return fmt.Errorf("managed_scope_recovery_authorization_invalid")
		}
		return validatePolicyBoundApproval(&intent.Preview, intent.PolicyBoundApproval)
	case machinecontract.ApplyAuthorizationReview, machinecontract.ApplyAuthorizationLegacy:
		if intent.PolicyBoundApproval != nil {
			return fmt.Errorf("managed_scope_recovery_authorization_invalid")
		}
		return validateApproval(&intent.Preview, intent.Approval)
	default:
		return fmt.Errorf("managed_scope_recovery_authorization_invalid")
	}
}
