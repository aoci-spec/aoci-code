package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicDocumentationRoutesVolumesAndReleaseVerification(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string][]string{
		"README.md": {
			"docs/getting-started.md",
			"docs/install.md",
			"docs/cognition-volumes.md",
		},
		"README.zh-CN.md": {
			"docs/getting-started.md",
			"docs/install.md",
			"docs/cognition-volumes.md",
		},
		filepath.Join("docs", "getting-started.md"): {
			"aoci_maintain",
			"aoci_update_entry",
		},
		filepath.Join("docs", "cognition-volumes.md"): {
			"../spec/public/aoci-cognition-volumes-v1.txt",
			"database-cognition-authoring.md",
			"aoci update-entry",
		},
		filepath.Join("docs", "database-cognition-authoring.md"): {
			"../spec/public/aoci-database-cognition-authoring-v1.txt",
			"aoci_maintain",
			"aoci_update_entry",
		},
		filepath.Join("docs", "install.md"): {
			"Full supply-chain verification",
		},
		filepath.Join("docs", "supply-chain.md"): {
			".github/workflows/release.yml",
		},
		filepath.Join("docs", "agent-integrations.md"): {
			"init --agent opencode",
			"aoci_rules",
		},
	}

	for relative, anchors := range checks {
		data, readErr := os.ReadFile(filepath.Join(repoRoot, relative))
		if readErr != nil {
			t.Fatalf("read %s: %v", relative, readErr)
		}
		text := string(data)
		for _, anchor := range anchors {
			if !strings.Contains(text, anchor) {
				t.Errorf("%s is missing documentation route %q", relative, anchor)
			}
		}
	}
}
