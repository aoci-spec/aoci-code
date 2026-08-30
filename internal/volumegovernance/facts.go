// Package volumegovernance computes the deterministic, read-only governance
// facts shared by Volumes-aware Verify, Check, Guide, and Maintain. It owns no
// semantic authoring, authorization, Baseline advancement, or repair.
package volumegovernance

import (
	"errors"
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
	afs "github.com/aoci-spec/aoci-code/internal/fs"
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
	// ListTruncation is present only on a transport projection produced by
	// BoundListsForTransport: it names every enumeration that was cut to the
	// leading sample and carries the complete counts. Verify, Check, and Guide
	// never set it; their lists stay complete.
	ListTruncation *ListTruncation `json:"list_truncation,omitempty"`
}

// ListTruncation reports the sample bound applied to per-item enumerations
// and the full count of every list that exceeded it.
type ListTruncation struct {
	Limit  int            `json:"limit"`
	Totals map[string]int `json:"totals"`
}

// BoundListsForTransport returns a copy of facts whose per-item enumerations
// keep at most limit leading items, recording each cut list's complete count
// in ListTruncation. Findings, drift lists, relation findings, and Database
// cognition items are situational awareness in a Maintain response; the
// actionable set is the candidate list, and the complete enumerations remain
// available from Verify and Check. Counts and every scalar fact are unchanged,
// so the projection never alters a governance decision.
func BoundListsForTransport(facts *Facts, limit int) *Facts {
	if facts == nil || limit <= 0 {
		return facts
	}
	bounded := *facts
	totals := map[string]int{}
	cutStrings := func(name string, values []string) []string {
		if len(values) <= limit {
			return values
		}
		totals[name] = len(values)
		return append([]string{}, values[:limit]...)
	}
	bounded.CodeDrift = Drift{
		Missing:         cutStrings("code_drift.missing", facts.CodeDrift.Missing),
		Orphan:          cutStrings("code_drift.orphan", facts.CodeDrift.Orphan),
		Stale:           cutStrings("code_drift.stale", facts.CodeDrift.Stale),
		Unbaselined:     cutStrings("code_drift.unbaselined", facts.CodeDrift.Unbaselined),
		LineEndingOnly:  cutStrings("code_drift.line_ending_only", facts.CodeDrift.LineEndingOnly),
		ObservedNew:     cutStrings("code_drift.observed_new", facts.CodeDrift.ObservedNew),
		ObservedChanged: cutStrings("code_drift.observed_changed", facts.CodeDrift.ObservedChanged),
		ObservedRemoved: cutStrings("code_drift.observed_removed", facts.CodeDrift.ObservedRemoved),
	}
	if len(facts.Findings) > limit {
		totals["findings"] = len(facts.Findings)
		bounded.Findings = append([]Finding{}, facts.Findings[:limit]...)
	}
	if len(facts.RelationFindings) > limit {
		totals["relation_findings"] = len(facts.RelationFindings)
		bounded.RelationFindings = append([]cognition.Finding{}, facts.RelationFindings[:limit]...)
	}
	if len(facts.DatabaseCognition.Items) > limit {
		totals["database_cognition.items"] = len(facts.DatabaseCognition.Items)
		bounded.DatabaseCognition.Items = append([]dbcognition.Item{}, facts.DatabaseCognition.Items[:limit]...)
	}
	if len(totals) > 0 {
		bounded.ListTruncation = &ListTruncation{Limit: limit, Totals: totals}
	}
	return &bounded
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

	// Root and Meta are checked here because nothing else did. Their drift was
	// invisible to Guide and Verify while internal/scopechange/plan.go refused
	// every Scope Change over it, so two authorities described the same
	// repository differently and only one of them named a file.
	assessFormalAsset(root, cfg, state, set.Root.Descriptor.Path, "root", facts)
	assessFormalAsset(root, cfg, state, set.Meta.Descriptor.Path, "meta", facts)

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
		switch matched, lineEndingOnly := baselineMatches(root, cfg, state, asset.Descriptor.Path); {
		case matched && lineEndingOnly:
			facts.Findings = append(facts.Findings, Finding{
				Code: "database_volume_line_ending_only", Domain: cognition.ScopeDatabase, Target: asset.Descriptor.Path,
				Cause:            "line_ending_rewrite",
				SafeRepairAction: "restore LF line endings in this Volume, or set \"* text=auto eol=lf\" in .gitattributes and check the file out again",
			})
		case !matched:
			facts.Findings = append(facts.Findings, Finding{
				Code: "database_volume_unbaselined", Domain: cognition.ScopeDatabase, Target: asset.Descriptor.Path,
				SafeRepairAction: volumeUnbaselinedRepairAction(root, asset.Descriptor.Path, state),
			})
		}
	} else {
		facts.DatabaseCognition = dbcognition.Assess(root, nil, set, state)
		facts.DatabaseEvidence.State = "not_applicable"
	}

	// A manifest failure used to collapse into one generic blocker no matter what
	// caused it, which was worst for the most common cause: a Managed Scope policy
	// that no longer matches its receipt already reports scope_change_required, and
	// naming it a second time as a business-source failure sent operators to look
	// in a subsystem that was working. The derived report is dropped, and every
	// other cause now carries the exact machine token instead of being erased.
	switch manifest, manifestErr := businesssource.Build(root, ""); {
	case manifestErr == nil:
		facts.BusinessSourceSHA256 = manifest.AggregateSHA256
	case errors.Is(manifestErr, businesssource.ErrScopeChangeRequired):
	default:
		facts.Findings = append(facts.Findings, Finding{
			Code:  "business_source_manifest_invalid",
			Cause: businessSourceCause(manifestErr),
		})
	}

	facts.Budget = AssessProjectedBudget(cfg, set)
	if facts.Budget.Mode == machinecontract.BudgetModeEnforce && len(facts.Budget.Violations) > 0 {
		// The code alone left the operator to guess. A budget is a project-owned
		// policy, so both levers are named: reduce what the index governs, or
		// raise the budget through the governed transaction. Neither is implied
		// by the code, and compression alone is the wrong answer for an index
		// that grew because the repository did.
		facts.Findings = append(facts.Findings, Finding{
			Code:             "cognition_budget_exceeded",
			Cause:            budgetExceededCause(facts.Budget),
			SafeRepairAction: budgetExceededRepairAction(facts.Budget),
		})
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
	switch matched, lineEndingOnly := baselineMatches(root, cfg, baselineState, asset.Descriptor.Path); {
	case matched && lineEndingOnly:
		// Equivalent under the team's own tolerance, so it neither blocks nor
		// creates authoring debt. It is still reported, because the raw bytes did
		// move and a Scope Change will refuse until they are restored.
		facts.Findings = append(facts.Findings, Finding{
			Code: "code_volume_line_ending_only", Domain: cognition.ScopeCode, Target: asset.Descriptor.Path,
			Cause:            "line_ending_rewrite",
			SafeRepairAction: "restore LF line endings in this Volume, or set \"* text=auto eol=lf\" in .gitattributes and check the file out again",
		})
	case !matched:
		facts.Findings = append(facts.Findings, Finding{
			Code: "code_volume_unbaselined", Domain: cognition.ScopeCode, Target: asset.Descriptor.Path,
			SafeRepairAction: volumeUnbaselinedRepairAction(root, asset.Descriptor.Path, baselineState),
		})
	}
}

