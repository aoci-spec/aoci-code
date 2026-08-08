package migrationapply

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type rawLine struct {
	number     int
	start      int64
	contentEnd int64
	end        int64
	text       string
}

// CaptureSnapshot records exact reversible Legacy and Baseline bytes plus the
// deterministic lexical ranges used by the Apply-grade mapping. It reads only
// local repository and saved Evidence state and never writes formal assets.
func CaptureSnapshot(repositoryRoot, locale string, targetKinds []string, capturedAt string) (*LegacySnapshot, error) {
	if err := validateUTC(capturedAt); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("migration_repository_root_invalid")
	}
	if pending, err := cognitiontxn.Pending(root); err != nil {
		return nil, fmt.Errorf("migration_recovery_state_unavailable")
	} else if len(pending) != 0 {
		return nil, fmt.Errorf("migration_pending_recovery")
	}

	legacyPath := filepath.Join(root, "aoci.txt")
	legacyInfo, err := os.Lstat(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("migration_legacy_required")
	}
	if err != nil || legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("migration_legacy_not_regular")
	}
	legacyRaw, err := os.ReadFile(legacyPath)
	if err != nil {
		return nil, fmt.Errorf("migration_legacy_read_failed")
	}
	if !utf8.Valid(legacyRaw) {
		return nil, fmt.Errorf("migration_legacy_utf8_invalid")
	}
	layout, err := cognition.DetectLayout(legacyRaw)
	if err != nil {
		return nil, fmt.Errorf("migration_root_marker_invalid")
	}
	if layout != cognition.LayoutLegacyMonolithic {
		return nil, fmt.Errorf("migration_layout_already_volumes")
	}

	for _, relative := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		if _, statErr := os.Lstat(filepath.Join(root, relative)); statErr == nil {
			return nil, fmt.Errorf("migration_mixed_layout: %s", relative)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("migration_target_inspection_failed: %s", relative)
		}
	}

	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineInfo, err := os.Lstat(baselinePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("migration_baseline_required")
	}
	if err != nil || baselineInfo.Mode()&os.ModeSymlink != 0 || !baselineInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("migration_baseline_not_regular")
	}
	baselineRaw, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("migration_baseline_read_failed")
	}
	baselineValue, exists, err := baseline.Load(root)
	if err != nil || !exists || baselineValue == nil {
		return nil, fmt.Errorf("migration_baseline_invalid")
	}

	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale, TargetKinds: targetKinds})
	if err != nil {
		return nil, err
	}
	if plan.Layout != machinecontract.CognitionPlannerLegacy || plan.Status != machinecontract.CognitionPlannerAuthoringRequired || plan.Operation != cognitionplan.OperationMigration {
		return nil, fmt.Errorf("migration_not_eligible")
	}

	doc, parseWarnings := index.Parse(string(legacyRaw))
	index.ResolveRelPaths(doc, root)
	ranges := enumerateLegacyRanges(legacyRaw, doc)
	findings := snapshotFindings(root, legacyRaw, baselineValue, plan, doc, parseWarnings)
	entryCount := 0
	for _, section := range doc.Sections {
		entryCount += len(section.Entries)
	}
	headerState := "present"
	if len(doc.HeaderLines) == 0 {
		headerState = "missing"
		findings = append(findings, SnapshotFinding{Code: "legacy_header_missing"})
	}
	sortSnapshotFindings(findings)
	eligibility := machinecontract.CognitionMigrationEligibilityEligible
	if len(findings) != 0 {
		eligibility = machinecontract.CognitionMigrationEligibilityIneligible
	}

	preimages := []SnapshotPreimage{
		{Path: "aoci.txt", State: "legacy", SHA256: sha256Hex(legacyRaw), ByteSize: int64(len(legacyRaw)), FileMode: modeString(legacyInfo.Mode())},
		{Path: "aoci.meta.txt", State: "absent", SHA256: sha256Hex(nil), FileMode: "0000"},
		{Path: "aoci.code.txt", State: "absent", SHA256: sha256Hex(nil), FileMode: "0000"},
		{Path: "aoci.database.txt", State: "absent", SHA256: sha256Hex(nil), FileMode: "0000"},
		{Path: ".aoci/baseline.json", State: "legacy_baseline", SHA256: sha256Hex(baselineRaw), ByteSize: int64(len(baselineRaw)), FileMode: modeString(baselineInfo.Mode())},
	}
	snapshot := &LegacySnapshot{
		Version: machinecontract.CognitionLegacySnapshotV1, Eligibility: eligibility,
		LegacyPath: "aoci.txt", LegacySHA256: sha256Hex(legacyRaw), LegacyByteSize: int64(len(legacyRaw)),
		LegacyFileMode: modeString(legacyInfo.Mode()), LegacyEncoding: "base64", LegacyContentBase64: base64.StdEncoding.EncodeToString(legacyRaw),
		BOM: detectBOM(legacyRaw), LineEndings: detectLineEndings(legacyRaw),
		BaselinePath: ".aoci/baseline.json", BaselineSHA256: sha256Hex(baselineRaw), BaselineByteSize: int64(len(baselineRaw)),
		BaselineFileMode: modeString(baselineInfo.Mode()), BaselineEncoding: "base64", BaselineContentBase64: base64.StdEncoding.EncodeToString(baselineRaw),
		EntryCount: entryCount, HeaderState: headerState, ParseIdentity: parseIdentity(ranges, findings), Ranges: ranges, Findings: findings,
		RepositoryIdentity: plan.RepositoryIdentity, LayoutIdentity: plan.LayoutIdentity, BaselineIdentity: plan.BaselineIdentity,
		InventoryIdentity: plan.InventoryIdentity, SourceEvidenceIdentity: plan.SourceEvidenceIdentity, CurationIdentity: plan.CurationIdentity,
		RegistryIdentity: plan.RegistryIdentity, ValidatorIdentity: machinecontract.CognitionMigrationValidatorV2,
		FormalPreimages: preimages, CapturedAt: capturedAt, NetworkAccessed: false,
	}
	identity, err := snapshotIdentity(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.SnapshotIdentity = identity
	return snapshot, nil
}

