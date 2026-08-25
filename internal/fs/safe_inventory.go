package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const SafeInventoryVersion = machinecontract.SafeInventoryV2

const (
	SafetySensitive  = "sensitive"
	SafetyRuntime    = "runtime"
	SafetyGenerated  = "generated"
	SafetyConfigured = "configured"
	SafetyIgnored    = "git_ignored"
	SafetyUnsafe     = "unsafe_filesystem_object"
)

type SafeInventoryExclusion struct {
	PathSummary string `json:"path_summary"`
	Category    string `json:"category"`
	RuleSource  string `json:"rule_source"`
	GitTracked  bool   `json:"git_tracked"`
}

type SafeInventorySummary struct {
	Version                  string `json:"version"`
	GitRepository            bool   `json:"git_repository"`
	GitTracked               int    `json:"git_tracked"`
	NonignoredUntracked      int    `json:"nonignored_untracked"`
	Ignored                  int    `json:"ignored"`
	BuiltinSensitiveExcluded int    `json:"builtin_sensitive_excluded"`
	RuntimeExcluded          int    `json:"runtime_excluded"`
	GeneratedExcluded        int    `json:"generated_excluded"`
	ConfiguredExcluded       int    `json:"configured_excluded"`
	CurationExcluded         int    `json:"curation_excluded"`
	UnsafeFilesystemExcluded int    `json:"unsafe_filesystem_excluded"`
	FinalManagedCandidates   int    `json:"final_managed_candidates"`
	ReviewVisibleCount       int    `json:"review_visible_count"`
	AutoBlockerCount         int    `json:"auto_blocker_count"`
	// RequiredHumanReview is retained for Safe Inventory v2 compatibility.
	// Auto Bootstrap authorization uses AutoBlockerCount instead.
	RequiredHumanReview        int    `json:"required_human_review"`
	RulesIdentity              string `json:"rules_identity"`
	InclusionExclusionIdentity string `json:"inclusion_exclusion_identity"`
}

type SafeInventory struct {
	Summary           SafeInventorySummary     `json:"summary"`
	ManagedCandidates []string                 `json:"managed_candidates"`
	TrackedPaths      []string                 `json:"tracked_paths,omitempty"`
	IgnoredPaths      []string                 `json:"ignored_paths,omitempty"`
	Exclusions        []SafeInventoryExclusion `json:"exclusions"`
}

