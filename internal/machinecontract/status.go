package machinecontract

// Auto-finalization states are shared by CLI Entries Auto, MCP update and
// Maintain results, Ledger classification, and model-visible contracts.
const (
	AutoStatusApplied        = "applied"
	AutoStatusRepairRequired = "repair_required"
	AutoStatusStopped        = "stopped"
)

// ActionCallNoArgumentMaintainForCurrentMachineBatch is shared by Guide and
// domain status projections so every ordinary Maintain route uses one token.
const ActionCallNoArgumentMaintainForCurrentMachineBatch = "call_no_argument_aoci_maintain_for_current_machine_batch"

// A blocked Volumes Maintain names the blocker the Host must clear in
// next_action. Until v0.1.0-rc21 every blocked arm answered the orphan token,
// so a Host acting on it tried to remove orphans that did not exist (#147).
// The orphan spelling is kept for the case it was always right about; the
// others follow the Guide stop vocabulary, highest precedence first.
const (
	MaintainActionExplicitOrphanRemove    = "explicit_orphan_remove_or_resolve_blocker"
	MaintainActionClosePendingRecovery    = "close_pending_recovery"
	MaintainActionScanToEstablishBaseline = "scan_to_establish_baseline"
	MaintainActionActivateScopeChange     = "activate_scope_change"
	MaintainActionAcknowledgeObserved     = "acknowledge_observed_evidence"
	MaintainActionResolveCognitionBudget  = "resolve_cognition_budget"
	MaintainActionResolveBlocker          = "resolve_blocker"
)

// ActionReviewObservedEvidence continues evidence review while authoring writes
// remain blocked. It grants no approval or permission to advance fingerprints.
const ActionReviewObservedEvidence = "review_observed_evidence"

// Database Cognition values are the shared machine vocabulary for local
// Evidence-to-FRAS authoring. They describe cognition state only; Database
// Evidence Baseline drift remains a separate contract.
const (
	DatabaseCognitionAssessmentVersion = 1
	DatabaseCognitionBindingVersion    = "database-cognition-binding/v1"
	DatabaseCognitionCandidateVersion  = "database-cognition-candidate/v1"

	DatabaseCognitionCurrent             = "cognition_current"
	DatabaseCognitionMissing             = "cognition_missing"
	DatabaseCognitionStale               = "cognition_stale"
	DatabaseCognitionUnbaselined         = "cognition_unbaselined"
	DatabaseCognitionOrphan              = "cognition_orphan"
	DatabaseCognitionEvidenceUnavailable = "evidence_unavailable"
	DatabaseCognitionEvidenceInvalid     = "evidence_invalid"
	DatabaseCognitionSourceDisabled      = "source_disabled"
	// DatabaseCognitionEngineNotImplemented is a declared source whose engine
	// has no collector yet: it neither feeds nor blocks Database Cognition.
	DatabaseCognitionEngineNotImplemented = "engine_not_implemented"

	DatabaseCognitionVolumeAbsent = "database_volume_absent"

	DatabaseCognitionActionNoConfiguration        = "no_database_configuration"
	DatabaseCognitionActionNoActionRequired       = "no_action_required"
	DatabaseCognitionActionSnapshotOrRepair       = "snapshot_or_repair_evidence"
	DatabaseCognitionActionBootstrapVolume        = "bootstrap_database_cognition"
	DatabaseCognitionActionMaintain               = ActionCallNoArgumentMaintainForCurrentMachineBatch
	DatabaseCognitionActionReviewFindings         = "review_database_cognition_findings"
	DatabaseCognitionActionReviewAllScopes        = "review_code_and_database_maintain_results"
	DatabaseCognitionActionAuthorCompleteBatch    = "author_complete_fras_and_submit_entire_batch"
	DatabaseEvidenceActionAuthorCompleteTableFRAS = "author_complete_model_authored_table_fras"
)

func DatabaseCognitionStates() []string {
	return []string{
		DatabaseCognitionCurrent,
		DatabaseCognitionMissing,
		DatabaseCognitionStale,
		DatabaseCognitionUnbaselined,
		DatabaseCognitionOrphan,
		DatabaseCognitionEvidenceUnavailable,
		DatabaseCognitionEvidenceInvalid,
		DatabaseCognitionSourceDisabled,
	}
}