// businessSourceCause keeps the stable machine token a manifest failure already
// carries, and nothing after it.
//
// Every error the builder returns starts with a business_source_* token and may
// append a path or a wrapped error. The token is what tells an operator which
// subsystem to look at; the remainder can name a repository path, so it stays
// out of a governance fact.
func businessSourceCause(err error) string {
	if err == nil {
		return ""
	}
	token := err.Error()
	if separator := strings.IndexAny(token, ": "); separator > 0 {
		token = token[:separator]
	}
	if !strings.HasPrefix(token, "business_source_") {
		return "business_source_unavailable"
	}
	return token
}

// assessFormalAsset reports Root and Meta drift with the same vocabulary the
// Code and Database Volumes use, so one repository cannot be aligned according
// to Guide and drifted according to Scope.
func assessFormalAsset(
	root string,
	cfg *config.Config,
	state *baseline.Baseline,
	rel string,
	kind string,
	facts *Facts,
) {
	if strings.TrimSpace(rel) == "" || state == nil {
		return
	}
	// Only a record that exists and no longer matches is reported. That is the
	// exact condition internal/scopechange/plan.go refuses a Scope Change over,
	// and matching it is the whole point: a repository must not be aligned
	// according to Guide and drifted according to Scope. An absent binding is
	// skipped there too, and several legal layouts never bind Root or Meta.
	if _, recorded := state.Files[rel]; !recorded {
		return
	}
	// Both outcomes are informational. The defect being fixed is invisibility,
	// not insufficient blocking: internal/scopechange/plan.go refuses a Scope
	// Change over this drift while Verify and Guide said nothing, so one
	// repository had two authorities and only one of them named a file. Guide now
	// names it too. Turning it into a stop instead would invent a new hard state
	// in flows that legitimately reach it, and no domain is attributed because
	// Root and Meta belong to none.
	switch matched, lineEndingOnly := baselineMatches(root, cfg, state, rel); {
	case matched && lineEndingOnly:
		facts.Findings = append(facts.Findings, Finding{
			Code: kind + "_volume_line_ending_only", Target: rel,
			Cause:            "line_ending_rewrite",
			SafeRepairAction: "restore LF line endings in this Volume, or set \"* text=auto eol=lf\" in .gitattributes and check the file out again",
		})
	case !matched:
		facts.Findings = append(facts.Findings, Finding{
			Code: kind + "_volume_baseline_drift", Target: rel,
			SafeRepairAction: volumeUnbaselinedRepairAction(root, rel, state),
		})
	}
}

