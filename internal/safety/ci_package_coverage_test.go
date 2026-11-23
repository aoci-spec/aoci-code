package safety

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fast-ci's catch-all step excludes the packages later steps run on their
// own. An excluded package no later step names is tested by no PR at all;
// internal/cognitiontxn and internal/managedstate sat in that gap (audit
// G7-01).
func TestFastCIRunsEveryPackageItsCatchAllExcludes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(ciWorkflowDir(t), "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	match := regexp.MustCompile(`grep -Ev '/internal/\(([^)]*)\)\$`).FindStringSubmatch(text)
	if match == nil {
		t.Fatal("ci.yml no longer has the catch-all exclusion this test reads; update the test")
	}
	for _, name := range strings.Split(match[1], "|") {
		named := regexp.MustCompile(`go test [^\n]*\./internal/` + regexp.QuoteMeta(name) + `(\s|$)`)
		if !named.MatchString(text) {
			t.Errorf("internal/%s is excluded from the catch-all and run by no other fast-ci step", name)
		}
	}
}
