package cognition

import (
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
)

// ValidateVolumeAuthoringExample runs one calibration line through the same
// object parser, FRAS validator, and scoped dictionary decision used by formal
// Volumes. It performs no filesystem, network, governance, or write action.
func ValidateVolumeAuthoringExample(domain, line string, dictionary *index.TagDict) []RepairFinding {
	var objects []Object
	var parseFindings []Finding
	switch domain {
	case ScopeCode:
		raw := CodeVolumeMarker + "\n===Example/example/===\n" + strings.TrimSpace(line) + "\n"
		_, objects, parseFindings = parseCodeVolume("/example", []byte(raw))
	case ScopeDatabase:
		raw := DatabaseMarker + "\n===Example/database://source/namespace/===\n" + strings.TrimSpace(line) + "\n"
		objects, parseFindings = parseDatabaseVolume([]byte(raw))
	default:
		return []RepairFinding{frasFinding("domain", "authoring_example_domain_invalid", "domain=code_or_database", "domain="+domain, "unsupported authoring example domain")}
	}
	if len(parseFindings) > 0 {
		result := make([]RepairFinding, 0, len(parseFindings))
		for _, finding := range parseFindings {
			result = append(result, frasFinding("FRAS", finding.Code, "formal_parser=valid", "formal_parser=invalid", finding.Message))
		}
		return result
	}
	if len(objects) != 1 || objects[0].Entry == nil {
		return []RepairFinding{frasFinding("FRAS", "authoring_example_object_count_invalid", "object_count=1", "object_count=0", "authoring example must parse as exactly one formal object")}
	}
	violations := index.ValidateTagAgainstDict(objects[0].Entry.TagsParsed, objects[0].Entry.TagsRaw, dictionary)
	result := make([]RepairFinding, 0, len(violations))
	for _, violation := range violations {
		result = append(result, frasFinding("tag", "object_tag_dictionary_violation", violation.Expected, violation.Actual, violation.Cause))
	}
	return result
}