// BuildSafeInventory discovers path facts without opening excluded content.
// Git repositories use Git's tracked/ignore authority; non-Git repositories
// retain secure traversal. Both routes apply the same hard safety boundary.
func BuildSafeInventory(root string, opt WalkOptions) (*SafeInventory, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("safe_inventory_root_invalid")
	}
	optIn := make(map[string]bool, len(opt.HighRiskOptIn))
	for _, path := range opt.HighRiskOptIn {
		if strings.ContainsAny(path, "*?[") {
			return nil, fmt.Errorf("safe_inventory_high_risk_opt_in_invalid")
		}
		clean, ok := safeRelativePath(path)
		if !ok {
			return nil, fmt.Errorf("safe_inventory_high_risk_opt_in_invalid")
		}
		category, _ := BuiltInSafetyCategory(clean)
		if category != SafetySensitive {
			return nil, fmt.Errorf("safe_inventory_high_risk_opt_in_forbidden")
		}
		optIn[clean] = true
	}
	tracked, untracked, ignored, gitRepository, err := gitInventory(absRoot)
	if err != nil {
		return nil, err
	}
	var pruned []SafeInventoryExclusion
	if !gitRepository {
		tracked = nil
		untracked, pruned, err = traversePathNames(absRoot)
		if err != nil {
			return nil, err
		}
		ignored = nil
	}

	report := &SafeInventory{Summary: SafeInventorySummary{
		Version: SafeInventoryVersion, GitRepository: gitRepository,
		GitTracked: len(tracked), NonignoredUntracked: len(untracked), Ignored: len(ignored),
	}, ManagedCandidates: []string{}, TrackedPaths: []string{}, IgnoredPaths: []string{}, Exclusions: []SafeInventoryExclusion{}}
	if !gitRepository {
		for _, exclusion := range pruned {
			report.addExclusion(exclusion.PathSummary, exclusion.Category, exclusion.RuleSource, false)
		}
	} else {
		// Git ignored names are classified before any content read. Consumers
		// that request policy evaluation may retain otherwise-safe names as
		// candidates; ordinary Safe Inventory continues to exclude them.
		for _, rel := range ignored {
			if optIn[rel] {
				untracked = append(untracked, rel)
				continue
			}
			if category, source := BuiltInSafetyCategory(rel); category != "" {
				report.addExclusion(rel, category, source, false)
			} else if opt.IncludeIgnoredCandidates {
				untracked = append(untracked, rel)
				report.IgnoredPaths = append(report.IgnoredPaths, rel)
			} else {
				report.addExclusion(rel, SafetyIgnored, "gitignore", false)
			}
		}
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, path := range tracked {
		trackedSet[path] = true
	}
	candidates := append(append([]string{}, tracked...), untracked...)
	sort.Strings(candidates)
	seen := map[string]bool{}
	casefold := map[string]string{}
	for _, rel := range candidates {
		rel, ok := safeRelativePath(rel)
		if !ok || seen[rel] {
			continue
		}
		seen[rel] = true
		trackedPath := trackedSet[rel]
		category, source := BuiltInSafetyCategory(rel)
		if category != "" && !optIn[rel] {
			report.addExclusion(rel, category, source, trackedPath)
			continue
		}
		if configuredPathExcluded(rel, opt) {
			report.addExclusion(rel, SafetyConfigured, "project_config", trackedPath)
			continue
		}
		info, statErr := os.Lstat(filepath.Join(absRoot, filepath.FromSlash(rel)))
		if statErr != nil || unsafePlatformObject(filepath.Join(absRoot, filepath.FromSlash(rel))) || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			report.addExclusion(rel, SafetyUnsafe, "filesystem_boundary", trackedPath)
			continue
		}
		if err := recordCasefoldPath(casefold, rel); err != nil {
			return nil, err
		}
		if category != "" {
			// Explicit high-risk opt-in is a managed candidate but remains visible
			// and blocks policy-bound Auto because its body will be read.
			report.Summary.addReviewVisible(1)
			report.Summary.AutoBlockerCount++
		}
		report.ManagedCandidates = append(report.ManagedCandidates, rel)
		if trackedPath {
			report.TrackedPaths = append(report.TrackedPaths, rel)
		}
	}
	sort.Strings(report.ManagedCandidates)
	sort.Strings(report.TrackedPaths)
	sort.Strings(report.IgnoredPaths)
	sort.Slice(report.Exclusions, func(i, j int) bool { return report.Exclusions[i].PathSummary < report.Exclusions[j].PathSummary })
	report.Summary.FinalManagedCandidates = len(report.ManagedCandidates)
	report.Summary.RulesIdentity = safeInventoryRulesIdentity(opt)
	report.Summary.InclusionExclusionIdentity = safeInventorySelectionIdentity(report)
	return report, nil
}

func recordCasefoldPath(seen map[string]string, path string) error {
	folded := strings.ToLower(path)
	if previous, exists := seen[folded]; exists && previous != path {
		return fmt.Errorf("safe_inventory_casefold_conflict")
	}
	seen[folded] = path
	return nil
}

func (report *SafeInventory) addExclusion(path, category, source string, tracked bool) {
	report.Exclusions = append(report.Exclusions, SafeInventoryExclusion{PathSummary: path, Category: category, RuleSource: source, GitTracked: tracked})
	switch category {
	case SafetySensitive:
		report.Summary.BuiltinSensitiveExcluded++
	case SafetyRuntime:
		report.Summary.RuntimeExcluded++
	case SafetyGenerated:
		report.Summary.GeneratedExcluded++
	case SafetyConfigured:
		report.Summary.ConfiguredExcluded++
	case SafetyUnsafe:
		report.Summary.UnsafeFilesystemExcluded++
	}
	if tracked {
		report.Summary.addReviewVisible(1)
	}
}

// AddReviewVisible records an audit-visible inventory decision while keeping
// the historical required_human_review projection byte-for-byte meaningful.
func (summary *SafeInventorySummary) AddReviewVisible(count int) {
	summary.addReviewVisible(count)
}

func (summary *SafeInventorySummary) addReviewVisible(count int) {
	if summary == nil || count <= 0 {
		return
	}
	summary.ReviewVisibleCount += count
	summary.RequiredHumanReview += count
}

