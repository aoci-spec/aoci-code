package bootstrapapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// AutoEligibilityBlocker is a machine-derived reason that policy-bound Auto
// cannot authorize the current Envelope. Count supports object-level blockers
// without exposing sensitive paths or Candidate semantics.
type AutoEligibilityBlocker struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// AutoEligibility is the additive authorization projection for Fresh
// Bootstrap. It is audit evidence only and is never supplied by a Candidate.
type AutoEligibility struct {
	Version                string                   `json:"version"`
	AutomationMode         string                   `json:"automation_mode"`
	AutomationPolicySource string                   `json:"automation_policy_source"`
	FreshBootstrap         bool                     `json:"fresh_bootstrap"`
	ProvenanceVerified     bool                     `json:"provenance_verified"`
	PreviewReady           bool                     `json:"preview_ready"`
	RiskCount              int                      `json:"risk_count"`
	ReviewVisibleCount     int                      `json:"review_visible_count"`
	AutoBlockerCount       int                      `json:"auto_blocker_count"`
	SourceStable           bool                     `json:"source_stable"`
	PlanStable             bool                     `json:"plan_stable"`
	RecoveryAvailable      bool                     `json:"recovery_available"`
	CASAvailable           bool                     `json:"cas_available"`
	BusinessRowsRead       int                      `json:"business_rows_read"`
	DDLDMLStatements       int                      `json:"ddl_dml_statements"`
	NetworkAccessed        bool                     `json:"network_accessed"`
	DatabaseSideEffects    bool                     `json:"database_side_effects"`
	AutoReady              bool                     `json:"auto_ready"`
	AuthorizationMechanism string                   `json:"authorization_mechanism,omitempty"`
	TTYRequired            bool                     `json:"tty_required"`
	ModelSelfApproval      bool                     `json:"model_self_approval"`
	Blockers               []AutoEligibilityBlocker `json:"blockers"`
}

// EvaluateAutoEligibility replays every non-semantic Fresh authorization fact.
// It does not write an Approval, transaction Intent, or formal asset.
func EvaluateAutoEligibility(root string, envelope *ApplyEnvelope) (*AutoEligibility, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	policy := envelope.AutomationPolicy
	projection := &AutoEligibility{
		Version:                machinecontract.CognitionBootstrapAutoEligibilityV1,
		AutomationMode:         policy.Mode,
		AutomationPolicySource: policy.Source,
		FreshBootstrap: envelope.Plan.Operation == cognitionplan.OperationBootstrap &&
			envelope.Plan.Layout == machinecontract.CognitionPlannerUninitialized &&
			envelope.Plan.Status == machinecontract.CognitionPlannerAuthoringRequired,
		PreviewReady:       envelope.Preview.Status == machinecontract.CognitionPlannerPreviewReady && envelope.Preview.ApprovalDigest != nil,
		RiskCount:          len(envelope.Preview.Risks),
		ReviewVisibleCount: envelope.Plan.SafeInventory.ReviewVisibleCount,
		BusinessRowsRead:   0, DDLDMLStatements: 0,
		NetworkAccessed: envelope.NetworkAccessed || envelope.Plan.NetworkAccessed || envelope.Preview.NetworkAccessed,
		TTYRequired:     false, ModelSelfApproval: false, Blockers: []AutoEligibilityBlocker{},
	}
	provenance := envelope.Preview.SemanticAuthoringProvenance
	projection.ProvenanceVerified = provenance != nil &&
		provenance.Status == machinecontract.SemanticAuthoringStatusVerified &&
		provenance.Origin == machinecontract.SemanticAuthoringOriginHostModel &&
		provenance.ReceiptSHA256 != ""
	projection.DatabaseSideEffects = projection.BusinessRowsRead != 0 || projection.DDLDMLStatements != 0

	add := func(code string, count int) {
		if count <= 0 {
			return
		}
		projection.Blockers = append(projection.Blockers, AutoEligibilityBlocker{Code: code, Count: count})
	}
	if projection.AutomationMode != config.AutomationModeAuto {
		add(machinecontract.CognitionAutoBlockerPolicyNotAuto, 1)
	}
	currentConfig, configErr := config.LoadReadOnly(root)
	if configErr != nil || currentConfig.ResolveOnboardingAutomation(true) != policy {
		add(machinecontract.CognitionAutoBlockerPolicyDrift, 1)
	}
	if !projection.FreshBootstrap {
		add(machinecontract.CognitionAutoBlockerNotFresh, 1)
	}
	add(machinecontract.CognitionAutoBlockerSensitiveRead, envelope.Plan.SafeInventory.AutoBlockerCount)
	if !projection.ProvenanceVerified {
		add(machinecontract.CognitionAutoBlockerProvenance, 1)
	}
	if !projection.PreviewReady {
		add(machinecontract.CognitionAutoBlockerPreview, 1)
	}
	add(machinecontract.CognitionAutoBlockerRisk, projection.RiskCount)
	if projection.NetworkAccessed {
		add(machinecontract.CognitionAutoBlockerNetworkSideEffect, 1)
	}
	if projection.DatabaseSideEffects {
		add(machinecontract.CognitionAutoBlockerDatabaseSideEffect, 1)
	}

	currentPreview, replayErr := cognitionplan.ValidateCandidate(root, &envelope.Plan, &envelope.Candidate)
	projection.PlanStable = replayErr == nil && currentPreview.Status != machinecontract.CognitionPlannerSuperseded &&
		len(cognitionplan.PreviewReplayMismatches(&envelope.Preview, currentPreview)) == 0
	if !projection.PlanStable {
		add(machinecontract.CognitionAutoBlockerPlanDrift, 1)
	}
	projection.SourceStable = cognitionplan.ValidateExternalGuards(root, &envelope.Plan) == nil
	if !projection.SourceStable {
		add(machinecontract.CognitionAutoBlockerSourceDrift, 1)
	}

	if _, exists, loadErr := baseline.Load(root); loadErr != nil || exists {
		add(machinecontract.CognitionAutoBlockerFormalState, 1)
	}
	if present, historyErr := matureGovernanceHistoryPresent(root); historyErr != nil || present {
		add(machinecontract.CognitionAutoBlockerGovernanceHistory, 1)
	}
	pending, pendingErr := cognitiontxn.Pending(root)
	projection.RecoveryAvailable = pendingErr == nil && len(pending) == 0
	if !projection.RecoveryAvailable {
		add(machinecontract.CognitionAutoBlockerRecovery, 1)
	}
	status, statusErr := inspectEnvelopeState(root, "auto-eligibility", envelope, false)
	projection.CASAvailable = statusErr == nil && status.Status == StatusPrepared &&
		!status.ThirdPartyConflict && autoRootPreimageAvailable(root, envelope)
	if !projection.CASAvailable {
		add(machinecontract.CognitionAutoBlockerCAS, 1)
	}
	if containsTargetKind(envelope, "database") && !currentDatabaseEvidenceAccepted(root) {
		add(machinecontract.CognitionAutoBlockerDatabaseEvidence, 1)
	}

	sort.Slice(projection.Blockers, func(i, j int) bool { return projection.Blockers[i].Code < projection.Blockers[j].Code })
	for _, blocker := range projection.Blockers {
		projection.AutoBlockerCount += blocker.Count
	}
	projection.AutoReady = projection.AutoBlockerCount == 0
	if projection.AutoReady {
		projection.AuthorizationMechanism = AutoApprovalMechanism
	}
	return projection, nil
}

