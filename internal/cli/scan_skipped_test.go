package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHeldSourcesTree(t *testing.T, root string) {
	t.Helper()
	for rel, body := range map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n",
		"assets/logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00",
		"data/empty.txt":  "",
		"data/dump.txt":   strings.Repeat("row\n", (1<<20)/4+1),
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runHeldSourcesCLI(t *testing.T, root string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeCLI(append([]string{"--repo", root}, args...), &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

// The public path a new user walks: init, scan, author what Maintain would
// issue, and reach aligned in auto mode although the tree holds an image, an
// empty file, and a file above the read limit. Releases up to v0.1.0-rc14
// stopped here with three pending_curation: markers and no way out.
func TestFirstScanAnnouncesHeldSourcesAndAutoModeReachesAligned(t *testing.T) {
	root := t.TempDir()
	writeHeldSourcesTree(t, root)
	if code, out := runHeldSourcesCLI(t, root, "init", "--locale", "en-US"); code != ExitOK {
		t.Fatalf("init: code=%d\n%s", code, out)
	}
	code, out := runHeldSourcesCLI(t, root, "scan", "--json")
	if code != ExitOK {
		t.Fatalf("scan: code=%d\n%s", code, out)
	}
	var scan struct {
		Skipped struct {
			Total, Binary, Empty, Oversize, CurationExcluded int
		} `json:"skipped_sources"`
	}
	if err := json.Unmarshal([]byte(out), &scan); err != nil {
		t.Fatalf("scan --json: %v\n%s", err, out)
	}
	if scan.Skipped.Total != 3 || scan.Skipped.Binary != 1 || scan.Skipped.Empty != 1 || scan.Skipped.Oversize != 1 ||
		scan.Skipped.CurationExcluded != 0 {
		t.Fatalf("scan must announce the held sources: %+v", scan.Skipped)
	}

	verify := func() (bool, map[string][]string, []string) {
		code, out := runHeldSourcesCLI(t, root, "verify", "--json")
		var report struct {
			Governance struct {
				Aligned  bool `json:"governance_aligned"`
				Findings []struct {
					Code, Target, Cause string
				} `json:"findings"`
				Drift struct {
					Skipped []string `json:"skipped"`
				} `json:"code_drift"`
			} `json:"governance"`
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("verify --json (code %d): %v\n%s", code, err, out)
		}
		byCode := map[string][]string{}
		for _, finding := range report.Governance.Findings {
			byCode[finding.Code] = append(byCode[finding.Code], finding.Target+"="+finding.Cause)
		}
		return report.Governance.Aligned, byCode, report.Governance.Drift.Skipped
	}
	aligned, findings, skipped := verify()
	if aligned || strings.Join(findings["code_missing"], ",") != ".gitattributes=,AGENTS.md=,main.go=" || len(skipped) != 3 ||
		strings.Join(findings["code_skipped"], ",") != "assets/logo.png=binary,data/dump.txt=oversize,data/empty.txt=empty" {
		t.Fatalf("after scan: aligned=%v findings=%v skipped=%v", aligned, findings, skipped)
	}

	// Author exactly what Verify names, the way the upgrade axis does; nothing
	// else may be demanded on the way to aligned.
	for round := 0; round < 3 && !aligned; round++ {
		for _, target := range findings["code_missing"] {
			rel := strings.TrimSuffix(target, "=")
			source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(source)
			if code, out := runHeldSourcesCLI(t, root, "update-entry", "--path", rel, "--source-sha256", hex.EncodeToString(digest[:]),
				"--entry", filepath.Base(rel)+"[CG5T]: F:Serves the sample repository | R:- | A:- | S:-"); code != ExitOK {
				t.Fatalf("update-entry %s: code=%d\n%s", rel, code, out)
			}
		}
		aligned, findings, skipped = verify()
	}
	if !aligned || len(findings["code_missing"]) != 0 || len(skipped) != 3 {
		t.Fatalf("after authoring every readable source: aligned=%v findings=%v skipped=%v", aligned, findings, skipped)
	}

	code, out = runHeldSourcesCLI(t, root, "index", "agent", "guide", "--agent", "codex", "--json")
	var guide struct {
		Complete          bool   `json:"complete"`
		NextAction        string `json:"next_action"`
		ExecutableTargets int    `json:"executable_targets"`
	}
	if err := json.Unmarshal([]byte(out), &guide); err != nil {
		t.Fatalf("guide --json (code %d): %v\n%s", code, err, out)
	}
	if !guide.Complete || guide.NextAction != "none" || guide.ExecutableTargets != 0 {
		t.Fatalf("guide must be complete with nothing to execute: %+v", guide)
	}
}
