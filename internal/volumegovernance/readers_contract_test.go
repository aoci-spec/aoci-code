package volumegovernance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Code drift of a Volumes repository is computed in exactly one place,
// CodeDrift, and every reader consumes it. #158 was three readers answering
// "is a formal asset Observe evidence" three ways; a fourth surfaced in the
// rc23 pre-tag audit. The files below may still call the detector or the
// business-scope projection directly, each for the reason given; a new
// caller fails this test until it is either routed through CodeDrift or
// listed here with a reason a reviewer can weigh.
func TestVolumesCodeDriftHasOneProjection(t *testing.T) {
	allowed := map[string]string{
		"internal/managedstate/state.go":            "the Detect wrapper CodeDrift calls",
		"internal/volumegovernance/facts.go":        "the projection itself",
		"internal/scopechange/plan.go":              "the active business Baseline identity a Scope Change binds, a writer",
		"internal/cli/scope.go":                     "the Legacy branch of scope status, raw lists, read-only on this line",
		"internal/cli/status.go":                    "Legacy status --deep; Volumes refuses",
		"internal/cli/verify.go":                    "Legacy verify; Volumes renders the projection",
		"internal/mcptools/tools_read_refresh.go":   "Legacy semantic change facts; Volumes adapts the projection",
		"internal/mcptools/tools_auto_alignment.go": "the Legacy inspector; the Volumes inspector reads CodeDrift",
		"internal/indexgen/score.go":                "the Legacy generator, retired with it in 0.2.0-rc2",
	}
	patterns := []string{"managedstate.Detect(", "baseline.DetectManagedScope(", "baseline.DetectWith(",
		"volumegovernance.ClassifyCodeDrift(", "cognition.BusinessScope("}
	repo := filepath.Join("..", "..")
	found := map[string][]string{}
	for _, top := range []string{"internal", "engine", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repo, top), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, relErr := filepath.Rel(repo, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			for _, pattern := range patterns {
				if strings.Contains(string(data), pattern) {
					found[rel] = append(found[rel], pattern)
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	var unexpected, missing []string
	for rel, hits := range found {
		if _, ok := allowed[rel]; !ok {
			unexpected = append(unexpected, rel+" ("+strings.Join(hits, ", ")+")")
		}
	}
	for rel := range allowed {
		if _, ok := found[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(missing)
	if len(unexpected) > 0 {
		t.Fatalf("new readers compute Code drift beside the projection; route them through CodeDrift or list them with a reason:\n  %s", strings.Join(unexpected, "\n  "))
	}
	if len(missing) > 0 {
		t.Fatalf("allowlisted files no longer call the detector; drop them from the list so it stays exact:\n  %s", strings.Join(missing, "\n  "))
	}
}