func autoRootPreimageAvailable(root string, envelope *ApplyEnvelope) bool {
	target := formalPostimageByPath(envelope, "aoci.txt")
	if target == nil {
		return false
	}
	path := filepath.Join(root, "aoci.txt")
	switch target.ExpectedPreimage {
	case PreimageAbsent:
		_, err := os.Lstat(path)
		return errors.Is(err, os.ErrNotExist)
	case PreimageOfficialMinimal:
		current, err := os.ReadFile(path)
		if err != nil || sha256Hex(current) != target.PreimageSHA256 || string(current) != target.PreimageContent {
			return false
		}
		official, err := cognitionplan.OfficialMinimalSkeleton(root, current)
		return err == nil && official
	default:
		return false
	}
}

func currentDatabaseEvidenceAccepted(root string) bool {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return false
	}
	accepted, exists, err := dbevidence.LoadBaseline(root)
	if err != nil || !exists {
		return false
	}
	acceptedBySource := map[string]string{}
	for _, source := range accepted.Sources {
		acceptedBySource[source.SourceID] = source.SourceSnapshotSHA256
	}
	for _, source := range cfg.DatabaseSources {
		if !source.Enabled {
			continue
		}
		_, snapshot, exists, err := dbevidence.LoadSnapshot(root, source.SourceID)
		if err != nil || !exists || acceptedBySource[source.SourceID] != snapshot.SourceSnapshotSHA256 {
			return false
		}
	}
	return true
}

func autoEligibilityError(projection *AutoEligibility) error {
	if projection == nil || projection.AutoReady {
		return nil
	}
	codes := make([]string, 0, len(projection.Blockers))
	for _, blocker := range projection.Blockers {
		codes = append(codes, fmt.Sprintf("%s:%d", blocker.Code, blocker.Count))
	}
	return fmt.Errorf("bootstrap_auto_eligibility_blocked: %s", strings.Join(codes, ","))
}

func validBootstrapAutomationPolicy(policy config.AutomationPolicy) bool {
	mode, err := config.ParseAutomationMode(policy.Mode)
	if err != nil || mode != policy.Mode {
		return false
	}
	switch policy.Source {
	case machinecontract.CognitionAutomationPolicyFreshDefault:
		return mode == config.AutomationModeAuto
	case machinecontract.CognitionAutomationPolicyTeamConfig:
		return mode == config.AutomationModeAuto || mode == config.AutomationModeReview || mode == config.AutomationModeOff
	default:
		return false
	}
}
