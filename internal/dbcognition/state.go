package dbcognition

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func Assess(root string, sources []dbevidence.SourceConfig, set *cognition.Set, state *baseline.Baseline) Assessment {
	result := Assessment{Version: machinecontract.DatabaseCognitionAssessmentVersion, ConfiguredSources: len(sources), NetworkAccessed: false, Sources: []SourceState{}, Items: []Item{}}
	if set == nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.Volumes["database"] == nil {
		result.DatabaseVolumeState = cognition.AssetAbsent
		assessAbsentVolume(root, sources, &result)
		return result
	}
	asset := set.Volumes["database"]
	result.DatabaseVolumeState = asset.State
	result.DatabaseVolumePath = asset.Descriptor.Path
	result.DatabaseVolumeSHA256 = asset.SHA256
	result.CognitionEntryCount = len(asset.Objects)

	orderedSources := append([]dbevidence.SourceConfig{}, sources...)
	sort.Slice(orderedSources, func(i, j int) bool { return orderedSources[i].SourceID < orderedSources[j].SourceID })
	sourceStatus := map[string]string{}
	blockingSources := 0
	evidenceBlockingSource := false
	evidence := map[string]Item{}
	acceptance := loadSnapshotAcceptance(root)
	for _, source := range orderedSources {
		// A declaration AOCI cannot collect from owes nothing whether it is
		// enabled or not; an Entry under it still blocks below.
		if !dbevidence.EngineCollectable(source.Engine) {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionEngineNotImplemented
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionEngineNotImplemented})
			continue
		}
		if !source.Enabled {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionSourceDisabled
			blockingSources++
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionSourceDisabled})
			continue
		}
		if !dbevidence.EngineCollectable(source.Engine) {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionEngineNotImplemented
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionEngineNotImplemented})
			continue
		}
		manifest, snapshot, exists, err := dbevidence.LoadSnapshot(root, source.SourceID)
		if err != nil {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionEvidenceInvalid
			blockingSources++
			evidenceBlockingSource = true
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionEvidenceInvalid, ErrorCode: "database_evidence_invalid"})
			continue
		}
		if !exists {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionEvidenceUnavailable
			blockingSources++
			evidenceBlockingSource = true
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionEvidenceUnavailable, ErrorCode: "database_snapshot_missing"})
			continue
		}
		if !dbevidence.SourceConfigMatchesManifest(source, manifest) {
			sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionEvidenceUnavailable
			blockingSources++
			evidenceBlockingSource = true
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionEvidenceUnavailable, ErrorCode: "database_snapshot_selection_mismatch"})
			continue
		}
		if state, code := acceptance.refusal(source.SourceID, snapshot.SourceSnapshotSHA256); code != "" {
			sourceStatus[source.SourceID] = state
			blockingSources++
			evidenceBlockingSource = true
			result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: state, ErrorCode: code})
			continue
		}
		sourceStatus[source.SourceID] = machinecontract.DatabaseCognitionCurrent
		result.Sources = append(result.Sources, SourceState{SourceID: source.SourceID, State: machinecontract.DatabaseCognitionCurrent, EvidenceVersion: snapshot.EvidenceVersion, TableCount: len(snapshot.Tables)})
		result.EvidenceTableCount += len(snapshot.Tables)
		for index := range snapshot.Tables {
			record := snapshot.Tables[index]
			copyRecord := record
			evidence[record.ObjectRef] = Item{ObjectRef: record.ObjectRef, SourceID: source.SourceID, TableEvidenceSHA256: record.TableEvidenceSHA256, EvidenceVersion: snapshot.EvidenceVersion, EvidenceRef: record.EvidenceRef, record: &copyRecord}
		}
	}

	objects := map[string]cognition.Object{}
	for _, object := range asset.Objects {
		objects[object.CanonicalRef] = object
	}
	bindings := map[string]baseline.DatabaseCognitionBinding{}
	if state != nil && state.DatabaseCognition != nil {
		for _, binding := range state.DatabaseCognition.Entries {
			bindings[binding.ObjectRef] = binding
		}
	}
	refs := make([]string, 0, len(evidence)+len(objects))
	seen := map[string]bool{}
	for ref := range evidence {
		refs = append(refs, ref)
		seen[ref] = true
	}
	if len(sources) > 0 {
		for ref := range objects {
			if !seen[ref] {
				refs = append(refs, ref)
			}
		}
	}
	sort.Strings(refs)
	for _, ref := range refs {
		item, hasEvidence := evidence[ref]
		object, hasEntry := objects[ref]
		sourceID := item.SourceID
		if sourceID == "" {
			sourceID = sourceIDFromObjectRef(ref)
			item.ObjectRef = ref
			item.SourceID = sourceID
		}
		if hasEntry {
			item.CurrentEntry = object.CanonicalLine
		}
		if !hasEvidence {
			switch sourceStatus[sourceID] {
			case machinecontract.DatabaseCognitionCurrent:
				item.State = machinecontract.DatabaseCognitionOrphan
			case machinecontract.DatabaseCognitionEvidenceInvalid:
				item.State = machinecontract.DatabaseCognitionEvidenceInvalid
			case machinecontract.DatabaseCognitionSourceDisabled:
				item.State = machinecontract.DatabaseCognitionSourceDisabled
			default:
				item.State = machinecontract.DatabaseCognitionEvidenceUnavailable
			}
			addSummary(&result.Summary, item.State)
			result.Items = append(result.Items, item)
			continue
		}
		if !hasEntry {
			item.State = machinecontract.DatabaseCognitionMissing
			addSummary(&result.Summary, item.State)
			result.Items = append(result.Items, item)
			continue
		}
		binding, bound := bindings[ref]
		if bound {
			item.Binding = &binding
		}
		switch {
		case !bound || binding.EntrySHA256 != EntrySHA256(object.CanonicalLine):
			item.State = machinecontract.DatabaseCognitionUnbaselined
		case binding.SourceID != sourceID || binding.EvidenceVersion != item.EvidenceVersion || binding.TableEvidenceSHA256 != item.TableEvidenceSHA256:
			item.State = machinecontract.DatabaseCognitionStale
		default:
			item.State = machinecontract.DatabaseCognitionCurrent
		}
		addSummary(&result.Summary, item.State)
		result.Items = append(result.Items, item)
	}
	result.BlockingSourceCount = blockingSources
	result.CognitionCurrent = blockingSources == 0 && result.Summary.Missing == 0 && result.Summary.Stale == 0 && result.Summary.Unbaselined == 0 && result.Summary.Orphan == 0 && result.Summary.EvidenceUnavailable == 0 && result.Summary.EvidenceInvalid == 0 && result.Summary.SourceDisabled == 0
	if len(sources) == 0 {
		result.CognitionCurrent = true
		result.NextAction = machinecontract.DatabaseCognitionActionNoConfiguration
	} else if result.CognitionCurrent {
		result.NextAction = machinecontract.DatabaseCognitionActionNoActionRequired
	} else if evidenceBlockingSource || result.Summary.EvidenceUnavailable+result.Summary.EvidenceInvalid > 0 {
		result.NextAction = machinecontract.DatabaseCognitionActionSnapshotOrRepair
	} else if result.Summary.Missing+result.Summary.Stale+result.Summary.Unbaselined > 0 {
		result.NextAction = machinecontract.DatabaseCognitionActionMaintain
	} else {
		result.NextAction = machinecontract.DatabaseCognitionActionReviewFindings
	}
	return result
}