func snapshotFindings(root string, legacyRaw []byte, baselineValue *baseline.Baseline, plan *cognitionplan.Plan, doc *index.Document, parseWarnings []index.Warning) []SnapshotFinding {
	findings := []SnapshotFinding{}
	for _, warning := range parseWarnings {
		findings = append(findings, SnapshotFinding{Code: "legacy_parse_warning", Line: warning.LineNo})
	}
	legacyFingerprint, exists := baselineValue.Files["aoci.txt"]
	if !exists {
		findings = append(findings, SnapshotFinding{Code: "legacy_index_unbaselined"})
	} else if legacyFingerprint.SHA256 != sha256Hex(legacyRaw) {
		findings = append(findings, SnapshotFinding{Code: "legacy_index_stale"})
	}
	entries := map[string]int{}
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			if explicitLegacyDatabaseRef(section.HeaderLine, entry.Filename) != "" {
				continue
			}
			entries[entry.RelPath]++
			if entry.RelPath == "" {
				findings = append(findings, SnapshotFinding{Code: "legacy_entry_identity_ambiguous", Line: entry.LineNo})
				continue
			}
			if entries[entry.RelPath] > 1 {
				findings = append(findings, SnapshotFinding{Code: "legacy_entry_duplicate", Line: entry.LineNo})
			}
			for _, violation := range index.ValidateEntryLine(entry.RelPath, entry.FullLine) {
				code := "legacy_entry_warning"
				if violation.Level == index.LevelError {
					code = "legacy_entry_invalid"
				}
				findings = append(findings, SnapshotFinding{Code: code, Line: entry.LineNo})
			}
			for _, violation := range index.ValidateEntryRelations(root, entry.RelPath, entry.FullLine) {
				if violation.Level == index.LevelWarning {
					findings = append(findings, SnapshotFinding{Code: "legacy_relation_warning", Line: entry.LineNo})
				}
			}
			if strings.HasSuffix(entry.RelPath, "/") {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(entry.RelPath))
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				findings = append(findings, SnapshotFinding{Code: "legacy_orphan", Line: entry.LineNo})
				continue
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				findings = append(findings, SnapshotFinding{Code: "legacy_source_wrong_type", Line: entry.LineNo})
				continue
			}
			current, hashErr := baseline.HashFile(path)
			accepted, baselined := baselineValue.Files[entry.RelPath]
			if hashErr != nil {
				findings = append(findings, SnapshotFinding{Code: "legacy_source_unreadable", Line: entry.LineNo})
			} else if !baselined {
				findings = append(findings, SnapshotFinding{Code: "legacy_source_unbaselined", Line: entry.LineNo})
			} else if equal, _ := baseline.EquivalentFingerprints(accepted, current, true); !equal {
				findings = append(findings, SnapshotFinding{Code: "legacy_source_stale", Line: entry.LineNo})
			}
		}
	}
	for _, object := range plan.Inventory {
		if object.Eligible && entries[object.Path] == 0 {
			findings = append(findings, SnapshotFinding{Code: "legacy_missing_entry", Identity: sha256Hex([]byte(object.Path))})
		}
	}
	return findings
}

