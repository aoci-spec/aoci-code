// MCP原子Apply后的轻量治理终态复查。
package mcptools

import (
	"fmt"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

func inspectAutoAlignment(
	root,
	mcpServiceVersion string,
	outcomes ...*AtomicBatchOutcome,
) (
	aligned bool,
	remaining int,
	findings []string,
	receipt cognitionReceipt,
) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return false, 1, []string{"[" + fail.Code + "] " + fail.Msg},
			newCognitionReceipt(root, mcpServiceVersion, "", cognitionScopeRepositoryFull)
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		var outcome *AtomicBatchOutcome
		if len(outcomes) > 0 {
			outcome = outcomes[0]
		}
		return inspectCognitionVolumeAlignment(root, mcpServiceVersion, loaded, outcome)
	}
	repository := loaded.legacyRepo()
	receipt = newCognitionReceipt(
		root, mcpServiceVersion, repository.text, cognitionScopeRepositoryFull,
	)
	state, err := managedstate.Load(root, repository.cfg)
	if err != nil {
		return false, 1, []string{"snapshot: " + err.Error()}, receipt
	}
	if state.ScopeChangeRequired {
		return false, 1, []string{"scope_change_required"}, receipt
	}
	warnings := state.Warnings
	detected, err := managedstate.Detect(root, repository.cfg, repository.doc, state)
	if err != nil {
		return false, 1, []string{"snapshot: " + err.Error()}, receipt
	}
	classification, _, _, err := curation.BuildClassification(
		root, repository.cfg, detected.Missing,
	)
	if err != nil {
		return false, 1, []string{"curation: " + err.Error()}, receipt
	}

	findings = []string{}
	for _, rel := range detected.Stale {
		findings = append(findings, "stale: "+rel)
	}
	for _, rel := range detected.Orphan {
		findings = append(findings, "orphan: "+rel)
	}
	for _, rel := range detected.Unbaselined {
		findings = append(findings, "unbaselined: "+rel)
	}
	for _, rel := range classification.Actionable {
		findings = append(findings, "missing: "+rel)
	}
	for _, pending := range classification.Pending {
		findings = append(findings, "pending_curation: "+pending.Path)
	}
	for _, warning := range warnings {
		findings = append(findings, "warning: "+warning)
	}
	if !state.Legacy {
		if repository.cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
			for _, rel := range detected.ObservedNew {
				findings = append(findings, "observed_new: "+rel)
			}
			for _, rel := range detected.ObservedChanged {
				findings = append(findings, "observed_changed: "+rel)
			}
			for _, rel := range detected.ObservedRemoved {
				findings = append(findings, "observed_removed: "+rel)
			}
		}
		budget, budgetErr := cognitionbudget.Build(root, []byte(repository.text), repository.cfg.EffectiveCognitionBudget())
		if budgetErr != nil {
			findings = append(findings, "cognition_budget_invalid")
		} else {
			for _, violation := range budget.Violations {
				findings = append(findings, violation.Code+": "+violation.Path)
			}
		}
	}
	remaining = len(findings)
	if remaining == 0 {
		return true, 0, findings, receipt
	}
	if len(findings) == 0 {
		findings = append(findings, fmt.Sprintf("remaining=%d", remaining))
	}
	return false, remaining, findings, receipt
}

