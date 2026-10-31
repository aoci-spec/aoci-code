// Classification helper for the unparseable-tag warning.
//
// Background (P-15/R51): ValidateEntryLineWith already recognizes the Warning "a [tag] structure is present but cannot be
// split by the ABCDE rule". score needs to aggregate that Warning separately as the tagparse dimension, but must not copy
// ParseTags or invent a second tag criterion, which would form a second set of hidden rules.
//
// This file only classifies existing Violations; it takes no part in tag parsing and changes no validation level.
package index

import "strings"

const tagParseWarningPrefix = "标签不可解析("

// HasTagParseWarning reports whether the violation list holds an unparseable-tag Warning.
//
// This function is the single entry through which the tagparse report dimension recognizes such a Warning. The judgement requires
// both LevelWarning and the stable wording prefix, so a format Error, a quota Warning, or an evolution-narrative
// Warning is not mixed in. Whether a tag is parseable is still decided entirely by ValidateEntryLineWith→ParseTags.
func HasTagParseWarning(vs []Violation) bool {
	for _, v := range vs {
		if v.Level == LevelWarning && strings.HasPrefix(v.Msg, tagParseWarningPrefix) {
			return true
		}
	}
	return false
}
