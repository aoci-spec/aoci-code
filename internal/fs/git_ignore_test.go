package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathIgnoredByGitDistinguishesNegativeRules(t *testing.T) {
	for _, source := range []string{".gitignore", ".git/info/exclude", "core.excludesFile"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			gitCommand(t, root, "init", "-q")
			ruleFile := source
			if source == "core.excludesFile" {
				ruleFile = "global-ignore"
				gitCommand(t, root, "config", "core.excludesFile", filepath.Join(root, ruleFile))
			}
			mustWrite(t, root, ruleFile, "*.txt\n!visible.txt\n!reignored.txt\nreignored.txt\n\\!literal.txt\n")
			for _, test := range []struct {
				path string
				want bool
			}{
				{"hidden.txt", true},
				{"visible.txt", false},
				{"reignored.txt", true},
				{"!literal.txt", true},
				{"unmatched.go", false},
				{"tracked.txt", false},
			} {
				mustWrite(t, root, test.path, "fixture\n")
				if test.path == "tracked.txt" {
					gitCommand(t, root, "add", "-f", "--", test.path)
				}
				ignored, rule := PathIgnoredByGit(root, test.path)
				if ignored != test.want {
					t.Errorf("%s: ignored=%t, want %t (rule %q)", test.path, ignored, test.want, rule)
				}
				if !test.want && rule != "" {
					t.Errorf("%s: a visible path must not name an excluding rule: %q", test.path, rule)
				}
				if test.want && !strings.Contains(rule, ruleFile+":") {
					t.Errorf("%s: excluding rule must retain its source: %q", test.path, rule)
				}
			}
		})
	}
}

func TestPathIgnoredByGitUsesUnquotedPatternFields(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	for _, test := range []struct {
		pattern string
		want    bool
	}{
		{"!visible\tfile.txt", false},
		{"visible\tfile.txt", true},
	} {
		mustWrite(t, root, ".gitignore", "*.txt\n"+test.pattern+"\n")
		ignored, rule := PathIgnoredByGit(root, "visible\tfile.txt")
		if ignored != test.want {
			t.Errorf("pattern %q: ignored=%t, want %t (rule %q)", test.pattern, ignored, test.want, rule)
		}
		if test.want && rule != ".gitignore:2:"+test.pattern {
			t.Errorf("rule fields must remain intact: %q", rule)
		}
	}
}

func TestPathIgnoredByGitWithoutGitFacts(t *testing.T) {
	if ignored, rule := PathIgnoredByGit(t.TempDir(), "aoci.txt"); ignored || rule != "" {
		t.Fatalf("a non-Git directory must not accuse an ignore rule: %t %q", ignored, rule)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[malformed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ignored, rule := PathIgnoredByGit(root, "aoci.txt"); ignored || rule != "" {
		t.Fatalf("failed Git lookup must not accuse an ignore rule: %t %q", ignored, rule)
	}
}
