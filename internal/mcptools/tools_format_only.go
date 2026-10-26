// 受治理的gofmt-only基线快速前移。
package mcptools

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
)

func formatOnlyCandidates(
	baselineValue *baseline.Baseline,
	snapshot map[string]baseline.Fingerprint,
	stale []string,
	tolerateLineEndings bool,
) []string {
	if baselineValue == nil {
		return []string{}
	}
	result := []string{}
	for _, rel := range stale {
		before, exists := baselineValue.Files[rel]
		after, currentExists := snapshot[rel]
		lineEndingOnly := before.NormalizedSHA256 != "" &&
			before.NormalizedSHA256 == after.NormalizedSHA256
		if lineEndingOnly && !tolerateLineEndings {
			continue
		}
		if exists && currentExists && baseline.IsFormatOnlyChange(before, after) {
			result = append(result, rel)
		}
	}
	return result
}
