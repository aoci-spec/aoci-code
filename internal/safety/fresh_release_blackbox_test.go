package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshReleaseBlackboxStaysOutsideImplementationBoundary(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot, "scripts", "blackbox", "fresh-project", "main_test.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "github.com/aoci-spec/aoci-code/internal/") {
		t.Fatal("release black box must not import AOCI implementation packages")
	}
	for _, anchor := range []string{
		"AOCI_BIN",
		"completion_request_template",
		"candidate_draft_request",
		"cognition-onboarding-candidate-binding/v1",
		`client.call(t, "tools/list"`,
		`client.toolText(t, "aoci_rules"`,
		`client.toolText(t, "aoci_overview"`,
	} {
		if !strings.Contains(text, anchor) {
			t.Errorf("release black box is missing boundary assertion %q", anchor)
		}
	}
}

func TestFreshHostDocumentationRoutesThroughLiveContract(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][]string{
		filepath.Join("spec", "public", "aoci-host-capability-and-interaction-v1.txt"): {
			"Fresh Batch, Candidate binding, and Session views expose the current action",
			"does not import an AOCI implementation package",
		},
		filepath.Join("spec", "public", "aoci-code-cli-runtime-v1.txt"): {
			"Fresh Host transport",
			"abandon the active Session",
		},
		filepath.Join("docs", "getting-started.md"): {
			"Fresh Batch, Candidate-binding, and Session status JSON expose",
			"never needs AOCI source code or an internal hash",
		},
		filepath.Join("docs", "agent-integrations.md"): {
			"consume the current JSON `next_action_contract`",
			"does not infer a Fresh state transition",
		},
	}
	for relative, anchors := range checks {
		data, readErr := os.ReadFile(filepath.Join(repoRoot, relative))
		if readErr != nil {
			t.Fatalf("read %s: %v", relative, readErr)
		}
		for _, anchor := range anchors {
			if !strings.Contains(string(data), anchor) {
				t.Errorf("%s is missing Fresh Host contract %q", relative, anchor)
			}
		}
	}
}