func inspectCognitionVolumeAlignment(
	root,
	mcpServiceVersion string,
	loaded *cognitionRepoCtx,
	outcome *AtomicBatchOutcome,
) (bool, int, []string, cognitionReceipt) {
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		return false, 1, []string{"scope: " + err.Error()},
			newCognitionReceipt(root, mcpServiceVersion, "", cognitionScopeRepositoryFull)
	}
	receipt := newVolumeCognitionReceipt(root, mcpServiceVersion, loaded.set, view)
	volumeIDs := []string{cognition.ScopeCode}
	if outcome != nil {
		if len(outcome.Volumes) == 0 || len(outcome.Volumes) > 2 || len(outcome.Items) == 0 {
			return false, 1, []string{"cognition_volume_alignment_unavailable"}, receipt
		}
		volumeIDs = outcome.Volumes
	}
	state, exists, loadErr := baseline.Load(root)
	if loadErr != nil || !exists || state == nil {
		return false, 1, []string{"baseline_unavailable"}, receipt
	}
	findings := []string{}
	if code := loaded.set.Volumes["code"]; code != nil && code.Document != nil {
		managed, managedErr := managedstate.Load(root, loaded.cfg)
		if managedErr != nil {
			return false, 1, []string{"managed_scope: " + managedErr.Error()}, receipt
		}
		if managed.ScopeChangeRequired {
			return false, 1, []string{"scope_change_required"}, receipt
		}
		// Match the Managed Scope-aware Code drift projection shared by ordinary
		// Volumes governance. Formal cognition assets are guards/outputs, not
		// Code source objects, while Observe and Exclude retain their policy roles.
		projected := *managed
		projected.Snapshot = make(map[string]baseline.Fingerprint, len(managed.Snapshot))
		for rel, fingerprint := range managed.Snapshot {
			projected.Snapshot[rel] = fingerprint
		}
		delete(projected.Snapshot, loaded.set.Root.Descriptor.Path)
		for _, asset := range loaded.set.Volumes {
			if asset != nil {
				delete(projected.Snapshot, asset.Descriptor.Path)
			}
		}
		detected, detectErr := managedstate.Detect(root, loaded.cfg, code.Document, &projected)
		if detectErr != nil {
			return false, 1, []string{"snapshot: " + detectErr.Error()}, receipt
		}
		classification, _, _, classifyErr := curation.BuildClassification(root, loaded.cfg, detected.Missing)
		if classifyErr != nil {
			return false, 1, []string{"curation: " + classifyErr.Error()}, receipt
		}
		for _, rel := range detected.Stale {
			findings = append(findings, "stale: "+rel)
		}
		for _, rel := range detected.Orphan {
			findings = append(findings, "orphan: "+rel)
		}
		for _, rel := range detected.Unbaselined {
			findings = append(findings, "unbaselined: "+rel)
		}
		for _, rel := range classification.Actionable {
			findings = append(findings, "missing: "+rel)
		}
		for _, pending := range classification.Pending {
			findings = append(findings, "pending_curation: "+pending.Path)
		}
		if loaded.cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
			for _, rel := range detected.ObservedNew {
				findings = append(findings, "observed_new: "+rel)
			}
			for _, rel := range detected.ObservedChanged {
				findings = append(findings, "observed_changed: "+rel)
			}
			for _, rel := range detected.ObservedRemoved {
				findings = append(findings, "observed_removed: "+rel)
			}
		}
		for _, warning := range managed.Warnings {
			findings = append(findings, "warning: "+warning)
		}
	}
	for _, volumeID := range volumeIDs {
		asset := loaded.set.Volumes[volumeID]
		if asset == nil {
			findings = append(findings, "unaligned: "+volumeID)
			continue
		}
		volumePath := asset.Descriptor.Path
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(volumePath)))
		stored, ok := state.Files[volumePath]
		if hashErr != nil || !ok || stored.SHA256 != current.SHA256 {
			findings = append(findings, "unaligned: "+volumePath)
		}
	}
	if containsString(volumeIDs, "database") {
		database := dbcognition.Assess(root, loaded.cfg.DatabaseSources, loaded.set, state)
		for _, source := range database.Sources {
			if source.State != machinecontract.DatabaseCognitionCurrent {
				findings = append(findings, source.State+": source:"+source.SourceID)
			}
		}
		for _, item := range database.Items {
			if item.State != machinecontract.DatabaseCognitionCurrent {
				findings = append(findings, item.State+": "+item.ObjectRef)
			}
		}
		if !database.CognitionCurrent && len(findings) == 0 {
			findings = append(findings, "database_cognition_unaligned")
		}
	}
	if len(findings) > 0 {
		return false, len(findings), findings, receipt
	}
	return true, 0, findings, receipt
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func currentWriteCognitionReceipt(root, mcpServiceVersion string) cognitionReceipt {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return newCognitionReceipt(root, mcpServiceVersion, "", cognitionScopeRepositoryFull)
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		view, err := loaded.set.Scope(cognition.ScopeAll)
		if err == nil {
			return newVolumeCognitionReceipt(root, mcpServiceVersion, loaded.set, view)
		}
	}
	return newCognitionReceipt(
		root,
		mcpServiceVersion,
		string(loaded.set.Root.Raw),
		cognitionScopeRepositoryFull,
	)
}
