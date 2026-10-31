// Single-entry form hints for the R field.
//
// The R field of an AOCI entry is a semantic annotation the model authors; it is not a CLI execution path and never
// controls file writes directly. The relations between entries are built by the model's attention while it reads the complete
// index, and no program takes part: this checker does not resolve where an R target points, does not query the filesystem, does not
// judge whether a target exists, is the same entity, or is duplicated, and builds no relation graph. An R that points at an object not
// yet authored, an object already removed, an external system, or something ambiguous by name is all the model's semantic freedom.
//
// It only asks whether this one line can be read: whether the separators are regular, whether there is an empty fragment, whether a
// placeholder is mixed with real targets. Every result is a Warning and never blocks Check, a human Apply, an MCP write-back, or Auto
// Apply. The real write target path is still hard-gated for safety by fs.NormalizeRelPath.
package index

import (
	"fmt"
	"strings"
)

// maxRelationTargetsToReport bounds the number of form hints one entry can produce, so an abnormal candidate cannot flood the output.
const maxRelationTargetsToReport = 64

// ValidateEntryRelations runs the single-entry form check on a candidate entry's R field.
//
// repoRoot stays in the signature to keep the existing call sites unchanged; this function no longer touches the filesystem.
// relPath is the repository-relative path of the current entry; line must be the complete single-line entry after
// StripFences.
//
// Entry structure and a missing or repeated R part remain ValidateEntryLineWith's job. This function checks the R value when the
// entry structure parses, and returns only LevelWarning.
func ValidateEntryRelations(
	repoRoot,
	relPath,
	line string,
) []Violation {
	match := matchEntryLine(line)
	if match == nil {
		// An entry structure error belongs to the format gate; do not report the same problem twice.
		return nil
	}

	_, relationText, _, _ := splitFRAS(match[3])
	relationText = strings.TrimSpace(relationText)

	if relationText == "" {
		return []Violation{relationWarning(
			"R字段为空: 无明确跨文件强依赖时建议使用 R:- 占位",
		)}
	}
	if relationText == "-" {
		return nil
	}

	violations := []Violation{}

	if strings.Contains(relationText, "，") {
		violations = append(
			violations,
			relationWarning(
				"R字段使用了全角逗号，建议改用半角逗号分隔目标",
			),
		)
		relationText = strings.ReplaceAll(
			relationText,
			"，",
			",",
		)
	}

	rawTargets := strings.Split(
		relationText,
		",",
	)
	checkTargets := rawTargets

	if len(rawTargets) > maxRelationTargetsToReport {
		violations = append(
			violations,
			relationWarning(
				fmt.Sprintf(
					"R目标共%d项，超过单条提示上限%d；仅提示前%d项，"+
						"Auto流程不会因此停止",
					len(rawTargets),
					maxRelationTargetsToReport,
					maxRelationTargetsToReport,
				),
			),
		)
		checkTargets = rawTargets[:maxRelationTargetsToReport]
	}

	for position, rawTarget := range checkTargets {
		target := strings.TrimSpace(rawTarget)

		if target == "" {
			violations = append(
				violations,
				relationWarning(
					fmt.Sprintf(
						"R目标第%d项为空，建议删除重复分隔符",
						position+1,
					),
				),
			)
			continue
		}

		if target == "-" {
			violations = append(
				violations,
				relationWarning(
					"R占位符与真实目标混用；无关系时写 R:-，"+
						"有关系时只列目标",
				),
			)
		}
	}

	return violations
}

func relationWarning(message string) Violation {
	return Violation{
		Level: LevelWarning,
		Msg:   message,
	}
}
