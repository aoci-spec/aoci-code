package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// scanSkippedSources counts, at scan time, the index-role files a model will
// never be asked to author: their bytes carry nothing it can read (empty,
// binary, above the read limit) or a curation decision keeps them out. The counts come from the same classification Verify reports
// as code_skipped and code_curation_excluded, so what scan announces is what
// governance holds out, and the operator learns before the first Maintain
// that an image needs neither an Entry nor a decision.
type scanSkippedSources struct {
	Total            int `json:"total"`
	Binary           int `json:"binary"`
	Empty            int `json:"empty"`
	Oversize         int `json:"oversize"`
	CurationExcluded int `json:"curation_excluded"`
	// actionable is the complement: the index-role files a model will be
	// asked to author, which sizes the first build.
	actionable []string
	// set is the loaded cognition layout, when it loads; the estimate sizes
	// the response with the repository's own Meta.
	set *cognition.Set
}

// scanAuthoringEstimate tells the operator what the first build costs before
// the model starts: how many files need an Entry and how many Maintain
// rounds the current batch rule takes to cover them.
type scanAuthoringEstimate struct {
	Targets              int `json:"targets"`
	Rounds               int `json:"rounds"`
	BatchLimit           int `json:"batch_limit"`
	TransportBudgetBytes int `json:"transport_budget_bytes"`
	// ParallelBatches is the team's code_cognition_parallel_batches and
	// ParallelRounds the rounds each of that many Agent sessions takes when
	// they author at once (L4): the first build is the hours a user waits
	// longest for, and this is the lever that divides them.
	ParallelBatches int `json:"parallel_batches"`
	ParallelRounds  int `json:"parallel_rounds"`
	// SourceLines is what the first build asks a model to read in all, and
	// SourceLinesPerRound the most one round asks for (a larger file travels
	// alone): the cost a user could not see before the model started.
	SourceLines         int `json:"source_lines"`
	SourceLinesPerRound int `json:"source_lines_per_round"`
}

func estimateScanAuthoring(root string, cfg *config.Config, skipped scanSkippedSources) scanAuthoringEstimate {
	return estimateAuthoringForPaths(root, cfg, skipped.set, skipped.actionable)
}

// estimateAuthoringForPaths is the first-build estimate over the given
// index-role paths, shared by scan and by init, which shows it before the
// first scan while narrowing the scope is still free.
func estimateAuthoringForPaths(root string, cfg *config.Config, set *cognition.Set, paths []string) scanAuthoringEstimate {
	lines, cachedLines, total := countSourceLines(root, paths)
	rounds, parallelRounds := mcptools.EstimateCodeAuthoringRounds(cfg, set, paths, lines, cachedLines)
	return scanAuthoringEstimate{Targets: len(paths), Rounds: rounds,
		BatchLimit: cfg.CodeCognitionBatchLimit(), TransportBudgetBytes: cfg.MaintainTransportBudget(),
		ParallelBatches: cfg.CodeCognitionParallelBatchLimit(), ParallelRounds: parallelRounds,
		SourceLines: total, SourceLinesPerRound: machinecontract.CodeCognitionBatchSourceLines}
}

// initNarrowHintRounds is the first-build size from which init points at
// the free moment to start smaller: before the first scan freezes roles,
// a narrower scope costs nothing, and after it a reduction is a governed
// Scope Change.
const initNarrowHintRounds = 20

// initAuthoringEstimateLines are the lines init prints about the first
// build of a fresh repository: its size, what parallel sessions make of
// it, and, for a large one, how to start smaller before the first scan.
func initAuthoringEstimateLines(root string, cfg *config.Config, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return nil
	}
	estimate := estimateAuthoringForPaths(root, cfg, set, paths)
	lines := []string{scanAuthoringEstimateLine(estimate)}
	if line := scanReadingEstimateLine(estimate); line != "" {
		lines = append(lines, line)
	}
	if line := scanParallelEstimateLine(estimate); line != "" {
		lines = append(lines, line)
	}
	if estimate.Rounds >= initNarrowHintRounds {
		lines = append(lines, cliMessage("scan.narrow_hint", shellPathArgument(root)))
	}
	return lines
}

