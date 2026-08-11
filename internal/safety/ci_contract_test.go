package safety

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func ciRepositoryFile(t *testing.T, elements ...string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve CI contract test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	data, err := os.ReadFile(filepath.Join(append([]string{root}, elements...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRequiredWorkflowsPinEveryActionToFullSHAWithVersionComment(t *testing.T) {
	pinned := regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*[^\s@]+@[0-9a-f]{40}\s+#\s+v[^\s]+\s*$`)
	uses := regexp.MustCompile(`(?m)^\s*-?\s*uses:.*$`)
	for _, workflow := range []string{"ci.yml", "full-confidence.yml", "release-rehearsal.yml"} {
		content := ciRepositoryFile(t, ".github", "workflows", workflow)
		lines := uses.FindAllString(content, -1)
		if len(lines) == 0 {
			t.Fatalf("%s has no action steps", workflow)
		}
		for _, line := range lines {
			if !pinned.MatchString(line) {
				t.Errorf("%s action is not pinned with a version comment: %s", workflow, strings.TrimSpace(line))
			}
		}
	}
}

func TestFullConfidenceRunsForEveryMainPush(t *testing.T) {
	content := ciRepositoryFile(t, ".github", "workflows", "full-confidence.yml")
	if regexp.MustCompile(`(?m)^\s{4}paths(?:-ignore)?:`).MatchString(content) {
		t.Fatal("full-confidence main push remains restricted by a paths filter")
	}
}

func TestSafetyAndDependencyGatesFailClosedWhenScriptsAreMissing(t *testing.T) {
	makefile := ciRepositoryFile(t, "Makefile")
	for _, current := range []struct {
		target string
		script string
	}{
		{target: "safety", script: "scripts/check-public-text.sh"},
		{target: "check-deps", script: "scripts/check-deps.sh"},
	} {
		marker := "\n" + current.target + ":\n"
		start := strings.Index("\n"+makefile, marker)
		if start < 0 {
			t.Fatalf("Makefile target %s not found", current.target)
		}
		remainder := ("\n" + makefile)[start+len(marker):]
		nextTarget := regexp.MustCompile(`(?m)^[A-Za-z0-9_.-]+:`).FindStringIndex(remainder)
		if nextTarget == nil {
			t.Fatalf("Makefile target %s has no following target boundary", current.target)
		}
		recipe := remainder[:nextTarget[0]]
		if !strings.Contains(recipe, "bash "+current.script) {
			t.Errorf("%s does not execute its required script", current.target)
		}
		if strings.Contains(recipe, "if [ -f") || strings.Contains(recipe, "跳过") {
			t.Errorf("%s still treats a missing required script as success", current.target)
		}
	}
}
