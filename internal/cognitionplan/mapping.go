package cognitionplan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func buildLegacyMapping(root string, raw []byte, indexPath string, targetKinds []string) (*SemanticMapping, []cognition.Finding, error) {
	if !utf8.Valid(raw) {
		return nil, nil, fmt.Errorf("legacy_utf8_invalid")
	}
	document, warnings := legacyDocument(root, raw)
	findings := make([]cognition.Finding, 0, len(warnings))
	for _, warning := range warnings {
		findings = append(findings, cognition.Finding{Code: "legacy_parse_warning", AssetID: "legacy", Line: warning.LineNo, Message: warning.Msg})
	}
	mapping := &SemanticMapping{Version: machinecontract.CognitionSemanticMappingV1, LegacySHA256: hashBytes(raw), LegacyPreimage: string(raw), Records: []MappingRecord{}}
	semanticTotal := 0
	semanticMapped := 0
	entryTotal := 0
	entryMapped := 0
	entryDispositions := 0
	selfEntries := 0
	ambiguous := 0
	duplicate := 0
	seenTargets := map[string]bool{}

	for lineIndex, line := range document.HeaderLines {
		record, semantic := mapHeaderLine(line, lineIndex+1)
		if semantic {
			record.DispositionVersion = machinecontract.CognitionLegacyEntryDispositionV1
			record.Disposition = "header_authoring"
			record.AllowedTargets = []string{"meta", "root"}
		}
		mapping.Records = append(mapping.Records, record)
		if semantic {
			semanticTotal++
			semanticMapped++
		}
	}
	for _, section := range document.Sections {
		sectionRecord := mappingRecord("section", section.StartLine, section.HeaderLine, machinecontract.CognitionMappingStructuralOnly, "", "", "legacy_structure")
		sectionRecord.DispositionVersion, sectionRecord.Disposition, sectionRecord.AllowedTargets = machinecontract.CognitionLegacyEntryDispositionV1, "structural", []string{}
		mapping.Records = append(mapping.Records, sectionRecord)
		entryLines := map[int]bool{}
		for _, entry := range section.Entries {
			entryLines[entry.LineNo] = true
			entryTotal++
			reason := "entry_semantics_require_model_mapping"
			if entry.RelPath == "" {
				reason = "entry_identity_ambiguous"
				ambiguous++
			} else if seenTargets[strings.ToLower(entry.RelPath)] {
				duplicate++
				reason = "entry_source_identity_duplicate"
			} else {
				seenTargets[strings.ToLower(entry.RelPath)] = true
			}
			if !legacyEntryStructurallyComplete(entry) {
				reason = "entry_fras_or_tag_invalid"
			}
			record := mappingRecord("entry", entry.LineNo, entry.FullLine, machinecontract.CognitionMappingModelRegenerationRequired, "", "", reason)
			record.DispositionVersion = machinecontract.CognitionLegacyEntryDispositionV1
			record.Disposition = "entry_authoring"
			record.AllowedTargets = append([]string{}, targetKinds...)
			if filepath.ToSlash(entry.RelPath) == filepath.ToSlash(indexPath) {
				record.LegacySelfEntry = true
				record.Disposition = "legacy_self_entry_root_authoring"
				record.ReasonCode = "legacy_self_entry_requires_root_ownership"
				record.AllowedTargets = []string{cognition.OwnerRoot}
				selfEntries++
			}
			mapping.Records = append(mapping.Records, record)
			entryMapped++
			entryDispositions++
		}
		for offset, line := range section.RawLines {
			lineNumber := section.StartLine + offset
			if offset == 0 || entryLines[lineNumber] {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "===") {
				record := mappingRecord("unparsed_semantic_atom", lineNumber, line, machinecontract.CognitionMappingModelRegenerationRequired, "", "", "legacy_unparsed_semantics_require_model_mapping")
				record.DispositionVersion, record.Disposition, record.AllowedTargets = machinecontract.CognitionLegacyEntryDispositionV1, "semantic_authoring", []string{"meta", "root"}
				mapping.Records = append(mapping.Records, record)
				semanticTotal++
				semanticMapped++
				ambiguous++
				continue
			}
			record := mappingRecord("structure", lineNumber, line, machinecontract.CognitionMappingStructuralOnly, "", "", "legacy_structure")
			record.DispositionVersion, record.Disposition, record.AllowedTargets = machinecontract.CognitionLegacyEntryDispositionV1, "structural", []string{}
			mapping.Records = append(mapping.Records, record)
		}
	}
	sort.SliceStable(mapping.Records, func(i, j int) bool {
		if mapping.Records[i].SourceLine != mapping.Records[j].SourceLine {
			return mapping.Records[i].SourceLine < mapping.Records[j].SourceLine
		}
		return mapping.Records[i].UnitID < mapping.Records[j].UnitID
	})
	mapping.Coverage = MappingCoverage{
		ByteReversible:   true,
		LegacyEntryTotal: entryTotal, LegacyEntryMapped: entryMapped, LegacyEntryCoverage: percentage(entryMapped, entryTotal),
		LegacyEntryDispositionTotal: entryTotal, LegacyEntryDispositionComplete: entryDispositions, LegacySelfEntryTotal: selfEntries,
		LegacySemanticAtomTotal: semanticTotal, LegacySemanticAtomMapped: semanticMapped, LegacySemanticAtomCoverage: percentage(semanticMapped, semanticTotal),
		DuplicateTargetCount: duplicate, UnexplainedDropCount: 0, AmbiguousMappingCount: ambiguous,
		ProjectedCognitionValid: false, SemanticReviewStatus: machinecontract.CognitionSemanticEquivalenceUnverified,
	}
	data, err := mappingBytesForIdentity(mapping)
	if err != nil {
		return nil, nil, fmt.Errorf("semantic_mapping_encode_failed")
	}
	mapping.MappingSHA256 = hashBytes(data)
	return mapping, findings, nil
}