func assessAbsentVolume(root string, sources []dbevidence.SourceConfig, result *Assessment) {
	if len(sources) == 0 {
		result.CognitionCurrent = true
		result.NextAction = machinecontract.DatabaseCognitionActionNoConfiguration
		return
	}
	acceptedBaseline, acceptedExists, acceptedErr := dbevidence.LoadBaseline(root)
	accepted := map[string]string{}
	if acceptedErr == nil && acceptedExists {
		for _, source := range acceptedBaseline.Sources {
			accepted[source.SourceID] = source.SourceSnapshotSHA256
		}
	}
	ordered := append([]dbevidence.SourceConfig{}, sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].SourceID < ordered[j].SourceID })
	collectable := 0
	for _, source := range ordered {
		state := SourceState{SourceID: source.SourceID}
		switch {
		case !dbevidence.EngineCollectable(source.Engine):
			// Enabled or not, a source AOCI cannot collect from owes nothing.
			state.State = machinecontract.DatabaseCognitionEngineNotImplemented
		case !source.Enabled:
			state.State = machinecontract.DatabaseCognitionSourceDisabled
			state.ErrorCode = "database_source_disabled"
		case acceptedErr != nil:
			state.State = machinecontract.DatabaseCognitionEvidenceInvalid
			state.ErrorCode = "database_evidence_baseline_invalid"
		case !acceptedExists:
			state.State = machinecontract.DatabaseCognitionEvidenceUnavailable
			state.ErrorCode = "database_evidence_baseline_missing"
		default:
			manifest, snapshot, exists, err := dbevidence.LoadSnapshot(root, source.SourceID)
			switch {
			case err != nil:
				state.State = machinecontract.DatabaseCognitionEvidenceInvalid
				state.ErrorCode = "database_evidence_invalid"
			case !exists:
				state.State = machinecontract.DatabaseCognitionEvidenceUnavailable
				state.ErrorCode = "database_snapshot_missing"
			case !dbevidence.SourceConfigMatchesManifest(source, manifest):
				state.State = machinecontract.DatabaseCognitionEvidenceUnavailable
				state.ErrorCode = "database_snapshot_selection_mismatch"
			case accepted[source.SourceID] != snapshot.SourceSnapshotSHA256:
				state.State = machinecontract.DatabaseCognitionEvidenceUnavailable
				state.ErrorCode = "database_snapshot_not_accepted"
			default:
				state.State = machinecontract.DatabaseCognitionCurrent
				state.EvidenceVersion = snapshot.EvidenceVersion
				state.TableCount = len(snapshot.Tables)
				result.EvidenceTableCount += len(snapshot.Tables)
			}
		}
		// Without a Volume no Entry can belong to a disabled source, so it
		// governs nothing, as in volumegovernance and databasebootstrap;
		// counting it as blocking hid a due bootstrap (audit G6-03).
		if state.State == machinecontract.DatabaseCognitionEngineNotImplemented || state.State == machinecontract.DatabaseCognitionSourceDisabled {
			result.Sources = append(result.Sources, state)
			continue
		}
		collectable++
		if state.State != machinecontract.DatabaseCognitionCurrent {
			result.BlockingSourceCount++
		}
		result.Sources = append(result.Sources, state)
	}
	// Declarations alone leave nothing to bootstrap: there is no Evidence a
	// Database Volume could be built from, which is the no-configuration case.
	if collectable == 0 {
		result.CognitionCurrent = true
		result.NextAction = machinecontract.DatabaseCognitionActionNoConfiguration
		return
	}
	result.CognitionCurrent = false
	if result.BlockingSourceCount == 0 {
		result.NextAction = machinecontract.DatabaseCognitionActionBootstrapVolume
	} else {
		result.NextAction = machinecontract.DatabaseCognitionActionSnapshotOrRepair
	}
}

