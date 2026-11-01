package cli

import (
	"strings"
	"testing"
)

// A person running the three everyday commands on a Legacy repository under
// 0.2.0 used to see aligned, ready to commit, and nothing about the layout
// being read-only; only Maintain and the Guide named the migration. The
// onboarding commands printed a next action token and no command.
func TestLegacyLayoutHumanOutputNamesTheMigration(t *testing.T) {
	root := buildUpdateRepo(t)
	for _, args := range [][]string{{"verify"}, {"check"}, {"status"}} {
		stdout, stderr, _ := runCLI(t, root, args...)
		if !strings.Contains(stdout+stderr, "cognition onboard start") {
			t.Fatalf("%s on a Legacy repository does not name the migration:\n%s%s", args[0], stdout, stderr)
		}
	}
	stdout, stderr, code := runCLI(t, root, "cognition", "onboard", "start")
	if code != ExitOK {
		t.Fatalf("onboard start: exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "cognition onboard next") {
		t.Fatalf("onboard start names no next command:\n%s", stdout)
	}
	stdout, stderr, code = runCLI(t, root, "cognition", "onboard", "next")
	if code != ExitOK || !strings.Contains(stdout, "--completion-file") {
		t.Fatalf("onboard next names no next command: exit %d\n%s%s", code, stdout, stderr)
	}
}