func mapHeaderLine(line string, lineNumber int) (MappingRecord, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "===") {
		record := mappingRecord("header_structural", lineNumber, line, machinecontract.CognitionMappingStructuralOnly, "", "", "legacy_structure")
		record.DispositionVersion, record.Disposition, record.AllowedTargets = machinecontract.CognitionLegacyEntryDispositionV1, "structural", []string{}
		return record, false
	}
	return mappingRecord("header_semantic_atom", lineNumber, line, machinecontract.CognitionMappingModelRegenerationRequired, "", "", "header_semantics_require_model_mapping"), true
}

func mappingRecord(unitKind string, sourceLine int, sourceText, mode, targetAsset, targetRef, reason string) MappingRecord {
	unitIdentity := newIdentity("legacy-information-unit")
	unitIdentity.field("unit_kind", unitKind)
	unitIdentity.field("source_line", fmt.Sprintf("%d", sourceLine))
	unitIdentity.field("source_bytes", sourceText)
	return MappingRecord{UnitID: unitIdentity.sum(), UnitKind: unitKind, SourceLine: sourceLine, SourceSHA256: hashBytes([]byte(sourceText)), SourceText: sourceText, Mode: mode, TargetAsset: targetAsset, TargetRef: targetRef, ReasonCode: reason}
}

func legacyEntryStructurallyComplete(entry *index.Entry) bool {
	if entry == nil || len(entry.TagsParsed) == 0 {
		return false
	}
	for _, value := range []string{entry.F, entry.R, entry.Api, entry.S} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func percentage(mapped, total int) string {
	if total == 0 {
		return "100.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(mapped)*100/float64(total))
}

func mappingBytesForIdentity(mapping *SemanticMapping) ([]byte, error) {
	copyValue := *mapping
	copyValue.MappingSHA256 = ""
	return canonicalJSON(copyValue)
}
