package cli

import (
	"fmt"
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
}

func estimateScanAuthoring(cfg *config.Config, skipped scanSkippedSources) scanAuthoringEstimate {
	return scanAuthoringEstimate{Targets: len(skipped.actionable),
		Rounds:     mcptools.EstimateCodeAuthoringRounds(cfg, skipped.set, skipped.actionable),
		BatchLimit: cfg.CodeCognitionBatchLimit(), TransportBudgetBytes: cfg.MaintainTransportBudget()}
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
	fmt.Println(cliMessage("scan.authoring_estimate", estimate.Targets, estimate.Rounds,
		estimate.BatchLimit, estimate.TransportBudgetBytes/1024))
	if dryRun {
		fmt.Println(cliMessage("scan.narrow_hint", root))
	}
}