// Cognition receipt and assessment values are shared by the MCP runtime,
// runtime-rules, host documentation, and compatibility tests.
const (
	CognitionStateValid     = "valid"
	CognitionStateUncertain = "uncertain"
	CognitionStateInvalid   = "invalid"

	CognitionScopeRepositoryFull = "repository_full"
	CognitionStateOwnerHostModel = "host_model"

	CognitionRecallFull                      = "full"
	CognitionRecallNone                      = "none"
	CognitionRecallHostChoiceNoneLocalOrFull = "host_choice_none_local_or_full"
	CognitionRecallHostChoiceLocalOrFull     = "host_choice_local_or_full"
)

// Cognition presentation levels are an additive explanation layer over the
// existing receipt, delivery, Attestation, and governance fields. They do not
// replace or relax any reliability decision.
const (
	CognitionLevelNoCognition      = 0
	CognitionLevelIndexLoaded      = 1
	CognitionLevelDeliveryVerified = 2
	CognitionLevelVerified         = 3
	CognitionLevelGoverned         = 4

	CognitionLevelStateNoCognition      = "no_cognition"
	CognitionLevelStateIndexLoaded      = "index_loaded"
	CognitionLevelStateDeliveryVerified = "delivery_verified"
	CognitionLevelStateVerified         = "cognition_verified"
	CognitionLevelStateGoverned         = "cognition_governed"
)

// Cognition State v2 is an opt-in, read-only projection. Its level describes
// model cognition availability only; strict Attestation, governance, and the
// final current-system reliability gate remain independent fields.
const (
	CognitionStateV2LevelNoCognition = 0
	CognitionStateV2LevelIndexLoaded = 1
	CognitionStateV2LevelDelivery    = 2
	CognitionStateV2LevelUsable      = 3

	CognitionStateV2LevelStateNoCognition = "no_cognition"
	CognitionStateV2LevelStateIndexLoaded = "index_loaded"
	CognitionStateV2LevelStateDelivery    = "delivery_verified"
	CognitionStateV2LevelStateUsable      = "model_cognition_usable"

	CognitionUsabilityUsable         = "usable"
	CognitionUsabilityNotEstablished = "not_established"
	CognitionUsabilityUnusable       = "unusable"

	StrictAttestationVerified    = "verified"
	StrictAttestationPartial     = "partial"
	StrictAttestationFailed      = "failed"
	StrictAttestationNotProvided = "not_provided"

	StrictAttestationReasonNotProvided              = "not_provided"
	StrictAttestationReasonEnvelopeIdentityMismatch = "attestation_envelope_identity_mismatch"
	StrictAttestationReasonAnswerCountMismatch      = "challenge_answer_count_mismatch"
	StrictAttestationReasonOrdinalOrderMismatch     = "ordinal_order_mismatch"
	StrictAttestationReasonObjectIdentityMismatch   = "object_identity_mismatch"
	StrictAttestationReasonTagMismatch              = "tag_mismatch"
	StrictAttestationReasonCoreFMismatch            = "core_f_mismatch"
	StrictAttestationReasonReportedEntryMismatch    = "reported_entry_count_mismatch"
	StrictAttestationReasonReportedTokenMismatch    = "reported_token_estimate_mismatch"
	StrictAttestationReasonCoverageBelowThreshold   = "coverage_below_threshold"
	StrictAttestationReasonTruncationDetected       = "truncation_detected"
)

// Cognition refresh uses a deliberately small machine vocabulary. These are
// assessment outcomes on the existing Overview path, not a parallel workflow
// or a replacement for Maintain and Guide.
const (
	RefreshStatusNotRequired         = "refresh_not_required"
	RefreshStatusRequired            = "refresh_required"
	RefreshStatusDeferredUntilStable = "refresh_deferred_until_stable"
	RefreshStatusReadyForOverview    = "refresh_ready_for_overview"

	RefreshReasonContextCompaction = "context_compaction"
	RefreshReasonSemanticThreshold = "semantic_threshold"
	RefreshReasonPhaseTransition   = "phase_transition"

	OverviewDeliveryGuidanceNone          = "-"
	OverviewDeliveryGuidanceCurrentSource = "verify_current_source_and_complete_governance"
	OverviewDeliveryGuidanceSourceBound   = "full_system_claim_disabled_source_bound_task_continuation_allowed"
)

// Host capacity (#114). A context-compaction reload that is itself interrupted
// by another compaction proves the Whole-Index does not fit the Host's
// post-compaction context; from then on, for the life of that MCP server
// session, a fresh reload of a scope at least that large answers the
// capacity stop instead of another chain that could only loop.
const (
	OverviewDeliveryModeCapacityStop            = "capacity_stop"
	OverviewFallbackHostContextCapacityExceeded = "host_context_capacity_exceeded"
	// The compaction_reload fact check_only reports: what a fresh reload of the
	// requested scope would get now.
	OverviewCompactionReloadDeliver      = "deliver"
	OverviewCompactionReloadCapacityStop = OverviewDeliveryModeCapacityStop
)