// volumeUnbaselinedRepairAction names why the Volume is not in the Baseline, so
// the operator never has to read source to find out.
//
// The two causes need opposite fixes and nothing else distinguishes them: a
// Volume absent from the Baseline was hidden from Git when the Baseline was
// built, while a Volume present but mismatched has had its bytes rewritten.
func volumeUnbaselinedRepairAction(root, rel string, state *baseline.Baseline) string {
	if state != nil {
		if _, recorded := state.Files[rel]; recorded {
			return "this Volume's bytes changed after the Baseline was established; restore them, or re-establish cognition through the governed Scope Change flow"
		}
	}
	if ignored, _ := afs.PathIgnoredByGit(root, rel); ignored {
		return "this Volume is hidden from Git, so scan never recorded it; remove the ignore rule covering it and run scan again"
	}
	return "this Volume has no Baseline record; run scan on a repository where the Volume is visible to Git"
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

// budgetExceededCause states which of the two budget gates fired, with the
// numbers, so an operator does not have to re-derive them from a separate
// command to know what refused them.
func budgetExceededCause(budget BudgetFacts) string {
	for _, violation := range budget.Violations {
		if violation.Code == "whole_index_budget_exceeded" {
			return fmt.Sprintf("whole_index_tokens=%d;max_tokens=%d", violation.Actual, violation.Maximum)
		}
	}
	return fmt.Sprintf("entry_field_budget_exceeded=%d;whole_index_tokens=%d;max_tokens=%d",
		len(budget.Violations), budget.WholeIndexTokens, budget.MaxTokens)
}

// budgetExceededRepairAction hands back both levers in the order a repository
// should consider them. Raising a budget is a governed change, so the sequence
// includes the approval it actually requires rather than only the edit command.
func budgetExceededRepairAction(budget BudgetFacts) string {
	for _, violation := range budget.Violations {
		if violation.Code == "whole_index_budget_exceeded" {
			// The activation sequence is spelled out because following it from
			// memory does not work: scope preview only emits the artifact the
			// rest of the flow consumes when it is given a candidate set, and a
			// configuration-only change still needs one, empty.
			return "reduce_index_membership: aoci scope explain <path> then aoci scope rule; " +
				"or raise the project budget: aoci scope budget set --max-tokens <n>; " +
				"then activate it: write {\"version\":\"managed-scope-candidate-set/v1\",\"entries\":[],\"dispositions\":[]} " +
				"to candidates.json, aoci scope preview --candidate-file candidates.json --json > preview.json, " +
				"aoci scope approve --preview-file preview.json --actor <id> --out-file approval.json, " +
				"aoci scope apply --preview-file preview.json --approval-file approval.json"
		}
	}
	return "re-author the named Entries within their C band, " +
		"or raise the band: aoci scope budget set --policy-file <policy.json>, " +
		"then activate it through the same governed Scope Change sequence"
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

// baselineMatches judges a formal Volume against its Baseline record through
// baseline.EquivalentFingerprints, which its own doc comment declares the single
// entry point for fingerprint equivalence.
//
// This used to compare raw SHA-256 directly, which made it the one consumer in
// the repository that bypassed that entry point — and therefore the one place
// that ignored line_ending_tolerance, a setting that defaults to true. A Windows
// checkout under the default core.autocrlf rewrites every line ending, so the
// volume that governs the whole repository was hard-blocked by a difference the
// team policy already calls equivalent, while ordinary business sources under
// the identical rewrite stayed authorable.
func baselineMatches(
	root string,
	cfg *config.Config,
	state *baseline.Baseline,
	rel string,
) (
	matched bool,
	lineEndingOnly bool,
) {
	if state == nil {
		return false, false
	}
	stored, ok := state.Files[rel]
	if !ok {
		return false, false
	}
	current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false, false
	}
	return baseline.EquivalentFingerprints(stored, current, cfg.LineEndingTolerance)
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
		// Informational findings must be named explicitly. The default arm below
		// means blocked, so a new code that nobody classifies silently becomes a
		// hard stop — which is exactly how code_volume_unbaselined wedged
		// repositories over a difference the tolerance policy calls equivalent.
		case finding.Code == "code_volume_line_ending_only" || finding.Code == "database_volume_line_ending_only" ||
			finding.Code == "root_volume_line_ending_only" || finding.Code == "meta_volume_line_ending_only" ||
			finding.Code == "root_volume_baseline_drift" || finding.Code == "meta_volume_baseline_drift":
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