func explicitLegacyDatabaseRef(sectionHeader, objectName string) string {
	trimmed := strings.TrimSpace(sectionHeader)
	marker := "/database://"
	position := strings.Index(trimmed, marker)
	if position < 0 || !strings.HasPrefix(trimmed, "===") || !strings.HasSuffix(trimmed, "/===") {
		return ""
	}
	namespace := strings.TrimSuffix(trimmed[position+1:], "/===")
	parts := strings.Split(strings.TrimPrefix(namespace, "database://"), "/")
	if len(parts) != 2 || strings.TrimSpace(objectName) == "" || strings.ContainsAny(objectName, "/\\") {
		return ""
	}
	return namespace + "/" + objectName
}

func enumerateLegacyRanges(raw []byte, doc *index.Document) []ByteRange {
	lines := splitRawLines(raw)
	entryByLine := map[int]*index.Entry{}
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			entryByLine[entry.LineNo] = entry
		}
	}
	inHeader := true
	ranges := []ByteRange{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line.text, "\ufeff"))
		section := strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===")
		if section {
			inHeader = false
			parent := newByteRange("section", "", line.start, line.contentEnd, line.number, line.number, raw)
			ranges = append(ranges, parent)
			context := newByteRange("directory_context", parent.Identity, line.start, line.contentEnd, line.number, line.number, raw)
			ranges = append(ranges, context)
			continue
		}
		if entry := entryByLine[line.number]; entry != nil {
			parent := newByteRange("entry", "", line.start, line.contentEnd, line.number, line.number, raw)
			ranges = append(ranges, parent)
			ranges = append(ranges, enumerateEntryAtoms(raw, line, parent.Identity)...)
			continue
		}
		kind := "structure"
		if inHeader && trimmed != "" && !strings.HasPrefix(trimmed, "#AOCI-") {
			kind = "header_atom"
		} else if !inHeader && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			kind = "directory_context"
		}
		ranges = append(ranges, newByteRange(kind, "", line.start, line.contentEnd, line.number, line.number, raw))
	}
	return ranges
}

