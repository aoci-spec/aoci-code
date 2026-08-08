// Maintain所需的Missing策展事实投影。
package mcptools

import "github.com/aoci-spec/aoci-code/internal/curation"

func buildMaintainCurationState(
	classification curation.Classification,
) maintainCurationState {
	pendingPaths := make(map[string]bool, len(classification.Pending))
	for _, pending := range classification.Pending {
		pendingPaths[pending.Path] = true
	}
	technicalSkipped := []curation.SkippedMissing{}
	for _, skipped := range classification.Skipped {
		if !pendingPaths[skipped.Path] {
			technicalSkipped = append(technicalSkipped, skipped)
		}
	}
	return maintainCurationState{
		pendingMissing:   len(classification.Pending),
		pending:          append([]curation.PendingCandidate{}, classification.Pending...),
		technicalSkipped: technicalSkipped,
	}
}
