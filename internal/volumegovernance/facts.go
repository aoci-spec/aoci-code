// Package volumegovernance computes the deterministic, read-only governance
// facts shared by Volumes-aware Verify, Check, Guide, and Maintain. It owns no
// semantic authoring, authorization, Baseline advancement, or repair.
package volumegovernance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

const Version = "volumes-governance-facts/v1"

const (
	ResultAligned           = "aligned"
	ResultAuthoringRequired = "authoring_required"
	ResultEvidenceRequired  = "evidence_required"
	ResultBlocked           = "blocked"
)

type AssetState struct {
	Enabled     bool   `json:"enabled"`
	Applicable  bool   `json:"applicable"`
	DomainState string `json:"domain_state"`
	AssetState  string `json:"asset_state"`
	Path        string `json:"path,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ObjectCount int    `json:"object_count"`
}

type Drift struct {
	Missing         []string `json:"missing"`
	Orphan          []string `json:"orphan"`
	Stale           []string `json:"stale"`
	Unbaselined     []string `json:"unbaselined"`
	LineEndingOnly  []string `json:"line_ending_only"`
	ObservedNew     []string `json:"observed_new"`
	ObservedChanged []string `json:"observed_changed"`
	ObservedRemoved []string `json:"observed_removed"`
}

type ManagedScopeFacts struct {
	Aligned               bool   `json:"aligned"`
	ScopeChangeRequired   bool   `json:"scope_change_required"`
	PolicyIdentity        string `json:"policy_identity,omitempty"`
	ActivePolicyIdentity  string `json:"active_policy_identity,omitempty"`
	IndexCount            int    `json:"index_count"`
	ObserveCount          int    `json:"observe_count"`
	ExcludeCount          int    `json:"exclude_count"`
	ObservedPendingReview int    `json:"observed_pending_review"`
}

type BudgetFacts struct {
	Mode             string                      `json:"mode"`
	Status           string                      `json:"status"`
	WholeIndexTokens int                         `json:"whole_index_tokens"`
	TargetTokens     int                         `json:"target_tokens"`
	WarningTokens    int                         `json:"warning_tokens"`
	MaxTokens        int                         `json:"max_tokens"`
	R                []cognitionbudget.FieldBand `json:"r"`
	S                []cognitionbudget.FieldBand `json:"s"`
	Violations       []cognitionbudget.Violation `json:"violations"`
}

type Finding struct {
	Code             string `json:"code"`
	Domain           string `json:"domain,omitempty"`
	Target           string `json:"target,omitempty"`
	Cause            string `json:"cause,omitempty"`
	ExpectedOwner    string `json:"expected_owner,omitempty"`
	ActualOwner      string `json:"actual_owner,omitempty"`
	AffectedPath     string `json:"affected_path,omitempty"`
	SafeRepairAction string `json:"safe_repair_action,omitempty"`
}

type DatabaseEvidenceFacts struct {
	State           string `json:"state"`
	Configured      int    `json:"configured_sources"`
	Accepted        int    `json:"accepted_sources"`
	BaselineCurrent bool   `json:"baseline_current"`
}

type Facts struct {
	Version              string                 `json:"version"`
	Layout               string                 `json:"layout"`
	EnabledDomains       []string               `json:"enabled_domains"`
	StructureValid       bool                   `json:"structure_valid"`
	GovernanceAligned    bool                   `json:"governance_aligned"`
	CompositeIdentity    string                 `json:"composite_identity"`
	Root                 AssetState             `json:"root"`
	Meta                 AssetState             `json:"meta"`
	Code                 AssetState             `json:"code"`
	Database             AssetState             `json:"database"`
	CodeSourceCount      int                    `json:"code_source_count"`
	CodeEntryCount       int                    `json:"code_entry_count"`
	DatabaseEntryCount   int                    `json:"database_entry_count"`
	DatabaseBindingCount int                    `json:"database_binding_count"`
	CodeDrift            Drift                  `json:"code_drift"`
	ManagedScope         ManagedScopeFacts      `json:"managed_scope"`
	Budget               BudgetFacts            `json:"budget"`
	DatabaseCognition    dbcognition.Assessment `json:"database_cognition"`
	DatabaseEvidence     DatabaseEvidenceFacts  `json:"database_evidence"`
	RelationFindings     []cognition.Finding    `json:"relation_findings"`
	PendingTransactions  int                    `json:"pending_transactions"`
	RecoveryPending      bool                   `json:"recovery_pending"`
	ThirdPartyConflict   bool                   `json:"third_party_conflict"`
	NetworkAccessed      bool                   `json:"network_accessed"`
	BusinessSourceSHA256 string                 `json:"business_source_sha256,omitempty"`
	Result               string                 `json:"result"`
	AffectedDomains      []string               `json:"affected_domains"`
	NextRequiredAction   string                 `json:"next_required_action"`
	Findings             []Finding              `json:"findings"`
}

// Assess consumes an already validated CognitionSet and current local
// governance authorities. The result is deterministic for the same formal
// bytes, Baseline, configuration, Curation, and saved Database Evidence.
func Assess(root string, cfg *config.Config, set *cognition.Set) (*Facts, error) {
	if cfg == nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, fmt.Errorf("volumes_governance_input_invalid")
	}
	facts := &Facts{
		Version: Version, Layout: set.LayoutMode, StructureValid: len(set.Errors) == 0,
		CompositeIdentity: set.CompositeIdentity, EnabledDomains: []string{},
		CodeDrift: emptyDrift(), RelationFindings: append([]cognition.Finding{}, set.Warnings...),
		Findings: []Finding{}, AffectedDomains: []string{}, NetworkAccessed: false,
		Root: assetFacts(&set.Root, true), Meta: assetFacts(&set.Meta, true),
		Code: absentAsset("aoci.code.txt"), Database: absentAsset("aoci.database.txt"),
		ManagedScope: ManagedScopeFacts{Aligned: true},
	}

	state, exists, err := baseline.Load(root)
	if err != nil {
		return nil, fmt.Errorf("volumes_governance_baseline_invalid: %w", err)
	}
	if !exists || state == nil {
		state = baseline.NewBaseline(nil)
		facts.Findings = append(facts.Findings, Finding{Code: "baseline_missing"})
	}

	if asset := enabledAsset(set, cognition.ScopeCode); asset != nil {
		facts.EnabledDomains = append(facts.EnabledDomains, cognition.ScopeCode)
		facts.Code = assetFacts(asset, true)
		facts.CodeEntryCount = asset.ObjectCount
		assessCode(root, cfg, set, state, facts)
	}
	if asset := enabledAsset(set, cognition.ScopeDatabase); asset != nil {
		facts.EnabledDomains = append(facts.EnabledDomains, cognition.ScopeDatabase)
		facts.Database = assetFacts(asset, true)
		facts.DatabaseEntryCount = asset.ObjectCount
		facts.DatabaseCognition = dbcognition.Assess(root, cfg.DatabaseSources, set, state)
		if state.DatabaseCognition != nil {
			facts.DatabaseBindingCount = len(state.DatabaseCognition.Entries)
		}
		assessDatabase(root, cfg, facts)
		if !baselineMatches(root, state, asset.Descriptor.Path) {
			facts.Findings = append(facts.Findings, Finding{Code: "database_volume_unbaselined", Domain: cognition.ScopeDatabase, Target: asset.Descriptor.Path})
		}
	} else {
		facts.DatabaseCognition = dbcognition.Assess(root, nil, set, state)
		facts.DatabaseEvidence.State = "not_applicable"
	}

	manifest, manifestErr := businesssource.Build(root, "")
	if manifestErr != nil {
		facts.Findings = append(facts.Findings, Finding{Code: "business_source_manifest_invalid", Cause: manifestErr.Error()})
	} else {
		facts.BusinessSourceSHA256 = manifest.AggregateSHA256
	}

	facts.Budget = AssessProjectedBudget(cfg, set)
	if facts.Budget.Mode == machinecontract.BudgetModeEnforce && len(facts.Budget.Violations) > 0 {
		facts.Findings = append(facts.Findings, Finding{Code: "cognition_budget_exceeded"})
	}

	pending, pendingErr := pendingTransactions(root)
	if pendingErr != nil {
		facts.Findings = append(facts.Findings, Finding{Code: "pending_transaction_state_invalid"})
	} else {
		facts.PendingTransactions = pending
		facts.RecoveryPending = pending > 0
		if pending > 0 {
			facts.Findings = append(facts.Findings, Finding{Code: "recovery_pending"})
		}
	}

	for _, finding := range facts.RelationFindings {
		facts.Findings = append(facts.Findings, Finding{Code: finding.Code, Domain: finding.AssetID})
	}
	finalize(facts)
	return facts, nil
}

func enabledAsset(set *cognition.Set, id string) *cognition.Asset {
	asset := set.Volumes[id]
	if asset == nil || asset.Descriptor.State == machinecontract.CognitionVolumeDisabled {
		return nil
	}
	return asset
}

func assetFacts(asset *cognition.Asset, applicable bool) AssetState {
	if asset == nil {
		return AssetState{AssetState: cognition.AssetAbsent, Applicable: applicable}
	}
	return AssetState{Enabled: true, Applicable: applicable, DomainState: "enabled", AssetState: asset.State,
		Path: asset.Descriptor.Path, SHA256: asset.SHA256, ObjectCount: asset.ObjectCount}
}

func absentAsset(path string) AssetState {
	return AssetState{Enabled: false, Applicable: false, DomainState: "not_applicable", AssetState: cognition.AssetAbsent, Path: path}
}

func emptyDrift() Drift {
	return Drift{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}}
}

func assessCode(root string, cfg *config.Config, set *cognition.Set, baselineState *baseline.Baseline, facts *Facts) {
	asset := set.Volumes[cognition.ScopeCode]
	ownershipConflicts := map[string]cognition.OwnershipConflict{}
	ownershipOrphans := []string{}
	for _, conflict := range cognition.OwnershipConflicts(set) {
		if conflict.ActualOwner != cognition.OwnerCode {
			continue
		}
		ownershipConflicts[conflict.Path] = conflict
		ownershipOrphans = append(ownershipOrphans, conflict.Path)
		facts.Findings = append(facts.Findings, Finding{
			Code: "code_orphan", Domain: cognition.ScopeCode, Target: conflict.Path,
			Cause: "volume_ownership_conflict", ExpectedOwner: conflict.ExpectedOwner,
			ActualOwner: conflict.ActualOwner, AffectedPath: conflict.Path,
			SafeRepairAction: "aoci_remove_entry path=" + conflict.ObjectRef,
		})
	}
	ownershipOrphans = sortedUnique(ownershipOrphans)
	facts.CodeDrift.Orphan = ownershipOrphans
	managed, err := managedstate.Load(root, cfg)
	if err != nil {
		facts.Findings = append(facts.Findings, Finding{Code: "managed_scope_invalid", Domain: cognition.ScopeCode})
		return
	}
	facts.ManagedScope = ManagedScopeFacts{Aligned: !managed.ScopeChangeRequired,
		ScopeChangeRequired: managed.ScopeChangeRequired, PolicyIdentity: managed.DesiredPolicyIdentity,
		ActivePolicyIdentity: managed.ActivePolicyIdentity}
	if managed.Evaluation != nil {
		facts.ManagedScope.IndexCount = managed.Evaluation.IndexCount
		facts.ManagedScope.ObserveCount = managed.Evaluation.ObserveCount
		facts.ManagedScope.ExcludeCount = managed.Evaluation.ExcludeCount
		for _, item := range managed.Evaluation.Index {
			if _, formal := cognition.FormalAssetOwner(item.Path); !formal && cognition.ExpectedOwner(item.Path) == cognition.OwnerCode {
				facts.CodeSourceCount++
			}
		}
	}
	if managed.ScopeChangeRequired {
		facts.Findings = append(facts.Findings, Finding{Code: "scope_change_required", Domain: cognition.ScopeCode})
		return
	}
	copyState := *managed
	copyState.Snapshot = cloneSnapshot(managed.Snapshot)
	delete(copyState.Snapshot, set.Root.Descriptor.Path)
	for _, formal := range set.Volumes {
		if formal != nil {
			delete(copyState.Snapshot, formal.Descriptor.Path)
		}
	}
	detected, err := managedstate.Detect(root, cfg, asset.Document, &copyState)
	if err != nil {
		facts.Findings = append(facts.Findings, Finding{Code: "code_drift_unavailable", Domain: cognition.ScopeCode})
		return
	}
	orphans := append(append([]string{}, detected.Orphan...), ownershipOrphans...)
	orphans = sortedUnique(orphans)
	facts.CodeDrift = Drift{Missing: detected.Missing, Orphan: orphans, Stale: detected.Stale,
		Unbaselined: detected.Unbaselined, LineEndingOnly: detected.LineEndingOnly,
		ObservedNew: detected.ObservedNew, ObservedChanged: detected.ObservedChanged, ObservedRemoved: detected.ObservedRemoved}
	if managed.Evaluation == nil {
		facts.CodeSourceCount = len(copyState.Snapshot)
	}
	if cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
		facts.ManagedScope.ObservedPendingReview = len(detected.ObservedNew) + len(detected.ObservedChanged) + len(detected.ObservedRemoved)
	}
	for _, item := range []struct {
		code string
		set  []string
	}{{"code_missing", detected.Missing}, {"code_stale", detected.Stale}, {"code_unbaselined", detected.Unbaselined}} {
		for _, target := range item.set {
			facts.Findings = append(facts.Findings, Finding{Code: item.code, Domain: cognition.ScopeCode, Target: target})
		}
	}
	for _, target := range orphans {
		if _, ownershipConflict := ownershipConflicts[target]; ownershipConflict {
			continue
		}
		facts.Findings = append(facts.Findings, Finding{Code: "code_orphan", Domain: cognition.ScopeCode, Target: target})
	}
	if facts.ManagedScope.ObservedPendingReview > 0 {
		facts.Findings = append(facts.Findings, Finding{Code: "observed_pending", Domain: cognition.ScopeCode})
	}
	if !baselineMatches(root, baselineState, asset.Descriptor.Path) {
		facts.Findings = append(facts.Findings, Finding{Code: "code_volume_unbaselined", Domain: cognition.ScopeCode, Target: asset.Descriptor.Path})
	}
}

func assessDatabase(root string, cfg *config.Config, facts *Facts) {
	assessment := facts.DatabaseCognition
	facts.DatabaseEvidence.Configured = len(cfg.DatabaseSources)
	if len(cfg.DatabaseSources) == 0 {
		facts.DatabaseEvidence.State = ResultEvidenceRequired
		facts.Findings = append(facts.Findings, Finding{Code: "database_evidence_required", Domain: cognition.ScopeDatabase})
		return
	}
	evidenceBaseline, baselineExists, baselineErr := dbevidence.LoadBaseline(root)
	if baselineErr != nil {
		facts.DatabaseEvidence.State = "invalid"
		facts.Findings = append(facts.Findings, Finding{Code: "database_evidence_invalid", Domain: cognition.ScopeDatabase})
	} else if !baselineExists {
		facts.DatabaseEvidence.State = ResultEvidenceRequired
		appendDatabaseSourceFindings(facts, cfg.DatabaseSources, "database_evidence_baseline_required")
	} else {
		accepted := map[string]string{}
		for _, source := range evidenceBaseline.Sources {
			accepted[source.SourceID] = source.SourceSnapshotSHA256
		}
		current := true
		for _, source := range cfg.DatabaseSources {
			if !source.Enabled {
				continue
			}
			_, snapshot, exists, err := dbevidence.LoadSnapshot(root, source.SourceID)
			if err != nil || !exists || accepted[source.SourceID] != snapshot.SourceSnapshotSHA256 {
				current = false
				facts.Findings = append(facts.Findings, Finding{Code: "database_evidence_baseline_stale", Domain: cognition.ScopeDatabase, Target: source.SourceID})
				continue
			}
			facts.DatabaseEvidence.Accepted++
		}
		facts.DatabaseEvidence.BaselineCurrent = current
		if current {
			facts.DatabaseEvidence.State = machinecontract.DatabaseCognitionCurrent
		} else {
			facts.DatabaseEvidence.State = ResultEvidenceRequired
		}
	}
	for _, source := range assessment.Sources {
		if source.State != machinecontract.DatabaseCognitionCurrent {
			facts.Findings = append(facts.Findings, Finding{Code: source.State, Domain: cognition.ScopeDatabase, Target: source.SourceID})
		}
	}
	for _, item := range assessment.Items {
		if item.State != machinecontract.DatabaseCognitionCurrent {
			facts.Findings = append(facts.Findings, Finding{Code: "database_" + item.State, Domain: cognition.ScopeDatabase, Target: item.ObjectRef})
		}
	}
}

func appendDatabaseSourceFindings(facts *Facts, sources []dbevidence.SourceConfig, code string) {
	added := false
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		facts.Findings = append(facts.Findings, Finding{Code: code, Domain: cognition.ScopeDatabase, Target: source.SourceID})
		added = true
	}
	if !added {
		facts.Findings = append(facts.Findings, Finding{Code: code, Domain: cognition.ScopeDatabase})
	}
}

// AssessProjectedBudget applies the same deterministic Volume budget facts to
// an in-memory projected CognitionSet before any formal write.
func AssessProjectedBudget(cfg *config.Config, set *cognition.Set) BudgetFacts {
	policy, err := cognitionbudget.Normalize(cfg.EffectiveCognitionBudget())
	if err != nil {
		return BudgetFacts{Status: machinecontract.BudgetStatusExceeded,
			Violations: []cognitionbudget.Violation{{Code: "cognition_budget_policy_invalid"}}}
	}
	bytes := len(set.Root.Raw) + len(set.Meta.Raw)
	violations := []cognitionbudget.Violation{}
	for _, id := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		asset := enabledAsset(set, id)
		if asset == nil {
			continue
		}
		bytes += len(asset.Raw)
		for _, object := range asset.Objects {
			for _, violation := range cognitionbudget.ValidateEntry(object.CanonicalLine, policy) {
				violation.Path = object.CanonicalRef
				violations = append(violations, violation)
			}
		}
	}
	tokens := bytes / 3
	if tokens > policy.WholeIndex.MaxTokens {
		violations = append(violations, cognitionbudget.Violation{Code: "whole_index_budget_exceeded", Actual: tokens, Maximum: policy.WholeIndex.MaxTokens})
	}
	status := machinecontract.BudgetStatusHealthy
	switch {
	case tokens > policy.WholeIndex.MaxTokens:
		status = machinecontract.BudgetStatusExceeded
	case tokens >= policy.WholeIndex.WarningTokens:
		status = machinecontract.BudgetStatusWarning
	case tokens > policy.WholeIndex.TargetTokens:
		status = machinecontract.BudgetStatusNearBudget
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Field < violations[j].Field
	})
	return BudgetFacts{Mode: policy.Mode, Status: status, WholeIndexTokens: tokens,
		TargetTokens: policy.WholeIndex.TargetTokens, WarningTokens: policy.WholeIndex.WarningTokens,
		MaxTokens: policy.WholeIndex.MaxTokens, R: append([]cognitionbudget.FieldBand{}, policy.R...),
		S: append([]cognitionbudget.FieldBand{}, policy.S...), Violations: violations}
}

func baselineMatches(root string, state *baseline.Baseline, rel string) bool {
	if state == nil {
		return false
	}
	stored, ok := state.Files[rel]
	current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && ok && stored.SHA256 == current.SHA256
}

func cloneSnapshot(input map[string]baseline.Fingerprint) map[string]baseline.Fingerprint {
	result := make(map[string]baseline.Fingerprint, len(input))
	for path, fingerprint := range input {
		result[path] = fingerprint
	}
	return result
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func pendingTransactions(root string) (int, error) {
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return 0, err
	}
	count := len(pending)
	entries, err := os.ReadDir(filepath.Join(root, ".aoci", "transactions"))
	if os.IsNotExist(err) {
		return count, nil
	}
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "entries-") && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count, nil
}

func finalize(facts *Facts) {
	blocked, evidence, authoring := false, false, false
	domains := map[string]bool{}
	for _, finding := range facts.Findings {
		if finding.Domain != "" {
			domains[finding.Domain] = true
		}
		switch {
		case strings.HasPrefix(finding.Code, "database_evidence") || strings.Contains(finding.Code, "evidence_unavailable") || strings.Contains(finding.Code, "evidence_invalid"):
			evidence = true
		case finding.Code == "code_missing" || finding.Code == "code_stale" || finding.Code == "code_unbaselined" ||
			strings.HasPrefix(finding.Code, "database_missing") || strings.HasPrefix(finding.Code, "database_stale") || strings.HasPrefix(finding.Code, "database_unbaselined") ||
			strings.HasPrefix(finding.Code, "database_cognition_missing") || strings.HasPrefix(finding.Code, "database_cognition_stale") || strings.HasPrefix(finding.Code, "database_cognition_unbaselined"):
			authoring = true
		default:
			blocked = true
		}
	}
	facts.AffectedDomains = []string{}
	for _, domain := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		if domains[domain] {
			facts.AffectedDomains = append(facts.AffectedDomains, domain)
		}
	}
	switch {
	case blocked:
		facts.Result, facts.NextRequiredAction = ResultBlocked, ResultBlocked
	case evidence:
		facts.Result = ResultEvidenceRequired
		facts.NextRequiredAction = facts.DatabaseCognition.NextAction
		if facts.NextRequiredAction == "" || facts.NextRequiredAction == machinecontract.DatabaseCognitionActionNoActionRequired {
			facts.NextRequiredAction = machinecontract.DatabaseCognitionActionSnapshotOrRepair
		}
	case authoring:
		facts.Result, facts.NextRequiredAction = ResultAuthoringRequired, ResultAuthoringRequired
	default:
		facts.Result, facts.NextRequiredAction = ResultAligned, "none"
	}
	facts.GovernanceAligned = facts.StructureValid && facts.Result == ResultAligned
}