func enumerateEntryAtoms(raw []byte, line rawLine, parent string) []ByteRange {
	text := strings.TrimPrefix(line.text, "\ufeff")
	baseOffset := line.start + int64(len(line.text)-len(text))
	result := []ByteRange{}
	open := strings.Index(text, "[")
	close := strings.Index(text, "]:")
	if open >= 0 && close > open {
		result = append(result, newByteRange("entry_tag", parent, baseOffset+int64(open+1), baseOffset+int64(close), line.number, line.number, raw))
	}
	bodyStart := close + 2
	if bodyStart < 1 || bodyStart > len(text) {
		return result
	}
	searchOffset := bodyStart
	for _, segment := range strings.Split(text[bodyStart:], " | ") {
		indexInTail := strings.Index(text[searchOffset:], segment)
		if indexInTail < 0 {
			continue
		}
		start := searchOffset + indexInTail
		end := start + len(segment)
		trimmed := strings.TrimSpace(segment)
		kind := "entry_semantic_atom"
		switch {
		case strings.HasPrefix(trimmed, "F:"):
			kind = "entry_f"
		case strings.HasPrefix(trimmed, "R:"):
			kind = "entry_r"
		case strings.HasPrefix(trimmed, "A:"):
			kind = "entry_a"
		case strings.HasPrefix(trimmed, "S:") || (len(trimmed) > 2 && trimmed[0] == 'S' && strings.Contains(trimmed[:strings.Index(trimmed, ":")+1], ":")):
			kind = "entry_s"
		}
		atom := newByteRange(kind, parent, baseOffset+int64(start), baseOffset+int64(end), line.number, line.number, raw)
		result = append(result, atom)
		if kind == "entry_r" {
			result = append(result, newByteRange("relation_atom", atom.Identity, baseOffset+int64(start), baseOffset+int64(end), line.number, line.number, raw))
		}
		searchOffset = end
	}
	return result
}

func splitRawLines(raw []byte) []rawLine {
	result := []rawLine{}
	start := 0
	line := 1
	for index := 0; index < len(raw); index++ {
		if raw[index] != '\n' {
			continue
		}
		contentEnd := index
		if contentEnd > start && raw[contentEnd-1] == '\r' {
			contentEnd--
		}
		result = append(result, rawLine{number: line, start: int64(start), contentEnd: int64(contentEnd), end: int64(index + 1), text: string(raw[start:contentEnd])})
		start = index + 1
		line++
	}
	if start <= len(raw) {
		result = append(result, rawLine{number: line, start: int64(start), contentEnd: int64(len(raw)), end: int64(len(raw)), text: string(raw[start:])})
	}
	return result
}

func newByteRange(kind, parent string, start, end int64, lineStart, lineEnd int, raw []byte) ByteRange {
	if start < 0 || end < start || end > int64(len(raw)) {
		start, end = 0, 0
	}
	data := raw[start:end]
	identity := sha256Hex([]byte(fmt.Sprintf("cognition-migration-source-range/v1\n%s\n%s\n%d\n%d\n%s\n", kind, parent, start, end, sha256Hex(data))))
	return ByteRange{Identity: identity, Kind: kind, ParentID: parent, ByteStart: start, ByteEnd: end, LineStart: lineStart, LineEnd: lineEnd, SHA256: sha256Hex(data)}
}

