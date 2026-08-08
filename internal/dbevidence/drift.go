package dbevidence

import (
	"slices"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func CompareSnapshot(snapshot Snapshot, baseline Baseline, baselinePresent bool) DriftReport {
	report := DriftReport{
		Version: 1, SourceID: snapshot.SourceID, Engine: snapshot.Engine,
		SourceStatus:         "available",
		SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256,
		BaselinePresent:      false, Items: []DriftItem{}, BusinessDataRead: false,
	}
	var accepted *BaselineSource
	for index := range baseline.Sources {
		if baseline.Sources[index].SourceID == snapshot.SourceID {
			accepted = &baseline.Sources[index]
			break
		}
	}
	if baselinePresent && accepted != nil {
		report.BaselinePresent = true
		report.SourceIdentityChanged = accepted.Engine != snapshot.Engine ||
			accepted.Database != snapshot.Database || accepted.EvidenceVersion != snapshot.EvidenceVersion ||
			!slices.Equal(accepted.Namespaces, snapshot.Namespaces) ||
			!slices.Equal(accepted.IncludeNamespaces, snapshot.IncludeNamespaces) ||
			!slices.Equal(accepted.ExcludeNamespaces, snapshot.ExcludeNamespaces) ||
			!slices.Equal(accepted.IncludeTables, snapshot.IncludeTables) ||
			!slices.Equal(accepted.ExcludeTables, snapshot.ExcludeTables) ||
			!equalCaseSemantics(accepted.CaseSemantics, snapshot.CaseSemantics)
	}
	currentByRef := make(map[string]SnapshotTable, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		currentByRef[table.ObjectRef] = table
	}
	baselineByRef := map[string]BaselineTable{}
	if accepted != nil {
		for _, table := range accepted.Tables {
			baselineByRef[table.ObjectRef] = table
		}
	}
	for _, current := range snapshot.Tables {
		item := DriftItem{ObjectRef: current.ObjectRef, CurrentSHA256: current.TableEvidenceSHA256, CurrentEvidenceRef: current.EvidenceRef}
		previous, exists := baselineByRef[current.ObjectRef]
		switch {
		case !exists:
			item.Status = DriftNew
			report.Summary.New++
		case previous.TableEvidenceSHA256 == current.TableEvidenceSHA256:
			item.Status = DriftUnchanged
			item.BaselineSHA256 = previous.TableEvidenceSHA256
			report.Summary.Unchanged++
		default:
			item.Status = DriftChanged
			item.BaselineSHA256 = previous.TableEvidenceSHA256
			item.ChangedComponents = changedComponents(previous.ComponentHashes, current.ComponentHashes)
			report.Summary.Changed++
		}
		report.Items = append(report.Items, item)
	}
	for objectRef, previous := range baselineByRef {
		if _, exists := currentByRef[objectRef]; exists {
			continue
		}
		report.Items = append(report.Items, DriftItem{ObjectRef: objectRef, Status: DriftRemoved, BaselineSHA256: previous.TableEvidenceSHA256})
		report.Summary.Removed++
	}
	sort.Slice(report.Items, func(i, j int) bool { return report.Items[i].ObjectRef < report.Items[j].ObjectRef })
	return report
}

func equalCaseSemantics(left, right CaseSemantics) bool {
	if left.IdentifierCase != right.IdentifierCase || (left.LowerCaseTableNames == nil) != (right.LowerCaseTableNames == nil) {
		return false
	}
	return left.LowerCaseTableNames == nil || *left.LowerCaseTableNames == *right.LowerCaseTableNames
}

func changedComponents(previous, current ComponentHashes) []string {
	result := []string{}
	for _, candidate := range []struct {
		name     string
		previous string
		current  string
	}{
		{"columns", previous.Columns, current.Columns},
		{"primary_key", previous.PrimaryKey, current.PrimaryKey},
		{"unique_constraints", previous.UniqueConstraints, current.UniqueConstraints},
		{"foreign_keys", previous.ForeignKeys, current.ForeignKeys},
		{"checks", previous.Checks, current.Checks},
		{"indexes", previous.Indexes, current.Indexes},
		{"partition", previous.Partition, current.Partition},
	} {
		if candidate.previous != candidate.current {
			result = append(result, candidate.name)
		}
	}
	return result
}

func BuildEvidenceBundle(table TableEvidence, digest string, migrationRefs, codeRefs []string, existingEntry *string) EvidenceBundle {
	return EvidenceBundle{
		Version:                   BundleVersion,
		ReadyFor:                  "model_authored_table_fras",
		ObjectRef:                 table.ObjectRef,
		EvidenceVersion:           EvidenceVersion,
		TableEvidenceSHA256:       digest,
		TableEvidence:             table,
		MigrationEvidenceRefs:     sortedCopy(migrationRefs),
		CodeEvidenceRefs:          sortedCopy(codeRefs),
		ExistingDatabaseEntry:     existingEntry,
		BusinessDataRead:          false,
		SemanticCandidateIncluded: false,
		NextAction:                machinecontract.DatabaseEvidenceActionAuthorCompleteTableFRAS,
	}
}
