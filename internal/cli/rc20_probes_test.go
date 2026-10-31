package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/aoci-spec/aoci-code/textassets"
)

// #107: scan says how many submodules it left alone, and explain names the
// boundary for the gitlink and for a path beneath it.
func TestScanAnnouncesGitSubmodulesAndExplainNamesTheBoundary(t *testing.T) {
	testkit.PinLocale(t, textassets.DefaultLocale)
	sub := t.TempDir()
	testkit.Git(t, sub, "init", "-q")
	testkit.WriteFile(t, sub, "lib.go", "package lib\n")
	testkit.Git(t, sub, "add", "lib.go")
	testkit.Git(t, sub, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "sub")
	root := t.TempDir()
	testkit.Git(t, root, "init", "-q")
	testkit.WriteFile(t, root, "main.go", "package main\n")
	testkit.Git(t, root, "add", "main.go")
	testkit.Git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "super")
	testkit.Git(t, root, "-c", "protocol.file.allow=always", "-c", "user.email=t@t", "-c", "user.name=t", "submodule", "add", "-q", sub, "vendor_sub")

	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: %s %s", stdout.String(), stderr.String())
	}
	announced := testkit.CaptureStdout(t, func() {
		if code := executeCLI([]string{"--repo", root, "scan"}, &stdout, &stderr); code != ExitOK {
			t.Errorf("scan: %s %s", stdout.String(), stderr.String())
		}
	})
	want := cliMessage("scan.submodules_skipped", 1)
	if !strings.Contains(announced, want) {
		t.Fatalf("scan did not announce the submodule: %q", announced)
	}
	for _, path := range []string{"vendor_sub", "vendor_sub/lib.go"} {
		stdout.Reset()
		stderr.Reset()
		if code := executeCLI([]string{"--repo", root, "scope", "explain", path}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("explain %s: %s %s", path, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "git_submodule:") || !strings.Contains(stdout.String(), cliMessage("scope.explain_submodule_hint")) {
			t.Fatalf("explain %s must name the submodule boundary: %s", path, stdout.String())
		}
	}
}

// #103: the E scale letter is audited against the source line count in
// verify and check, as information that never changes the exit code.
func TestCheckAndVerifyReportEScaleMismatchesWithoutFailing(t *testing.T) {
	testkit.PinLocale(t, textassets.DefaultLocale)
	root := t.TempDir()
	testkit.Git(t, root, "init", "-q")
	testkit.WriteFile(t, root, "small.go", "package main\n")
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: %s %s", stdout.String(), stderr.String())
	}
	testkit.WriteFile(t, root, "big.go", "package main\n"+strings.Repeat("// line\n", 500))
	section := "===" + filepath.ToSlash(root) + "/===\n"
	testkit.WriteFile(t, root, "aoci.code.txt", "#AOCI-CODE-VOLUME: 1\n\n"+section+
		"big.go[CG5T]: F:Grew past its band | R:- | A:- | S:-\n"+
		"small.go[CG5T]: F:Still tiny | R:- | A:- | S:-\n")
	for _, command := range []string{"check", "verify"} {
		stdout.Reset()
		stderr.Reset()
		executeCLI([]string{"--repo", root, "--json", command}, &stdout, &stderr)
		var report struct {
			EScale struct {
				Checked    int `json:"checked"`
				Mismatched int `json:"mismatched"`
				Items      []struct {
					Path     string   `json:"path"`
					Actual   string   `json:"actual"`
					Expected []string `json:"expected"`
				} `json:"items"`
			} `json:"e_scale"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("%s --json: %v: %s %s", command, err, stdout.String(), stderr.String())
		}
		if report.EScale.Checked != 2 || report.EScale.Mismatched != 1 || len(report.EScale.Items) != 1 ||
			report.EScale.Items[0].Path != "big.go" || report.EScale.Items[0].Actual != "T" || strings.Join(report.EScale.Items[0].Expected, "/") != "L" {
			t.Fatalf("%s e_scale audit is wrong: %+v", command, report.EScale)
		}
	}
	stdout.Reset()
	stderr.Reset()
	executeCLI([]string{"--repo", root, "check"}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), cliMessage("check.e_scale", 1, 2)) || !strings.Contains(stdout.String(), "big.go") {
		t.Fatalf("human check output lacks the E scale line: %s", stdout.String())
	}
}
