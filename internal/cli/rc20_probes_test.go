package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func rc20Git(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func rc20Write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStdout runs fn while os.Stdout is a pipe, for commands that print
// with fmt.Println rather than through the command writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	fn()
	os.Stdout = previous
	_ = writer.Close()
	return <-done
}

func pinLocale(t *testing.T) {
	t.Helper()
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
}

// #107: scan says how many submodules it left alone, and explain names the
// boundary for the gitlink and for a path beneath it.
func TestScanAnnouncesGitSubmodulesAndExplainNamesTheBoundary(t *testing.T) {
	pinLocale(t)
	sub := t.TempDir()
	rc20Git(t, sub, "init", "-q")
	rc20Write(t, sub, "lib.go", "package lib\n")
	rc20Git(t, sub, "add", "lib.go")
	rc20Git(t, sub, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "sub")
	root := t.TempDir()
	rc20Git(t, root, "init", "-q")
	rc20Write(t, root, "main.go", "package main\n")
	rc20Git(t, root, "add", "main.go")
	rc20Git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "super")
	rc20Git(t, root, "-c", "protocol.file.allow=always", "-c", "user.email=t@t", "-c", "user.name=t", "submodule", "add", "-q", sub, "vendor_sub")

	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: %s %s", stdout.String(), stderr.String())
	}
	announced := captureStdout(t, func() {
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
	pinLocale(t)
	root := t.TempDir()
	rc20Git(t, root, "init", "-q")
	rc20Write(t, root, "small.go", "package main\n")
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: %s %s", stdout.String(), stderr.String())
	}
	rc20Write(t, root, "big.go", "package main\n"+strings.Repeat("// line\n", 500))
	section := "===" + filepath.ToSlash(root) + "/===\n"
	rc20Write(t, root, "aoci.code.txt", "#AOCI-CODE-VOLUME: 1\n\n"+section+
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

// #102: under Managed Scope the Legacy refresh refuses, and the refusal now
// names the path that works instead of pointing at the other refusal.
func TestBaselineScopePlanNamesTheManagedScopePath(t *testing.T) {
	pinLocale(t)
	root := t.TempDir()
	rc20Git(t, root, "init", "-q")
	rc20Write(t, root, "main.go", "package main\n")
	rc20Git(t, root, "add", "main.go")
	rc20Git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: %s %s", stdout.String(), stderr.String())
	}
	if code := executeCLI([]string{"--repo", root, "scan"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("scan: %s %s", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "baseline", "scope", "plan"}, &stdout, &stderr); code == ExitOK {
		t.Fatalf("baseline scope plan must refuse under Managed Scope")
	}
	if !strings.Contains(stderr.String(), "baseline_scope_managed_scope_unsupported") || !strings.Contains(stderr.String(), cliMessage("baseline.scope.managed_scope_hint")) {
		t.Fatalf("refusal lacks the Managed Scope hint: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "scan", "--force"}, &stdout, &stderr); code == ExitOK {
		t.Fatalf("scan --force must refuse over a Managed Scope Baseline")
	}
	if !strings.Contains(stderr.String(), "aoci_maintain") || !strings.Contains(stderr.String(), "aoci_remove_entry") {
		t.Fatalf("scan --force refusal must name Maintain and remove_entry: %s", stderr.String())
	}
}
