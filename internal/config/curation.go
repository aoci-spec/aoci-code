// Team static path exclusion matching semantics.
//
// This file handles only the curation_exclude path selectors in config.json, the team-level negative-space policy the
// maintainer declares in advance, which is not the same as the formal file-level semantic curation asset:
//
//   - config.curation_exclude: static path body or directory subtree selectors;
//   - .aoci/curation.json: file-level include/exclude decisions maintained by internal/curation and bound to
//     source_sha256.
//
// When both layers take part in Missing classification, the static path policy wins over the file-level decision.
//
// The matching semantics first lived in the mcptools package; as maintain, build, verify, and the formal Missing
// classification came to share it, it moved into the config package as the single source of truth, so the matching logic is not copied across packages.
//
// v1.1 semantics:
//   - every item is a repository-relative path selector, with or without a trailing slash;
//   - it matches the path body or its directory subtree, by directory boundary;
//   - an exact single-file hit is supported;
//   - backslashes, repeated slashes, and "./" are normalized uniformly;
//   - absolute paths, paths escaping the repository, and blank items are invalid;
//   - wildcards are not supported yet, so they do not mix with fs.MatchExcludePattern into undecided semantics.
//
// Current consumption boundary:
//   - Missing classification: converted to CurationExcludedMissing first;
//   - maintain: filters only the Missing dispatch surface; the Stale of existing entries is still handled entry-first;
//   - build --missing: filters the automatic drafting queue, and an explicit --paths keeps the human override;
//   - verify: keeps the raw Missing fact while showing the static path exclusion source.
package config

import (
	"path"
	"strings"
)

// normalizeCurationPath normalizes a static path selector or a path to be matched into a repository-relative path.
// An empty return means the input is invalid or should not take part in matching.
func normalizeCurationPath(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if s == "" {
		return ""
	}

	// path.Clean unifies repeated slashes, trailing slashes, and "./".
	clean := path.Clean(s)
	if clean == "." ||
		path.IsAbs(clean) ||
		clean == ".." ||
		strings.HasPrefix(clean, "../") {
		return ""
	}

	return strings.TrimSuffix(clean, "/")
}

// CurationExcluded reports whether rel hits the config.curation_exclude static path policy.
//
// The policy means "does not enter automatic recording", not a rewrite of the fact "the file does not exist"; verify still keeps
// the raw Missing. The formal file-level include/exclude decision is handled by internal/curation.
func (c *Config) CurationExcluded(rel string) bool {
	if c == nil || len(c.CurationExclude) == 0 {
		return false
	}

	rel = normalizeCurationPath(rel)
	if rel == "" {
		return false
	}

	for _, raw := range c.CurationExclude {
		ex := normalizeCurationPath(raw)
		if ex == "" {
			continue
		}
		if rel == ex ||
			strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}

	return false
}

// CountCurationExcluded counts the entries of a path list that hit the static path policy.
// A caller needing complete path evidence should walk CurationExcluded directly and collect.
func (c *Config) CountCurationExcluded(rels []string) int {
	count := 0

	for _, rel := range rels {
		if c.CurationExcluded(rel) {
			count++
		}
	}

	return count
}