// AutoStatuses returns the canonical auto-finalization vocabulary in protocol
// order. The returned slice is new so callers cannot mutate package state.
func AutoStatuses() []string {
	return []string{
		AutoStatusApplied,
		AutoStatusRepairRequired,
		AutoStatusStopped,
	}
}

// CognitionStates returns the canonical receipt state vocabulary in lifecycle
// order. The returned slice is new so callers cannot mutate package state.
func CognitionStates() []string {
	return []string{
		CognitionStateValid,
		CognitionStateUncertain,
		CognitionStateInvalid,
	}
}

// CognitionLevelStates returns the presentation-level vocabulary from absent
// cognition through governed strict verification.
func CognitionLevelStates() []string {
	return []string{
		CognitionLevelStateNoCognition,
		CognitionLevelStateIndexLoaded,
		CognitionLevelStateDeliveryVerified,
		CognitionLevelStateVerified,
		CognitionLevelStateGoverned,
	}
}

// CognitionStateV2LevelStates returns the v2 availability levels in ascending
// order without exposing mutable package state.
func CognitionStateV2LevelStates() []string {
	return []string{
		CognitionStateV2LevelStateNoCognition,
		CognitionStateV2LevelStateIndexLoaded,
		CognitionStateV2LevelStateDelivery,
		CognitionStateV2LevelStateUsable,
	}
}

// StrictAttestationFailureReasons returns the stable non-oracle diagnostic
// order used by the optional Cognition State v2 projection.
func StrictAttestationFailureReasons() []string {
	return []string{
		StrictAttestationReasonNotProvided,
		StrictAttestationReasonEnvelopeIdentityMismatch,
		StrictAttestationReasonAnswerCountMismatch,
		StrictAttestationReasonOrdinalOrderMismatch,
		StrictAttestationReasonObjectIdentityMismatch,
		StrictAttestationReasonTagMismatch,
		StrictAttestationReasonCoreFMismatch,
		StrictAttestationReasonReportedEntryMismatch,
		StrictAttestationReasonReportedTokenMismatch,
		StrictAttestationReasonCoverageBelowThreshold,
		StrictAttestationReasonTruncationDetected,
	}
}

// RefreshStatuses returns the canonical compact Overview assessment states.
func RefreshStatuses() []string {
	return []string{
		RefreshStatusNotRequired,
		RefreshStatusRequired,
		RefreshStatusDeferredUntilStable,
		RefreshStatusReadyForOverview,
	}
}

// RefreshReasons returns the only three reasons that may cause a complete
// cognition refresh. Hosts may declare the first and third; the machine alone
// derives the semantic threshold reason.
func RefreshReasons() []string {
	return []string{
		RefreshReasonContextCompaction,
		RefreshReasonSemanticThreshold,
		RefreshReasonPhaseTransition,
	}
}

// OverviewCapacityTokens returns the Host-capacity stop's machine words, the
// ones the runtime contract and the Overview description must spell exactly.
func OverviewCapacityTokens() []string {
	return []string{
		OverviewDeliveryModeCapacityStop,
		OverviewFallbackHostContextCapacityExceeded,
	}
}

// Legacy (single-file) layouts are read-only from 0.2.0: their authoring
// surfaces are retired, every write tool and Guide answers this state, and the
// Legacy-to-Volumes migration is the one next action. Reads, Overview, verify,
// check, scan, and status --deep keep working.
const (
	StatusLegacyReadOnlyMigrateRequired   = "legacy_read_only_migrate_required"
	MaintainActionMigrateLegacyRepository = "migrate_legacy_repository"
)

// Observe review through MCP (#151). A Maintain blocked by observed_pending
// publishes the pending evidence and the digest that binds it; the
// observe_review intent confirms that exact list through the same Scope
// transaction as scope acknowledge.
const (
	MaintainIntentObserveReview = "observe_review"

	ObserveReviewStatusAcknowledged          = "acknowledged"
	ObserveReviewStatusStale                 = "stale"
	ObserveReviewStatusNotPending            = "not_pending"
	ObserveReviewStatusHumanApprovalRequired = "human_approval_required"

	ObserveReviewActionReviewCurrent = "review_current_observe_evidence"
	ObserveReviewActionHumanApproval = "request_human_approval"
)
