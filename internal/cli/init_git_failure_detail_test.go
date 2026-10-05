package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// A Safe Inventory git failure used to surface as the bare machine code; the
// operator who hit #97 needed an external git shim to learn that git had been
// killed mid-enumeration. The CLI now prints the tail of git's own stderr as a
// separate diagnostic line in every output mode, while the machine code and
// the exit code stay exactly what they were.
func TestInitPrintsGitStderrTailOnInventoryFailure(t *testing.T) {
	// The invocation runs in en-US; pin the process locale to it as well so the
	// expected prefix is rendered from the same catalog the command used.
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("not a repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"human", "json"} {
		args := []string{"--repo", root, "init", "--locale", "en-US"}
		if mode == "json" {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := executeCLI(args, &stdout, &stderr)
		if code == ExitOK {
			t.Fatalf("%s: init on an unverifiable git boundary must fail: stdout=%s stderr=%s", mode, stdout.String(), stderr.String())
		}
		prefix := strings.SplitN(cliMessage("cli.git_stderr_tail", "SENTINEL"), "SENTINEL", 2)[0]
		// Human mode reports the code on stderr; JSON mode carries it in the
		// error envelope on stdout. The git detail is stderr in both.
		visible := stderr.String()
		if mode == "json" {
			visible = stdout.String()
		}
		if !strings.Contains(visible, "safe_inventory_git_query_failed") {
			t.Fatalf("%s: the machine code must stay visible: stdout=%s stderr=%s", mode, stdout.String(), stderr.String())
		}
		index := strings.Index(stderr.String(), prefix)
		if index < 0 || strings.TrimSpace(stderr.String()[index+len(prefix):]) == "" {
			t.Fatalf("%s: git's own stderr must follow the failure as its own line: %s", mode, stderr.String())
		}
		if mode == "json" && strings.Contains(stdout.String(), prefix) {
			t.Fatalf("the git detail is a diagnostic and must never enter the JSON business output: %s", stdout.String())
		}
	}
}

// scan runs the inventory after init succeeded; a git index that breaks later
// must surface git's own message the same way, and under zh-CN the line is the
// documented verbatim exception to locale suppression (docs/localization.md).
func TestScanPrintsGitStderrTailOnListingFailureInBothLocales(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		root := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "x"}} {
			if args[0] == "add" {
				if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("git", append([]string{"-C", root}, args...)...)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, output)
			}
		}
		var stdout, stderr bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "init", "--locale", locale}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("%s: init failed: %s %s", locale, stdout.String(), stderr.String())
		}
		if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("DIRC garbage"), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		if code := executeCLI([]string{"--repo", root, "scan"}, &stdout, &stderr); code == ExitOK {
			t.Fatalf("%s: scan over a corrupt git index must fail", locale)
		}
		previous := textassets.ActiveLocale()
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		prefix := strings.SplitN(cliMessage("cli.git_stderr_tail", "SENTINEL"), "SENTINEL", 2)[0]
		_ = textassets.SetActiveLocale(previous)
		index := strings.Index(stderr.String(), prefix)
		if index < 0 || !strings.Contains(stderr.String()[index:], "index") {
			t.Fatalf("%s: scan must print git's own message after the failure: %s", locale, stderr.String())
		}
	}
}