func gitInventory(root string) (tracked, untracked, ignored []string, gitRepository bool, err error) {
	if _, statErr := os.Lstat(filepath.Join(root, ".git")); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, nil, false, nil
		}
		return nil, nil, nil, false, fmt.Errorf("safe_inventory_git_boundary_unavailable")
	}
	probe := UntrustedRepositoryGitCommand(root, "rev-parse", "--show-toplevel")
	probeOutput, probeErr := probe.Output()
	var executableError *exec.Error
	if errors.As(probeErr, &executableError) {
		return nil, nil, nil, true, fmt.Errorf("safe_inventory_git_unavailable")
	}
	if probeErr != nil {
		return nil, nil, nil, true, fmt.Errorf("safe_inventory_git_query_failed")
	}
	if !sameGitRootPath(strings.TrimSpace(string(probeOutput)), root, runtime.GOOS) {
		// Once a .git boundary is present, an unverifiable or foreign repository
		// root must not silently downgrade to non-Git traversal. Doing so could
		// hide tracked sensitive files from the required-review signal.
		return nil, nil, nil, true, fmt.Errorf("safe_inventory_git_boundary_mismatch")
	}
	run := func(args ...string) ([]string, error) {
		command := UntrustedRepositoryGitCommand(root, append([]string{"-c", "core.quotepath=false"}, args...)...)
		data, commandErr := command.Output()
		if commandErr != nil {
			return nil, fmt.Errorf("safe_inventory_git_query_failed")
		}
		return splitNULPaths(data), nil
	}
	tracked, err = run("ls-files", "-z", "--cached")
	if err != nil {
		return nil, nil, nil, true, err
	}
	untracked, err = run("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, nil, nil, true, err
	}
	ignored, err = run("ls-files", "-z", "--others", "--ignored", "--exclude-standard")
	return tracked, untracked, ignored, true, err
}

