// Cross-platform directory traversal
// Index entry: walk.go[FS.Walk.8.Xp.S]
//
// Discipline:
//   - the traversal layer prunes only by built-in category (node_modules is a tens-of-thousands-of-files
//     performance difference); exclude_dirs and exclude_root_dirs are judged per file in BuildSafeInventory,
//     and a directory git ignores whose whole tree hits a hard exclusion collapses into one line (HardExcludedDirectory);
//   - symbolic links are not followed (against loops and escapes);
//   - only two unconditional built-in exclusions: .aoci (against self-ingestion) and .git (a VCS internal directory
//     of zero index value that holds binary packs; the 2026-07-10 httpx live defect: a fresh init had an empty
//     default configuration and .git was scanned wholesale into the inventory and the baseline); neither depends
//     on the caller's configuration and neither can be switched off by the user;
//   - common business exclusions such as node_modules and vendor are not hard-coded here; they enter the
//     configuration layer through config.DefaultConfig (visible and editable) and this layer takes only the final WalkOptions;
//   - output is sorted by rel_path so downstream JSON stays stable and diffable.
//
// Dependency note: this package does not import internal/config (avoiding the fs<-config<-fs cycle);
// exclusion rules are injected by the caller through WalkOptions, and the config package maps config.json onto this structure.
package fs

import "strings"

// WalkOptions are the traversal exclusion rules
type WalkOptions struct {
	// ExcludeDirs excludes by directory base name (node_modules, for example)
	ExcludeDirs []string
	// ExcludeRootDirs matches the first repository-relative directory, and a
	// git-ignored directory of that name at any depth (ignoredArtifactSource).
	ExcludeRootDirs []string
	// ExcludeFiles are file exclusion patterns with the platform's matchExcludePattern semantics:
	// *.bak suffix / backup_* prefix / *.backup.* contains / without * an exact base name or relative path
	ExcludeFiles []string
	// HighRiskOptIn contains exact repository-relative sensitive-file paths.
	// Runtime, generated, AOCI, VCS, and unsafe filesystem boundaries cannot be
	// opted in through this mechanism.
	HighRiskOptIn []string
	// IncludeIgnoredCandidates keeps Git-ignored regular-file names available
	// to Managed Scope evaluation. The Safe Inventory hard boundary still
	// excludes sensitive, runtime, generated, and unsafe objects before any
	// content read. Legacy callers leave this false and retain rc17 behavior.
	IncludeIgnoredCandidates bool
	// SensitiveNameRules selects the built-in sensitive-name rule set: "" or
	// SensitiveNameRulesV1 is the rc22 set, SensitiveNameRulesV2 adds the
	// shapes operational repositories use for credentials (#148). The value is
	// persisted team configuration, never a built-in default, because the rule
	// set enters every scope identity (safeInventoryRulesIdentity).
	SensitiveNameRules string
}

// WalkRepo traverses the repository and returns the sorted relative paths (forward slashes, regular files only).
func WalkRepo(root string, opt WalkOptions) ([]string, error) {
	inventory, err := BuildSafeInventory(root, opt)
	if err != nil {
		return nil, err
	}
	return append([]string{}, inventory.ManagedCandidates...), nil
}

// MatchExcludePattern matches a file exclusion pattern with the platform's matchExcludePattern semantics:
// with * it supports prefix (prefix*), suffix (*.ext), contains (*.backup.*), and prefix plus suffix (backup_*.sql);
// without * it matches the base name or the full relative path exactly.
func MatchExcludePattern(relPath string, patterns []string) bool {
	base := relPath
	if i := strings.LastIndex(relPath, "/"); i >= 0 {
		base = relPath[i+1:]
	}
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if !strings.Contains(pat, "*") {
			if pat == base || pat == relPath {
				return true
			}
			continue
		}
		switch {
		case strings.HasPrefix(pat, "*") && strings.HasSuffix(pat, "*"):
			// *xx* contains
			mid := strings.Trim(pat, "*")
			if mid != "" && strings.Contains(base, mid) {
				return true
			}
		case strings.HasPrefix(pat, "*"):
			// *.ext suffix
			if strings.HasSuffix(base, strings.TrimPrefix(pat, "*")) {
				return true
			}
		case strings.HasSuffix(pat, "*"):
			// prefix* prefix
			if strings.HasPrefix(base, strings.TrimSuffix(pat, "*")) {
				return true
			}
		default:
			// infix *: prefix*suffix (backup_*.sql, for example)
			i := strings.Index(pat, "*")
			pre, suf := pat[:i], pat[i+1:]
			if strings.HasPrefix(base, pre) && strings.HasSuffix(base, suf) {
				return true
			}
		}
	}
	return false
}