func EntrySHA256(line string) string {
	digest := sha256.Sum256([]byte(line))
	return hex.EncodeToString(digest[:])
}

func sourceIDFromObjectRef(ref string) string {
	parts := strings.Split(strings.TrimPrefix(ref, "database://"), "/")
	if len(parts) != 3 {
		return ""
	}
	return parts[0]
}

func addSummary(summary *Summary, state string) {
	switch state {
	case machinecontract.DatabaseCognitionCurrent:
		summary.Current++
	case machinecontract.DatabaseCognitionMissing:
		summary.Missing++
	case machinecontract.DatabaseCognitionStale:
		summary.Stale++
	case machinecontract.DatabaseCognitionUnbaselined:
		summary.Unbaselined++
	case machinecontract.DatabaseCognitionOrphan:
		summary.Orphan++
	case machinecontract.DatabaseCognitionEvidenceUnavailable:
		summary.EvidenceUnavailable++
	case machinecontract.DatabaseCognitionEvidenceInvalid:
		summary.EvidenceInvalid++
	case machinecontract.DatabaseCognitionSourceDisabled:
		summary.SourceDisabled++
	}
}

// snapshotAcceptance is the accepted Evidence Baseline every Database reader
// requires: assessment and submission read only a snapshot that
// database baseline accept recorded (aoci-database-cognition-authoring-v1).
// The present-Volume branch and ValidateSubmission used to skip it, so a
// captured but unaccepted snapshot was issued and written (audit G6-02).
type snapshotAcceptance struct {
	accepted map[string]string
	exists   bool
	err      error
}

func loadSnapshotAcceptance(root string) snapshotAcceptance {
	document, exists, err := dbevidence.LoadBaseline(root)
	result := snapshotAcceptance{accepted: map[string]string{}, exists: exists, err: err}
	if err == nil && exists {
		for _, source := range document.Sources {
			result.accepted[source.SourceID] = source.SourceSnapshotSHA256
		}
	}
	return result
}

// refusal names the state and error code when the snapshot is not the
// accepted one, or empty strings when it is.
func (a snapshotAcceptance) refusal(sourceID, snapshotSHA256 string) (string, string) {
	switch {
	case a.err != nil:
		return machinecontract.DatabaseCognitionEvidenceInvalid, "database_evidence_baseline_invalid"
	case !a.exists:
		return machinecontract.DatabaseCognitionEvidenceUnavailable, "database_evidence_baseline_missing"
	case a.accepted[sourceID] != snapshotSHA256:
		return machinecontract.DatabaseCognitionEvidenceUnavailable, "database_snapshot_not_accepted"
	}
	return "", ""
}

// AcceptedSnapshotRefusal reports, as an error code, why the saved snapshot
// of sourceID is not the one database baseline accept recorded, or "" when it
// is. A writer that binds Entries to Evidence checks it before any write, so
// the post-write verify, which reads only accepted Evidence, cannot fail late.
func AcceptedSnapshotRefusal(root, sourceID string) string {
	_, snapshot, exists, err := dbevidence.LoadSnapshot(root, sourceID)
	if err != nil {
		return "database_evidence_invalid"
	}
	if !exists {
		return "database_snapshot_missing"
	}
	_, code := loadSnapshotAcceptance(root).refusal(sourceID, snapshot.SourceSnapshotSHA256)
	return code
}
