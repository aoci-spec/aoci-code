// The lightweight governance terminal-state recheck after an MCP atomic Apply.
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
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func inspectAutoAlignment(
	root,
	mcpServiceVersion string,
	outcome *AtomicBatchOutcome,
) (
	aligned bool,
	remaining int,
	findings []string,
	receipt cognitionReceipt,
	finishSiblings bool,
) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return false, 1, []string{"[" + fail.Code + "] " + fail.Msg},
			newCognitionReceipt(root, mcpServiceVersion, "", cognitionScopeRepositoryFull), false
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return inspectCognitionVolumeAlignment(root, mcpServiceVersion, loaded, outcome)
	}
	repository := loaded.legacyRepo()
	receipt = newCognitionReceipt(
		root, mcpServiceVersion, repository.text, cognitionScopeRepositoryFull,
	)
	state, err := managedstate.Load(root, repository.cfg)
	if err != nil {
		return false, 1, []string{"snapshot: " + err.Error()}, receipt, false
	}
	if state.ScopeChangeRequired {
		return false, 1, []string{"scope_change_required"}, receipt, false
	}
	warnings := state.Warnings
	detected, err := managedstate.Detect(root, repository.cfg, repository.doc, state)
	if err != nil {
		return false, 1, []string{"snapshot: " + err.Error()}, receipt, false
	}
	classification, _, _, err := curation.BuildClassification(
		root, repository.cfg, detected.Missing,
	)
	if err != nil {
		return false, 1, []string{"curation: " + err.Error()}, receipt, false
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
		return true, 0, findings, receipt, false
	}
	if len(findings) == 0 {
		findings = append(findings, fmt.Sprintf("remaining=%d", remaining))
	}
	return false, remaining, findings, receipt, false
}

func inspectCognitionVolumeAlignment(
	root,
	mcpServiceVersion string,
	loaded *cognitionRepoCtx,
	outcome *AtomicBatchOutcome,
) (bool, int, []string, cognitionReceipt, bool) {
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		return false, 1, []string{"scope: " + err.Error()},
			newCognitionReceipt(root, mcpServiceVersion, "", cognitionScopeRepositoryFull), false
	}
	receipt := newVolumeCognitionReceipt(root, mcpServiceVersion, loaded.set, view)
	if outcome == nil || len(outcome.Volumes) == 0 || len(outcome.Volumes) > 2 || len(outcome.Items) == 0 {
		return false, 1, []string{"cognition_volume_alignment_unavailable"}, receipt, false
	}
	state, exists, loadErr := baseline.Load(root)
	if loadErr != nil || !exists || state == nil {
		return false, 1, []string{"baseline_unavailable"}, receipt, false
	}
	findings := []string{}
	parallelRemaining, outstanding := 0, 0
	parallelWave, finishSiblings := false, false
	if code := loaded.set.Volumes["code"]; code != nil && code.Document != nil {
		managed, managedErr := managedstate.Load(root, loaded.cfg)
		if managedErr != nil {
			return false, 1, []string{"managed_scope: " + managedErr.Error()}, receipt, false
		}
		if managed.ScopeChangeRequired {
			return false, 1, []string{"scope_change_required"}, receipt, false
		}
		// The one Code drift projection every reader shares: formal cognition
		// assets leave the snapshot and the Baseline, so a Root or Meta record
		// a rule once observed is not observed_removed here while every other
		// reader calls the repository aligned (#158), and held sources are the
		// informational facts Verify reports, never debt, or an optimization
		// batch beside one image could never finalize (#90).
		drift, detected, driftErr := volumegovernance.CodeDrift(root, loaded.cfg, code.Document, managed)
		if driftErr != nil {
			return false, 1, []string{"snapshot: " + driftErr.Error()}, receipt, false
		}
		if loaded.cfg.CodeCognitionParallelBatchLimit() > 1 {
			siblings := currentParallelSiblings(root, parallelIdentities(loaded.set, managed.DesiredPolicyIdentity))
			if len(siblings) > 0 {
				parallelWave = true
				work := volumegovernance.CodeAuthoringWorkFor(loaded.set, drift)
				parallelRemaining = unissuedCodeTargetCount(work.Targets, siblings) + len(drift.Orphan)
				outstanding = outstandingSiblingCount(siblings)
				finishSiblings = outstanding > 0 && !siblingsNeverFetched(siblings)
			}
		}
		for _, rel := range drift.Stale {
			findings = append(findings, "stale: "+rel)
		}
		for _, rel := range drift.Orphan {
			findings = append(findings, "orphan: "+rel)
		}
		for _, rel := range drift.Unbaselined {
			findings = append(findings, "unbaselined: "+rel)
		}
		for _, rel := range drift.Missing {
			findings = append(findings, "missing: "+rel)
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
	for _, volumeID := range outcome.Volumes {
		asset := loaded.set.Volumes[volumeID]
		if asset == nil {
			findings = append(findings, "unaligned: "+volumeID)
			continue
		}
		volumePath := asset.Descriptor.Path
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(volumePath)))
		stored, ok := state.Files[volumePath]
		// The same equivalence governance uses: a Volume that differs from its
		// Baseline record only by line endings is aligned under the team's
		// tolerance (audit G2-04).
		if hashErr != nil || !ok {
			findings = append(findings, "unaligned: "+volumePath)
		} else if equal, _ := baseline.EquivalentFingerprints(stored, current, loaded.cfg.LineEndingTolerance); !equal {
			findings = append(findings, "unaligned: "+volumePath)
		}
	}
	if containsString(outcome.Volumes, cognition.ScopeDatabase) ||
		(parallelWave && loaded.set.Volumes[cognition.ScopeDatabase] != nil) {
		database := dbcognition.Assess(root, loaded.cfg.DatabaseSources, loaded.set, state)
		parallelRemaining += databaseAuthoringTargetCount(database.Summary)
		for _, source := range database.Sources {
			// A declared-only source blocks nothing, as Maintain answers
			// (audit G2-03).
			if source.State != machinecontract.DatabaseCognitionCurrent && source.State != machinecontract.DatabaseCognitionEngineNotImplemented {
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
	remaining := len(findings)
	if parallelWave {
		remaining = parallelRemaining
	}
	return len(findings) == 0 && remaining == 0 && outstanding == 0, remaining, findings, receipt, finishSiblings
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
