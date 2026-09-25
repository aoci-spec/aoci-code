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
}

func classifyScanSkippedSources(root string, cfg *config.Config, snap map[string]baseline.Fingerprint) scanSkippedSources {
	// A re-scan of a Legacy repository may find files that already carry an
	// Entry; those are governed objects, not sources held out of authoring.
	described := map[string]bool{}
	if set, err := cognition.Load(root, cfg.IndexPath); err == nil && set != nil {
		if asset := set.Volumes[cognition.ScopeCode]; asset != nil {
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
	var counts scanSkippedSources
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
