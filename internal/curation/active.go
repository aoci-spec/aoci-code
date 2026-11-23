package curation

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ActiveExclusions is the one judgement of which paths Managed Scope excludes
// by curation: the configured list, plus every exclude decision whose source
// still has the bytes it was decided on. A path the active Baseline records is
// matched against that record; a path it no longer records, because the
// Scope Change that applied this very decision removed it, is matched against
// the file on disk. Matching only the Baseline dropped the decision the moment
// it took effect, so the repository asked for the same Scope Change again
// (audit G4-04); three copies of this rule used to live in managedstate,
// scopechange, and the CLI.
func ActiveExclusions(root string, configured []string, document *Document, active *baseline.Baseline) []string {
	result := append([]string{}, configured...)
	if document != nil && active != nil {
		for _, decision := range document.Decisions {
			if decision.Decision != DecisionExclude {
				continue
			}
			if fingerprint, recorded := active.Files[decision.Path]; recorded {
				if fingerprint.SHA256 == decision.SourceSHA256 {
					result = append(result, decision.Path)
				}
				continue
			}
			// The path left the Baseline, so nothing has vetted it: read it
			// only as a regular file inside the repository (rv34).
			if data, err := afs.ReadRegularFileInRoot(root, decision.Path); err == nil && sha256Hex(data) == decision.SourceSHA256 {
				result = append(result, decision.Path)
			}
		}
	}
	sort.Strings(result)
	deduplicated := result[:0]
	for _, path := range result {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != path {
			deduplicated = append(deduplicated, path)
		}
	}
	return deduplicated
}