func classifyScanSkippedSources(root string, cfg *config.Config, snap map[string]baseline.Fingerprint) scanSkippedSources {
	// A re-scan of a Legacy repository may find files that already carry an
	// Entry; those are governed objects, not sources held out of authoring.
	described := map[string]bool{}
	var loadedSet *cognition.Set
	if set, err := cognition.Load(root, cfg.IndexPath); err == nil && set != nil {
		loadedSet = set
		// A Legacy index keeps its Entries on the Root asset; a Volumes
		// layout keeps them on the Code Volume.
		for _, asset := range []*cognition.Asset{set.Volumes[cognition.ScopeCode], &set.Root} {
			if asset == nil {
				continue
			}
			for _, object := range asset.Objects {
				described[strings.TrimPrefix(object.CanonicalRef, "code:")] = true
			}
		}
	}
	paths := make([]string, 0, len(snap))
	for rel, fingerprint := range snap {
		if _, formal := cognition.FormalAssetOwner(rel); formal || rel == cfg.IndexPath || described[rel] {
			continue
		}
		if baseline.EffectiveRole(fingerprint) == machinecontract.ScopeRoleIndex {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	counts := scanSkippedSources{set: loadedSet}
	classification, _, _, err := curation.BuildClassification(root, cfg, paths)
	if err != nil {
		return counts
	}
	for _, item := range classification.Skipped {
		counts.Total++
		switch item.Reason {
		case curation.ProfileReasonBinary:
			counts.Binary++
		case curation.ProfileReasonEmpty:
			counts.Empty++
		case curation.ProfileReasonOversize:
			counts.Oversize++
		}
	}
	counts.CurationExcluded = len(classification.CurationExcluded)
	counts.actionable = append([]string{}, classification.Actionable...)
	return counts
}

func printScanSkippedSources(counts scanSkippedSources) {
	if counts.Total > 0 {
		fmt.Println(cliMessage("scan.skipped_sources", counts.Total, counts.Binary, counts.Empty, counts.Oversize))
	}
	if counts.CurationExcluded > 0 {
		fmt.Println(cliMessage("scan.curation_excluded", counts.CurationExcluded))
	}
}

// printScanAuthoringEstimate prints the first-build cost, and on a dry run
// the one moment a narrower scope is still free: before the first scan.
func printScanAuthoringEstimate(root string, estimate scanAuthoringEstimate, dryRun bool) {
	if estimate.Targets == 0 {
		return
	}
	fmt.Println(scanAuthoringEstimateLine(estimate))
	if line := scanReadingEstimateLine(estimate); line != "" {
		fmt.Println(line)
	}
	if line := scanParallelEstimateLine(estimate); line != "" {
		fmt.Println(line)
	}
	if dryRun {
		fmt.Println(cliMessage("scan.narrow_hint", shellPathArgument(root)))
	}
}

// countSourceLines keeps the line counts available to Maintain's wire sizing
// separate from the counts this estimate reads for the source-line budget.
func countSourceLines(root string, paths []string) (lines, cachedLines map[string]int, total int) {
	cache := baseline.OpenFingerprintCache(root)
	lines = make(map[string]int, len(paths))
	cachedLines = make(map[string]int, len(paths))
	for _, path := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		count, known := cache.KnownLines(absolute, path)
		if known {
			cachedLines[path] = count
		} else {
			var err error
			count, err = cache.FileLines(absolute, path)
			if err != nil {
				continue
			}
		}
		lines[path] = count
		total += count
	}
	return lines, cachedLines, total
}

// scanReadingEstimateLine says how much source the first build reads; a
// repository with nothing counted says nothing.
func scanReadingEstimateLine(estimate scanAuthoringEstimate) string {
	if estimate.SourceLines == 0 {
		return ""
	}
	return cliMessage("scan.authoring_reading", estimate.SourceLines, estimate.SourceLinesPerRound)
}

// scanParallelEstimateLine says what parallel sessions make of the estimate:
// the rounds each session takes under the team's setting, or, at the
// setting of one, what four sessions would take and how to turn that on. A
// one-round build has nothing to divide.
func scanParallelEstimateLine(estimate scanAuthoringEstimate) string {
	if estimate.Rounds < 2 {
		return ""
	}
	if estimate.ParallelBatches > 1 {
		if estimate.ParallelRounds == 1 {
			return cliMessage("scan.authoring_estimate_parallel_one", estimate.ParallelBatches)
		}
		return cliMessage("scan.authoring_estimate_parallel", estimate.ParallelBatches, estimate.ParallelRounds)
	}
	if estimate.ParallelRounds == 1 {
		return cliMessage("scan.authoring_estimate_parallel_hint_one")
	}
	return cliMessage("scan.authoring_estimate_parallel_hint", estimate.ParallelRounds)
}

// scanAuthoringEstimateLine spells the first-build cost; one round is the
// usual answer for a small repository and reads as "1 rounds" otherwise.
func scanAuthoringEstimateLine(estimate scanAuthoringEstimate) string {
	if estimate.Targets == 1 {
		return cliMessage("scan.authoring_estimate_single", estimate.BatchLimit, estimate.TransportBudgetBytes/1024)
	}
	if estimate.Rounds == 1 {
		return cliMessage("scan.authoring_estimate_one", estimate.Targets, estimate.BatchLimit, estimate.TransportBudgetBytes/1024)
	}
	return cliMessage("scan.authoring_estimate", estimate.Targets, estimate.Rounds,
		estimate.BatchLimit, estimate.TransportBudgetBytes/1024)
}