func snapshotIdentity(snapshot *LegacySnapshot) (string, error) {
	copyValue := *snapshot
	copyValue.CapturedAt = ""
	copyValue.SnapshotIdentity = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateSnapshot(snapshot *LegacySnapshot) error {
	if snapshot == nil || snapshot.Version != machinecontract.CognitionLegacySnapshotV1 || snapshot.NetworkAccessed ||
		snapshot.ValidatorIdentity != machinecontract.CognitionMigrationValidatorV2 || snapshot.LegacyEncoding != "base64" || snapshot.BaselineEncoding != "base64" {
		return fmt.Errorf("migration_snapshot_invalid")
	}
	legacy, err := base64.StdEncoding.Strict().DecodeString(snapshot.LegacyContentBase64)
	if err != nil || sha256Hex(legacy) != snapshot.LegacySHA256 || int64(len(legacy)) != snapshot.LegacyByteSize {
		return fmt.Errorf("migration_snapshot_legacy_bytes_invalid")
	}
	baselineBytes, err := base64.StdEncoding.Strict().DecodeString(snapshot.BaselineContentBase64)
	if err != nil || sha256Hex(baselineBytes) != snapshot.BaselineSHA256 || int64(len(baselineBytes)) != snapshot.BaselineByteSize {
		return fmt.Errorf("migration_snapshot_baseline_bytes_invalid")
	}
	if err := validateSnapshotRanges(legacy, snapshot.Ranges); err != nil {
		return err
	}
	identity, err := snapshotIdentity(snapshot)
	if err != nil || identity != snapshot.SnapshotIdentity {
		return fmt.Errorf("migration_snapshot_identity_invalid")
	}
	return validateUTC(snapshot.CapturedAt)
}

func validateSnapshotRanges(raw []byte, ranges []ByteRange) error {
	byID := make(map[string]ByteRange, len(ranges))
	children := map[string][]ByteRange{}
	top := []ByteRange{}
	for _, current := range ranges {
		if current.Identity == "" || current.ByteStart < 0 || current.ByteEnd < current.ByteStart || current.ByteEnd > int64(len(raw)) ||
			current.LineStart < 1 || current.LineEnd < current.LineStart || sha256Hex(raw[current.ByteStart:current.ByteEnd]) != current.SHA256 {
			return fmt.Errorf("migration_snapshot_range_invalid: %s", current.Identity)
		}
		if _, exists := byID[current.Identity]; exists {
			return fmt.Errorf("migration_snapshot_range_duplicate: %s", current.Identity)
		}
		byID[current.Identity] = current
		if current.ParentID == "" {
			top = append(top, current)
		} else {
			children[current.ParentID] = append(children[current.ParentID], current)
		}
	}
	validateSiblings := func(parent ByteRange, values []ByteRange) error {
		sort.Slice(values, func(i, j int) bool {
			if values[i].ByteStart != values[j].ByteStart {
				return values[i].ByteStart < values[j].ByteStart
			}
			return values[i].ByteEnd < values[j].ByteEnd
		})
		end := parent.ByteStart
		for _, child := range values {
			if child.ByteStart < parent.ByteStart || child.ByteEnd > parent.ByteEnd || child.ByteStart < end {
				return fmt.Errorf("migration_snapshot_range_overlap: %s", child.Identity)
			}
			end = child.ByteEnd
		}
		return nil
	}
	root := ByteRange{ByteStart: 0, ByteEnd: int64(len(raw))}
	if err := validateSiblings(root, top); err != nil {
		return err
	}
	for parentID, values := range children {
		parent, exists := byID[parentID]
		if !exists {
			return fmt.Errorf("migration_snapshot_range_parent_missing: %s", parentID)
		}
		if err := validateSiblings(parent, values); err != nil {
			return err
		}
	}
	return nil
}

func parseIdentity(ranges []ByteRange, findings []SnapshotFinding) string {
	value := struct {
		Ranges   []ByteRange       `json:"ranges"`
		Findings []SnapshotFinding `json:"findings"`
	}{ranges, findings}
	data, _ := canonicalJSON(value)
	return sha256Hex(data)
}

func detectBOM(raw []byte) string {
	if len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf {
		return "utf8_bom"
	}
	return "absent"
}

func detectLineEndings(raw []byte) string {
	crlf := strings.Count(string(raw), "\r\n")
	lf := strings.Count(string(raw), "\n") - crlf
	switch {
	case crlf > 0 && lf > 0:
		return "mixed"
	case crlf > 0:
		return "crlf"
	case lf > 0:
		return "lf"
	default:
		return "none"
	}
}

func modeString(mode os.FileMode) string { return fmt.Sprintf("%04o", mode.Perm()) }

func sortSnapshotFindings(findings []SnapshotFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Identity < findings[j].Identity
	})
}
