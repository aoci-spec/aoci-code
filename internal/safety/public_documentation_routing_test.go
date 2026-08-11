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
			"Legacy-only deep status; not the Cognition Volumes maintenance route",
			"`status --deep`, `index score`, and `index agent plan` are Legacy-only",
			"basic, recommended, or full verification level",
			"**OpenCode V1**",
			"init --agent opencode",
		},
		"README.zh-CN.md": {
			"仅用于 Legacy 深度状态，不是 Cognition Volumes 维护路线",
			"`status --deep`、`index score` 和 `index agent plan` 仅用于 Legacy",
			"基础、推荐或完整校验层级",
			"**OpenCode V1**",
			"init --agent opencode",
		},
		filepath.Join("docs", "getting-started.md"): {
			"Fresh initialization uses Code-only Cognition Volumes",
			"calls ordinary no-argument `aoci_maintain`",
			"`status --deep`, `index score`, and `index agent plan` are Legacy-only",
		},
		filepath.Join("docs", "cognition-volumes.md"): {
			"Fresh initialization now creates a semantic-",
			"free Code-only Volumes repository",
			"## Lifecycle command routing",
			"submits it through `aoci_update_entry`",
		},
		filepath.Join("docs", "database-cognition-authoring.md"): {
			"Fresh initialization creates Code-only Volumes",
			"Database Cognition authoring uses ordinary no-argument Maintain",
		},
		filepath.Join("docs", "install.md"): {
			"### Basic installation: archive checksum and binary identity",
			"### Recommended installation: authenticate the checksum publisher",
			"### Full supply-chain verification",
			"has no runtime dependency on GitHub CLI",
			"without a GitHub API login",
			"fully offline verification requires supplying an",
		},
		filepath.Join("docs", "supply-chain.md"): {
			"Consumer assurance is layered",
			"runtime dependency",
		},
		filepath.Join("docs", "agent-integrations.md"): {
			"## OpenCode V1",
			"init --agent opencode",
			"`aoci_aoci_rules`",
			"does not merge OpenCode V2",
			"Refresh or reopen that project",
			"only when it has not loaded the new server",
			"do not require a blanket application restart",
		},
		filepath.Join("docs", "troubleshooting.md"): {
			"first\ncheck the server loaded by the current Host",
			"refresh or\nreconnect that project's MCP integration",
			"reopen the project session when\nthe Host requires it",
			"does not prove that the active\nserver is using those bytes",
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
				t.Errorf("%s is missing documentation contract %q", relative, anchor)
			}
		}
	}
}