func sameGitRootPath(gitRoot, inventoryRoot, goos string) bool {
	if goos == "windows" {
		canonical := func(value string) string {
			value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
			return strings.ToLower(filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))))
		}
		if canonical(gitRoot) == canonical(inventoryRoot) {
			return true
		}
		// Windows may present the Temp directory through an 8.3 short name while
		// Git reports its long-name alias. Compare directory identity before
		// rejecting that spelling as a foreign repository boundary.
		gitInfo, gitErr := os.Stat(filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(gitRoot), `\`, "/")))
		rootInfo, rootErr := os.Stat(inventoryRoot)
		return gitErr == nil && rootErr == nil && os.SameFile(gitInfo, rootInfo)
	}
	return filepath.Clean(strings.TrimSpace(gitRoot)) == filepath.Clean(inventoryRoot)
}

func splitNULPaths(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	result := make([]string, 0, len(parts))
	for _, value := range parts {
		if clean, ok := safeRelativePath(value); ok {
			result = append(result, clean)
		}
	}
	sort.Strings(result)
	return result
}

func traversePathNames(root string) ([]string, []SafeInventoryExclusion, error) {
	result := []string{}
	pruned := []SafeInventoryExclusion{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		category, _ := BuiltInSafetyCategory(rel)
		if unsafePlatformObject(path) {
			pruned = append(pruned, SafeInventoryExclusion{PathSummary: rel, Category: SafetyUnsafe, RuleSource: "filesystem_boundary"})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if category != "" {
				_, source := BuiltInSafetyCategory(rel)
				pruned = append(pruned, SafeInventoryExclusion{PathSummary: rel, Category: category, RuleSource: source})
				return filepath.SkipDir
			}
			if entry.Type()&os.ModeSymlink != 0 {
				pruned = append(pruned, SafeInventoryExclusion{PathSummary: rel, Category: SafetyUnsafe, RuleSource: "filesystem_boundary"})
				return filepath.SkipDir
			}
			return nil
		}
		result = append(result, rel)
		return nil
	})
	sort.Strings(result)
	sort.Slice(pruned, func(i, j int) bool { return pruned[i].PathSummary < pruned[j].PathSummary })
	return result, pruned, err
}

// BuiltInSafetyCategory classifies only names and never opens the target.
func BuiltInSafetyCategory(rel string) (category, source string) {
	clean, ok := safeRelativePath(rel)
	if !ok {
		return SafetyUnsafe, "path_boundary"
	}
	if clean == "aoci.code.target.txt" {
		return SafetyGenerated, "builtin_aoci_planning_artifact"
	}
	lower := strings.ToLower(clean)
	parts := strings.Split(lower, "/")
	base := parts[len(parts)-1]
	for _, part := range parts {
		switch part {
		case ".git":
			return SafetyGenerated, "builtin_vcs"
		case ".aoci":
			return SafetyRuntime, "builtin_aoci_runtime"
		case ".runtime", ".pm2", "pm2", "run", "pids", "logs", "mysql-data", "postgres-data", "postgresql-data", "pgdata", "pg_wal", "pg_xact", "redis-data":
			return SafetyRuntime, "builtin_runtime_directory"
		case "node_modules", "vendor", "dist", "build", "coverage", "cache", ".cache", "tmp", "temp", "__pycache__", ".next", ".nuxt", "target",
			"uploads", "backup", "backups", "artifacts", ".output", "third-party-dist", "third_party_dist":
			return SafetyGenerated, "builtin_generated_directory"
		}
	}
	if strings.Contains(lower, "/mysql/data/") || strings.Contains(lower, "/postgres/data/") || strings.Contains(lower, "/postgresql/data/") || strings.Contains(lower, "/redis/data/") {
		return SafetyRuntime, "builtin_database_runtime"
	}
	if sensitiveBase(base) {
		return SafetySensitive, "builtin_sensitive_name"
	}
	if strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".pid") || strings.HasSuffix(base, ".sock") || strings.HasSuffix(base, ".socket") ||
		base == "dump.rdb" || strings.HasSuffix(base, ".rdb") || strings.HasSuffix(base, ".aof") ||
		base == "ibdata1" || strings.HasSuffix(base, ".ibd") || strings.HasSuffix(base, ".frm") ||
		strings.HasSuffix(base, ".sqlite") || strings.HasSuffix(base, ".sqlite3") || strings.HasSuffix(base, ".db") {
		return SafetyRuntime, "builtin_runtime_file"
	}
	if base == ".gitkeep" || base == ".ds_store" || base == "thumbs.db" || base == "desktop.ini" || strings.HasSuffix(base, ".tmp") || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, "~") ||
		strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".class") || strings.HasSuffix(base, ".o") || strings.HasSuffix(base, ".dll") || strings.HasSuffix(base, ".dylib") || strings.HasSuffix(base, ".so") || strings.HasSuffix(base, ".exe") {
		return SafetyGenerated, "builtin_generated_file"
	}
	return "", ""
}

func sensitiveBase(base string) bool {
	if strings.HasPrefix(base, ".env.") && (strings.HasSuffix(base, ".example") || strings.HasSuffix(base, ".sample") || strings.HasSuffix(base, ".template")) {
		return false
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	if base == ".netrc" || base == ".npmrc" || base == ".pypirc" || base == "credentials" || base == "credentials.json" || base == "credential.json" || strings.HasPrefix(base, "secrets.") {
		return true
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".jks", ".keystore"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return base == "id_rsa" || base == "id_ed25519"
}

func configuredPathExcluded(rel string, opt WalkOptions) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		for _, excluded := range opt.ExcludeDirs {
			if part == strings.TrimSpace(excluded) {
				return true
			}
		}
	}
	return MatchExcludePattern(rel, opt.ExcludeFiles)
}

// PathExcludedByConfig reports project policy exclusions without reading the
// target. It is shared by Scope Refresh diagnostics and candidate discovery.
func PathExcludedByConfig(rel string, opt WalkOptions) bool {
	return configuredPathExcluded(rel, opt)
}

func safeRelativePath(value string) (string, bool) {
	value = filepath.ToSlash(strings.TrimSpace(value))
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean, value != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !filepath.IsAbs(value)
}

func safeInventoryRulesIdentity(opt WalkOptions) string {
	values := []string{SafeInventoryVersion, "sensitive-v1", "runtime-v1", "generated-v4", fmt.Sprint(opt.IncludeIgnoredCandidates)}
	values = append(values, opt.ExcludeDirs...)
	values = append(values, opt.ExcludeFiles...)
	values = append(values, opt.HighRiskOptIn...)
	return digestStrings(values)
}

func safeInventorySelectionIdentity(report *SafeInventory) string {
	return ManagedSelectionIdentity(report.Summary.RulesIdentity, report.ManagedCandidates)
}

// ManagedSelectionIdentity derives a scope identity for a consumer-defined
// subset of Safe Inventory candidates. Cognition planning uses this to keep
// formal AOCI assets outside the business-source guard while Baseline scanning
// can continue to use the complete managed candidate set.
func ManagedSelectionIdentity(rulesIdentity string, paths []string) string {
	stable := append([]string{}, paths...)
	sort.Strings(stable)
	values := []string{rulesIdentity}
	values = append(values, stable...)
	return digestStrings(values)
}

func digestStrings(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
