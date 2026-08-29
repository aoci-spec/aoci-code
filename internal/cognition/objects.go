package cognition

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var databaseSectionPattern = regexp.MustCompile(`^===(.*?)/(database://[^\s=]+/)===\s*$`)
var databaseNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$-]*$`)

// IsCanonicalDatabaseRef reports whether ref is the stable table identity
// database://source/namespace/table. It validates only identity syntax and
// never infers or generates table cognition.
func IsCanonicalDatabaseRef(ref string) bool {
	if !strings.HasPrefix(ref, "database://") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(ref, "database://"), "/")
	return len(parts) == 3 && databaseNamePattern.MatchString(parts[0]) &&
		databaseNamePattern.MatchString(parts[1]) && databaseNamePattern.MatchString(parts[2])
}

func parseCodeVolume(repositoryRoot string, raw []byte, sq *index.SQuotaThresholds) (*index.Document, []Object, []Finding) {
	doc, warnings := index.Parse(string(raw))
	index.ResolveRelPaths(doc, repositoryRoot)
	findings := make([]Finding, 0, len(warnings))
	for _, warning := range warnings {
		findings = append(findings, Finding{Code: "code_parse_warning", AssetID: "code", Line: warning.LineNo, Message: warning.Msg})
	}
	var objects []Object
	seen := map[string]int{}
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath == "" {
				findings = append(findings, Finding{Code: "code_object_path_unresolved", AssetID: "code", Line: entry.LineNo, Message: "code Entry cannot be mapped to a repository-relative path"})
				continue
			}
			key := strings.ToLower(entry.RelPath)
			if previous := seen[key]; previous != 0 {
				findings = append(findings, Finding{Code: "code_object_duplicate", AssetID: "code", Line: entry.LineNo, Message: fmt.Sprintf("code object duplicates line %d under cross-platform case folding", previous)})
				continue
			}
			seen[key] = entry.LineNo
			for _, violation := range validateFRASV2(entry, sq) {
				findings = append(findings, Finding{
					Code: violation.Code, AssetID: "code", Line: entry.LineNo, Message: violation.Message,
				})
			}
			objects = append(objects, Object{VolumeID: "code", Kind: "file", Name: entry.Filename, CanonicalRef: "code:" + entry.RelPath, Entry: entry, CanonicalLine: entry.FullLine, SourceSection: section.HeaderLine, SourceLineNumber: entry.LineNo})
		}
	}
	return doc, objects, findings
}

func parseDatabaseVolume(raw []byte, sq *index.SQuotaThresholds) ([]Object, []Finding) {
	lines := splitLines(raw)
	seen := map[string]int{}
	var objects []Object
	var findings []Finding
	namespace := ""
	section := ""
	for lineIndex, line := range lines {
		lineNumber := lineIndex + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "===") {
			match := databaseSectionPattern.FindStringSubmatch(trimmed)
			if match == nil {
				findings = append(findings, Finding{Code: "database_section_invalid", AssetID: "database", Line: lineNumber, Message: "database sections must end in a database:// source/namespace URI"})
				namespace, section = "", ""
				continue
			}
			namespace = strings.TrimSuffix(match[2], "/")
			parts := strings.Split(strings.TrimPrefix(namespace, "database://"), "/")
			if len(parts) != 2 || !databaseNamePattern.MatchString(parts[0]) || !databaseNamePattern.MatchString(parts[1]) {
				findings = append(findings, Finding{Code: "database_namespace_invalid", AssetID: "database", Line: lineNumber, Message: "database namespace must be database://source/namespace"})
				namespace, section = "", ""
				continue
			}
			section = line
			continue
		}
		entry, ok := index.ParseEntryLine(line, lineNumber)
		if !ok || namespace == "" {
			findings = append(findings, Finding{Code: "database_entry_invalid", AssetID: "database", Line: lineNumber, Message: "database business content must be a table FRAS Entry inside a database section"})
			continue
		}
		if !databaseNamePattern.MatchString(entry.Filename) {
			findings = append(findings, Finding{Code: "database_table_name_invalid", AssetID: "database", Line: lineNumber, Message: "Database v1 object_name must be one unqualified table name"})
			continue
		}
		key := strings.ToLower(namespace + "/" + entry.Filename)
		if previous := seen[key]; previous != 0 {
			findings = append(findings, Finding{Code: "database_table_duplicate", AssetID: "database", Line: lineNumber, Message: fmt.Sprintf("table duplicates line %d", previous)})
			continue
		}
		seen[key] = lineNumber
		for _, violation := range validateFRASV2(entry, sq) {
			findings = append(findings, Finding{
				Code: violation.Code, AssetID: "database", Line: lineNumber, Message: violation.Message,
			})
		}
		objects = append(objects, Object{VolumeID: "database", Kind: "table", Name: entry.Filename, Namespace: namespace, CanonicalRef: namespace + "/" + entry.Filename, Entry: entry, CanonicalLine: entry.FullLine, SourceSection: section, SourceLineNumber: lineNumber})
	}
	return objects, findings
}

func validateFRASV2(entry *index.Entry, sq *index.SQuotaThresholds) []RepairFinding {
	var findings []RepairFinding
	violations := index.ValidateEntryLine(entry.Filename, entry.FullLine)
	for _, violation := range violations {
		if violation.Level == index.LevelError {
			findings = append(findings, frasFinding(
				"FRAS", "fras_structure_invalid", "canonical_structure=true",
				"canonical_structure=false", violation.Msg,
			))
		}
	}
	if !hasCanonicalV2Tag(entry.TagsParsed) {
		findings = append(findings, frasFinding(
			"tag", "fras_tag_invalid", "format=ABCDE_compact_or_dotted",
			"format=invalid", "tag must use the canonical ABCDE compact or dotted grammar",
		))
	}
	if !hasCanonicalV2FRASOrder(entry.FullLine) {
		findings = append(findings, frasFinding(
			"FRAS", "fras_structure_invalid", "field_order=F,R,A,S",
			"field_order=invalid", "Volumes v1 objects require exactly one F, R, A, and S field in canonical order",
		))
	}
	for _, field := range []struct{ name, value string }{{"F", entry.F}, {"R", entry.R}, {"A", entry.Api}, {"S", entry.S}} {
		if strings.TrimSpace(field.value) == "" {
			ruleCode := "fras_" + strings.ToLower(field.name) + "_empty"
			findings = append(findings, frasFinding(
				field.name, ruleCode, "content=model_authored_or_dash",
				"content=empty", field.name+" must have model-authored content or the canonical - placeholder",
			))
		}
	}
	limits := machinecontract.ObjectFRASV2Limits()
	checks := []struct {
		name, value string
		max         int
	}{
		{"F", entry.F, limits.FMaxRunes}, {"R", entry.R, limits.RMaxRunes}, {"A", entry.Api, limits.AMaxRunes},
	}
	for _, check := range checks {
		actual := utf8.RuneCountInString(check.value)
		if actual > check.max {
			ruleCode := "fras_" + strings.ToLower(check.name) + "_too_long"
			findings = append(findings, frasFinding(
				check.name, ruleCode, fmt.Sprintf("max_runes=%d", check.max),
				fmt.Sprintf("rune_count=%d", actual),
				fmt.Sprintf("%s contains %d Unicode characters; Volumes v1 maximum is %d; regenerate the complete Entry", check.name, actual, check.max),
			))
		}
	}
	for _, check := range []struct {
		name, value string
		max         int
	}{{"R", entry.R, limits.RMaxItems}, {"A", entry.Api, limits.AMaxItems}} {
		actual := countListItems(check.value)
		if actual > check.max {
			ruleCode := "fras_" + strings.ToLower(check.name) + "_too_many_items"
			findings = append(findings, frasFinding(
				check.name, ruleCode, fmt.Sprintf("max_items=%d", check.max),
				fmt.Sprintf("item_count=%d", actual),
				fmt.Sprintf("%s contains %d items; Volumes v1 maximum is %d; regenerate the complete Entry", check.name, actual, check.max),
			))
		}
	}
	importance := 0
	if value := entry.TagsParsed["C"]; len(value) == 1 && value[0] >= '1' && value[0] <= '9' {
		importance = int(value[0] - '0')
	}
	// The effective limit comes from index.LimitForC, the single place that
	// resolves a C band against the repository's Meta declaration. This gate used
	// to call machinecontract directly and so ignored the declaration entirely,
	// while the authoring contract the model is handed honours it: a repository
	// declaring a wider band was told 500, authored to it, and was refused at 200
	// with no edit that could clear the block. LimitForC only ever loosens, so a
	// Volume that loads today still loads.
	if importance > 0 {
		maximum, declared := sq.LimitForC(importance)
		if actual := utf8.RuneCountInString(entry.S); actual > maximum {
			source := "the machine default"
			if declared {
				source = "this repository's Meta S quota declaration"
			}
			findings = append(findings, frasFinding(
				"S", "fras_s_too_long", fmt.Sprintf("max_runes=%d", maximum),
				fmt.Sprintf("rune_count=%d", actual),
				fmt.Sprintf("S contains %d Unicode characters; the C%d maximum is %d by %s; regenerate the complete Entry",
					actual, importance, maximum, source),
			))
		}
	}
	return findings
}

func frasFinding(field, ruleCode, expected, actual, cause string) RepairFinding {
	return RepairFinding{
		Code: ruleCode, Field: field, RuleCode: ruleCode, Expected: expected,
		Actual: actual, Cause: cause, Message: cause,
	}
}

func hasCanonicalV2Tag(tags map[string]string) bool {
	for _, required := range []string{"A", "B", "C", "E"} {
		if strings.TrimSpace(tags[required]) == "" {
			return false
		}
	}
	importance := tags["C"]
	return len(importance) == 1 && importance[0] >= '1' && importance[0] <= '9'
}

func hasCanonicalV2FRASOrder(line string) bool {
	separator := strings.Index(line, "]:")
	if separator < 0 {
		return false
	}
	segments := strings.Split(strings.TrimSpace(line[separator+2:]), " | ")
	if len(segments) != 4 {
		return false
	}
	for index, prefix := range []string{"F:", "R:", "A:", "S:"} {
		segment := strings.TrimSpace(segments[index])
		if !strings.HasPrefix(segment, prefix) || strings.TrimSpace(strings.TrimPrefix(segment, prefix)) == "" {
			return false
		}
	}
	return true
}

func countListItems(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0
	}
	count := 0
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}
