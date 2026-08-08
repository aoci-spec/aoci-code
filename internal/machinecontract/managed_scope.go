package machinecontract

// Managed Scope and cognition budget identifiers are the single executable
// authority. Specifications, CLI JSON, capabilities, and validators consume
// these values and must not copy their vocabularies.
const (
	ManagedScopePolicyV2         = "managed-scope-policy/v2"
	ManagedScopeEvaluationV2     = "managed-scope-evaluation/v2"
	ManagedScopeChangePlanV2     = "managed-scope-change-plan/v2"
	ManagedScopeChangePreviewV2  = "managed-scope-change-preview/v2"
	ManagedScopeChangeEnvelopeV2 = "managed-scope-change-envelope/v2"
	ManagedScopeChangeApprovalV2 = "managed-scope-change-approval/v2"
	ManagedScopeChangeResultV2   = "managed-scope-change-result/v2"
	ManagedScopeChangeStatusV2   = "managed-scope-change-status/v2"
	ManagedScopeStatusV2         = "managed-scope-status/v2"
	ManagedScopeRecoveryV2       = "managed-scope-change-recovery/v2"
	ManagedScopeProposalV1       = "managed-scope-proposal/v1"
	ScopeEntryDispositionV1      = "scope-entry-disposition/v1"
	ManagedScopeCandidateSetV1   = "managed-scope-candidate-set/v1"
	ManagedScopeSafetyApprovalV1 = "managed-scope-safety-approval/v1"
	ManagedScopeBaselineV1       = "managed-scope-baseline/v1"
	ApplyAuthorizationPolicyV1   = "aoci-apply-authorization-policy/v1"
	PolicyBoundApprovalV1        = "aoci-policy-bound-approval/v1"

	CognitionBudgetPolicyV1     = "cognition-budget-policy/v1"
	CognitionBudgetReportV1     = "cognition-budget-report/v1"
	CognitionBudgetValidationV1 = "cognition-budget-validation/v1"

	ScopeRoleIndex   = "index"
	ScopeRoleObserve = "observe"
	ScopeRoleExclude = "exclude"

	ScopeProfileProduction = "production"
	ScopeProfileFull       = "full"
	ScopeProfileCustom     = "custom"

	ScopePatternFile      = "file"
	ScopePatternDirectory = "directory"
	ScopePatternGlob      = "glob"

	ScopeRuleBuiltin  = "builtin"
	ScopeRuleProfile  = "profile"
	ScopeRuleUser     = "user"
	ScopeRuleCuration = "curation"
	ScopeRuleSafety   = "safety"

	ScopeDecisionUnspecified                  = "unspecified"
	ScopeDecisionSemanticDensity              = "semantic_density"
	ScopeDecisionTestResponsibilityDelegation = "test_responsibility_delegation"
	ScopeDecisionGeneratedAsset               = "generated_asset"
	ScopeDecisionLocaleDelegation             = "locale_delegation"
	ScopeDecisionPublicPrivateBoundary        = "public_private_boundary"
	ScopeDecisionDuplicateSemantics           = "duplicate_semantics"
	ScopeDecisionTransportConstraint          = "transport_constraint"
	ScopeDecisionMaintainerDirection          = "maintainer_direction"

	ObserveChangeReviewRequired = "review_required"
	ObserveChangeInformational  = "informational"

	BudgetModeObserve = "observe"
	BudgetModeEnforce = "enforce"

	BudgetStatusHealthy    = "healthy"
	BudgetStatusNearBudget = "near_budget"
	BudgetStatusWarning    = "warning"
	BudgetStatusExceeded   = "exceeded"

	ScopeApprovalModeInherit = "inherit"
	ScopeApprovalModeAuto    = "auto"
	ScopeApprovalModeReview  = "review"

	ApplyAuthorizationAuto   = "auto"
	ApplyAuthorizationReview = "review"
	ApplyAuthorizationLegacy = "legacy"
	ApplyAuthorizationOff    = "off"

	ApprovalMechanismPolicyBoundAuto               = "policy_bound_auto"
	ApprovalMechanismInteractiveDigestConfirmation = "interactive_digest_confirmation"
)

func ScopeRoles() []string {
	return []string{ScopeRoleIndex, ScopeRoleObserve, ScopeRoleExclude}
}

func ScopeProfiles() []string {
	return []string{ScopeProfileProduction, ScopeProfileFull, ScopeProfileCustom}
}

func ScopePatternKinds() []string {
	return []string{ScopePatternFile, ScopePatternDirectory, ScopePatternGlob}
}

func ScopeRuleSources() []string {
	return []string{ScopeRuleBuiltin, ScopeRuleProfile, ScopeRuleUser, ScopeRuleCuration, ScopeRuleSafety}
}

func ScopeDecisionBases() []string {
	return []string{ScopeDecisionUnspecified, ScopeDecisionSemanticDensity, ScopeDecisionTestResponsibilityDelegation,
		ScopeDecisionGeneratedAsset, ScopeDecisionLocaleDelegation, ScopeDecisionPublicPrivateBoundary,
		ScopeDecisionDuplicateSemantics, ScopeDecisionTransportConstraint, ScopeDecisionMaintainerDirection}
}

func BudgetModes() []string {
	return []string{BudgetModeObserve, BudgetModeEnforce}
}

func ScopeApprovalModes() []string {
	return []string{ScopeApprovalModeInherit, ScopeApprovalModeAuto, ScopeApprovalModeReview}
}
